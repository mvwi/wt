package cmd

import (
	"fmt"
	"os/exec"
	"strconv"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:     "open [name]",
	GroupID: groupWorkflow,
	Short:   "Open PR in browser",
	Long: `Open the GitHub pull request for a worktree in your browser.

Without arguments, opens the PR for the current branch.
With a name, resolves the worktree and opens its PR.

Requires the GitHub CLI (gh) to be installed.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)
}

func runOpen(cmd *cobra.Command, args []string) error {
	if !github.IsAvailable() {
		return fmt.Errorf("gh CLI is required for this command (brew install gh)")
	}

	ctx, err := newContext()
	if err != nil {
		return err
	}

	var branch string

	if len(args) == 0 {
		// Current branch
		branch, err = git.CurrentBranch()
		if err != nil {
			return fmt.Errorf("not in a git repository or detached HEAD\n   Run this from inside a worktree")
		}
	} else {
		// Resolve worktree by name
		worktrees, err := git.ListWorktrees()
		if err != nil {
			return err
		}
		target := resolveWorktree(ctx, worktrees, args[0])
		if target == "" {
			return fmt.Errorf("worktree not found: %s\n   Run wt list to see available worktrees", args[0])
		}
		branch, err = git.CurrentBranchIn(target)
		if err != nil {
			return fmt.Errorf("could not determine branch for worktree: %s", args[0])
		}
	}

	pr, err := github.GetPRForBranch(branch)
	if err != nil {
		return fmt.Errorf("failed to check for PR: %w", err)
	}
	if pr == nil {
		return fmt.Errorf("no open PR for branch: %s\n   Run wt submit to push and create one", branch)
	}

	return exec.Command("gh", "pr", "view", strconv.Itoa(pr.Number), "--web").Run()
}
