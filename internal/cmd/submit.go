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
	RunE: runSubmit,
}

var (
	submitContinueFlag bool
	submitAbortFlag    bool
)

func init() {
	submitCmd.Flags().BoolVar(&submitContinueFlag, "continue", false, "resume after resolving conflicts")
	submitCmd.Flags().BoolVar(&submitAbortFlag, "abort", false, "abort and restore state")
	rootCmd.AddCommand(submitCmd)
}

func runSubmit(cmd *cobra.Command, args []string) error {
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

	// If rebase was just --abort or still in conflict, don't push
	if submitAbortFlag {
		return nil
	}
	inProgress, _ := git.IsRebaseInProgress()
	if inProgress {
		return nil
	}

	// Push
	fmt.Println()
	fmt.Println("Pushing to remote...")

	expectedUpstream := ctx.Config.Remote + "/" + branch
	actualUpstream := git.Upstream()

	if actualUpstream == expectedUpstream {
		if err := git.PushForceWithLease(); err != nil {
			return fmt.Errorf("failed to push to remote: %w", err)
		}
	} else {
		if actualUpstream != "" {
			fmt.Printf("(fixing upstream: %s → %s)\n", actualUpstream, expectedUpstream)
		} else {
			fmt.Println("(setting upstream for new branch)")
		}
		if err := git.PushSetUpstream(ctx.Config.Remote); err != nil {
			return fmt.Errorf("failed to push to remote: %w", err)
		}
	}

	fmt.Println()
	ui.Success("Submitted!")

	if github.IsAvailable() {
		fmt.Println()
		if ui.Confirm("Watch PR status?", true) {
			fmt.Println()
			return runWatch(cmd, nil)
		}
	}

	return nil
}
