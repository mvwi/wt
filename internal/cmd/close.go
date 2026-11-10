package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var closeCmd = &cobra.Command{
	Use:     "close [name]",
	Aliases: []string{"rm"},
	GroupID: groupManage,
	Short:   "Close and clean up a worktree",
	Long: `Close a worktree by removing the directory and deleting the local branch.

Without arguments, closes the current worktree.
With a name, closes the specified worktree.

Safety checks:
  - Asks before discarding uncommitted changes or unpushed commits
    (--yes does not skip this; --force does)
  - Warns if PR is still open
  - Cannot close the main repository worktree`,
	Example: `  wt close                 Close current worktree
  wt close sidebar         Close the "sidebar" worktree
  wt close sidebar --yes   Skip prompts that lose no work
  wt close sidebar --force Discard uncommitted changes and unpushed commits`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runClose,
}

var closeForceFlag bool

func init() {
	closeCmd.Flags().BoolVarP(&closeForceFlag, "force", "f", false, "discard uncommitted changes and unpushed commits")
	addOutputFlag(closeCmd)
	rootCmd.AddCommand(closeCmd)
}

// closeJSON is what close prints with --output json.
type closeJSON struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	Branch        string `json:"branch"`
	BranchDeleted bool   `json:"branch_deleted"`
}

func runClose(cmd *cobra.Command, args []string) error {
	if err := startOutput(); err != nil {
		return err
	}
	ctx, err := newContext()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine current directory: %w", err)
	}
	worktrees, err := git.ListWorktrees()
	if err != nil {
		return err
	}

	// Determine target
	var targetPath, targetBranch string

	if len(args) > 0 {
		var (
			fuzzy      bool
			resolveErr error
		)
		targetPath, fuzzy, resolveErr = resolveWorktree(ctx, worktrees, args[0])
		if resolveErr != nil {
			return resolveErr
		}
		if targetPath == "" {
			return fmt.Errorf("worktree not found: %s\n   Run wt list to see available worktrees", args[0])
		}
		if fuzzy {
			if !ui.Confirm(fmt.Sprintf("Close %s?", ctx.shortName(targetPath)), false) {
				return cancelled(nil)
			}
		}
	} else {
		// Close current worktree. cwd may be a subdirectory, so ask git for the root.
		if targetPath, err = git.TopLevel(); err != nil {
			return err
		}
		if targetPath == ctx.MainWorktree {
			return fmt.Errorf("cannot close the main repository worktree\n   Specify a worktree name: wt close <name>")
		}
	}

	if targetPath == ctx.MainWorktree {
		return fmt.Errorf("cannot close the main repository worktree")
	}

	targetBranch, _ = git.CurrentBranchIn(targetPath)

	// Safety: uncommitted changes
	if git.HasChangesIn(targetPath) {
		ui.Warn("Worktree has uncommitted changes:")
		short, _ := git.StatusShortIn(targetPath)
		fmt.Println(short)
		fmt.Println()
		if ok, err := ui.ConfirmDiscard("Discard changes and close?", closeForceFlag); !ok {
			return cancelled(err)
		}
	}

	// Safety: unpushed commits
	if unpushed := git.UnpushedCountIn(targetPath); unpushed > 0 {
		ui.Warn("Branch has %d unpushed commit(s)", unpushed)
		if ok, err := ui.ConfirmDiscard("Discard commits and close?", closeForceFlag); !ok {
			return cancelled(err)
		}
	}

	// Safety: open PR
	if github.IsAvailable() && targetBranch != "" {
		pr, err := github.GetPRForBranch(targetBranch)
		if err != nil && !isNotGitHubRemote(err) {
			fmt.Fprintln(os.Stderr, ui.Dim("Skipped the open-PR check: "+firstLine(err.Error())))
		}
		if pr != nil && pr.State == "OPEN" {
			ui.Warn("PR #%d is still open", pr.Number)
			if !ui.Confirm("Close worktree anyway?", false) {
				return cancelled(nil)
			}
		}
	}

	warnRepoHooks(ctx)
	fmt.Printf("Closing worktree: %s\n", targetPath)
	fmt.Printf("Branch: %s\n", targetBranch)
	fmt.Println()

	// Check if we need to cd out before removal
	needsCd := len(args) == 0 || cwd == targetPath || isSubpath(cwd, targetPath)

	branchDeleted, err := closeWorktree(ctx, targetPath, targetBranch, false)
	if err != nil {
		return err
	}

	// Our cwd may be gone now, and git refuses to run from a deleted dir.
	if err := os.Chdir(ctx.MainWorktree); err != nil {
		return fmt.Errorf("cannot enter main worktree: %w", err)
	}
	if needsCd {
		ui.PrintCdHint(ctx.MainWorktree)
		fmt.Printf("Moved to main repo: %s\n", ctx.MainWorktree)
	}
	if branchDeleted {
		ui.Success("Deleted local branch: %s", targetBranch)
	}
	branchKept := !branchDeleted && git.BranchExists(targetBranch)

	fmt.Println()
	if branchKept {
		ui.Warn("Closed worktree, but branch %s is still there", targetBranch)
	} else {
		ui.Success("Closed worktree")
	}
	ui.PrintCTA("wt list")
	if jsonOut != nil {
		return printJSON(closeJSON{Name: ctx.shortName(targetPath), Path: targetPath, Branch: targetBranch, BranchDeleted: branchDeleted})
	}
	return nil
}

// closeWorktree is the one way wt removes a worktree, shared by close, prune
// and the dashboard: pre_close hooks, the removal, post_close hooks, then the
// branch delete. A failing pre_close hook or removal is the error; later
// failures only warn. quiet prints nothing, for the dashboard.
func closeWorktree(ctx *cmdContext, path, branch string, quiet bool) (branchDeleted bool, err error) {
	hooks := ctx.Config.Hooks
	var env []string
	if len(hooks.PreClose)+len(hooks.PostClose) > 0 {
		// Read the env now: the index file goes away with the worktree.
		env = worktreeEnv(worktreeVars(ctx, path))
	}
	if err := runHooks("pre_close", hooks.PreClose, path, env, quiet); err != nil {
		return false, fmt.Errorf("%w\n   Close cancelled", err)
	}
	if err := git.RemoveWorktree(path); err != nil {
		return false, fmt.Errorf("failed to remove worktree: %w", err)
	}
	if err := runHooks("post_close", hooks.PostClose, ctx.MainWorktree, env, quiet); err != nil && !quiet {
		ui.Warn("%v", err)
	}

	// Run git from the main worktree: the caller's cwd may be the one just removed.
	main := ctx.MainWorktree
	if git.RunSilentIn(main, "show-ref", "--verify", "--quiet", "refs/heads/"+branch) != nil {
		return false, nil
	}
	if _, err := git.RunIn(main, "branch", "-D", branch); err != nil {
		if !quiet {
			ui.Warn("Could not delete local branch %s: %v", branch, err)
		}
		return false, nil
	}
	return true, nil
}

// cancelled ends a command after a declined prompt: exit 0 for a plain "no",
// or the error when the prompt could not be asked. With --output json a "no"
// is an error too, since no prompt was shown and stdout would stay empty.
func cancelled(err error) error {
	if err != nil {
		return err
	}
	if jsonOut != nil {
		return fmt.Errorf("needs confirmation, and --output json never prompts\n   Re-run with --yes")
	}
	fmt.Println("Cancelled")
	return nil
}

// firstLine returns the first line of s, for one-line notes about errors.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
