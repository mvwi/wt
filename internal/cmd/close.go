package cmd

import (
	"fmt"
	"os"

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
	rootCmd.AddCommand(closeCmd)
}

func runClose(cmd *cobra.Command, args []string) error {
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
				fmt.Println("Cancelled")
				return nil
			}
		}
	} else {
		// Close current worktree
		if cwd == ctx.MainWorktree || isSubpath(cwd, ctx.MainWorktree) {
			return fmt.Errorf("cannot close the main repository worktree\n   Specify a worktree name: wt close <name>")
		}
		targetPath = cwd
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
		pr, _ := github.GetPRForBranch(targetBranch)
		if pr != nil && pr.State == "OPEN" {
			ui.Warn("PR #%d is still open", pr.Number)
			if !ui.Confirm("Close worktree anyway?", false) {
				fmt.Println("Cancelled")
				return nil
			}
		}
	}

	// Read the env now: the index file goes away with the worktree.
	var hookEnv []string
	if len(ctx.Config.Hooks.PreClose)+len(ctx.Config.Hooks.PostClose) > 0 {
		hookEnv = worktreeEnv(worktreeVars(ctx, targetPath))
	}
	if err := runHooks("pre_close", ctx.Config.Hooks.PreClose, targetPath, hookEnv); err != nil {
		return fmt.Errorf("%w\n   Close cancelled", err)
	}

	fmt.Printf("Closing worktree: %s\n", targetPath)
	fmt.Printf("Branch: %s\n", targetBranch)
	fmt.Println()

	// Check if we need to cd out before removal
	needsCd := cwd == targetPath || isSubpath(cwd, targetPath)

	// Remove worktree
	if err := git.RemoveWorktree(targetPath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	// Our cwd may be gone now, and git refuses to run from a deleted dir.
	if err := os.Chdir(ctx.MainWorktree); err != nil {
		return fmt.Errorf("cannot enter main worktree: %w", err)
	}

	// cd out after successful removal
	if needsCd {
		ui.PrintCdHint(ctx.MainWorktree)
		fmt.Printf("Moved to main repo: %s\n", ctx.MainWorktree)
	}

	if err := runHooks("post_close", ctx.Config.Hooks.PostClose, ctx.MainWorktree, hookEnv); err != nil {
		ui.Warn("%v", err)
	}

	// Delete local branch
	branchKept := false
	if targetBranch != "" && git.BranchExists(targetBranch) {
		if err := git.DeleteBranch(targetBranch); err != nil {
			ui.Warn("Could not delete local branch %s: %v", targetBranch, err)
			branchKept = true
		} else {
			ui.Success("Deleted local branch: %s", targetBranch)
		}
	}

	fmt.Println()
	if branchKept {
		ui.Warn("Closed worktree, but branch %s is still there", targetBranch)
	} else {
		ui.Success("Closed worktree")
	}
	ui.PrintCTA("wt list")
	return nil
}

// cancelled ends a command after a declined prompt: exit 0 for a plain "no",
// or the error when the prompt could not be asked.
func cancelled(err error) error {
	if err != nil {
		return err
	}
	fmt.Println("Cancelled")
	return nil
}
