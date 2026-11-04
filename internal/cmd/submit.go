package cmd

import (
	"fmt"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var submitCmd = &cobra.Command{
	Use:     "submit",
	GroupID: groupSync,
	Short:   "Rebase on base branch + push to remote",
	Long: `Rebase the current branch onto the base branch, then push to remote.

Uses --force-with-lease for safety (fails if remote has unexpected commits).
Cannot submit the base branch (use git push directly).`,
	Example: `  wt submit              Rebase + push current branch
  wt submit --continue   Resume after resolving rebase conflicts
  wt submit --abort      Abort rebase and cancel push`,
	RunE: runSubmit,
}

var (
	submitContinueFlag bool
	submitAbortFlag    bool
)

func init() {
	submitCmd.Flags().BoolVar(&submitContinueFlag, "continue", false, "resume after resolving conflicts")
	submitCmd.Flags().BoolVar(&submitAbortFlag, "abort", false, "abort and restore state")
	addOutputFlag(submitCmd)
	rootCmd.AddCommand(submitCmd)
}

// submitJSON is what submit prints with --output json.
type submitJSON struct {
	Branch string   `json:"branch"`
	Pushed bool     `json:"pushed"`
	PR     int      `json:"pr,omitempty"`
	CTA    []string `json:"cta,omitempty"`
}

func runSubmit(cmd *cobra.Command, args []string) error {
	if err := startOutput(); err != nil {
		return err
	}
	ctx, err := newContext()
	if err != nil {
		return err
	}

	branch, err := git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("not in a git repository or detached HEAD\n   Run this from inside a worktree")
	}

	if ctx.isBaseBranch(branch) {
		return fmt.Errorf("cannot submit the base branch (%s)\n   Switch to a feature worktree, or use git push directly", branch)
	}

	// Delegate to rebase first
	if err := runRebaseWith(rebaseOpts{
		continueRebase: submitContinueFlag,
		abort:          submitAbortFlag,
	}); err != nil {
		return err
	}

	// After --abort there is nothing to push. A rebase that stops on
	// conflicts returns an error above, so nothing is pushed then either.
	if submitAbortFlag {
		if jsonOut != nil {
			return printJSON(submitJSON{Branch: branch})
		}
		return nil
	}

	expectedUpstream := ctx.Config.Remote + "/" + branch
	actualUpstream := git.Upstream()

	// Push
	fmt.Println()
	head, _ := git.RevParseHead()
	remoteHead, _ := git.Run("rev-parse", "--verify", "--quiet", expectedUpstream)
	pushed := true
	switch {
	case actualUpstream == expectedUpstream && head != "" && head == remoteHead:
		pushed = false
		fmt.Printf("Nothing to push %s %s already matches your branch\n", ui.Dash, expectedUpstream)
	case actualUpstream == expectedUpstream:
		fmt.Println("Pushing to remote…")
		if err := git.PushForceWithLease(); err != nil {
			// Likely "stale info": the remote moved since we last fetched.
			// After the fetch, a retry overwrites those remote commits.
			fmt.Println()
			ui.Warn("Push rejected %s the remote branch has commits you haven't fetched", ui.Dash)
			fmt.Println("  Fetching latest remote state…")
			if fetchErr := git.Fetch(ctx.Config.Remote, branch); fetchErr != nil {
				return fmt.Errorf("failed to fetch: %w", fetchErr)
			}
			if ok, err := ui.ConfirmDiscard("Overwrite the remote commits?", false); !ok {
				if err != nil {
					return fmt.Errorf("push rejected: %s has commits you don't have\n   Check them with: git log HEAD..%s", expectedUpstream, expectedUpstream)
				}
				return fmt.Errorf("push cancelled")
			}
			fmt.Println()
			fmt.Println("Pushing to remote…")
			if err := git.PushForceWithLease(); err != nil {
				return fmt.Errorf("failed to push to remote: %w", err)
			}
		}
	default:
		fmt.Println("Pushing to remote…")
		if actualUpstream != "" {
			fmt.Printf("(fixing upstream: %s → %s)\n", actualUpstream, expectedUpstream)
		} else {
			fmt.Println("(setting upstream for new branch)")
		}
		if err := git.PushSetUpstream(ctx.Config.Remote); err != nil {
			return fmt.Errorf("failed to push to remote: %w", err)
		}
	}
	if pushed {
		fmt.Println()
		ui.Success("Submitted!")
	}

	if git.HasChanges() {
		fmt.Println("Note: uncommitted changes stay local. Commit them to include them.")
	}

	if jsonOut != nil {
		out := submitJSON{Branch: branch, Pushed: pushed}
		if pr, err := github.GetPRForBranch(branch); err == nil && github.IsAvailable() {
			out.CTA = []string{"gh pr create"}
			if pr != nil {
				out.PR, out.CTA = pr.Number, []string{"wt watch"}
			}
		}
		return printJSON(out)
	}

	if !github.IsAvailable() {
		return nil
	}
	pr, err := github.GetPRForBranch(branch)
	switch {
	case err != nil:
		// No PR info (e.g. not a GitHub remote); say nothing.
	case pr == nil:
		fmt.Println()
		fmt.Printf("No open PR for this branch. Create one: %s\n", ui.Cyan("gh pr create"))
		ui.PrintCTA("gh pr create")
	case ui.IsTTY():
		fmt.Println()
		if ui.Confirm("Watch PR status?", true) {
			fmt.Println()
			return runWatch(cmd, nil)
		}
	default:
		ui.PrintCTA("wt watch")
	}
	return nil
}
