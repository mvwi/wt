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
  - Warns if worktree has uncommitted changes
  - Warns if PR is still open
  - Cannot close the main repository worktree`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runClose,
}

func init() {
	rootCmd.AddCommand(closeCmd)
}

func runClose(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	worktrees, err := git.ListWorktrees()
	if err != nil {
		return err
	}

	// Determine target
	var targetPath, targetBranch string

	if len(args) > 0 {
		targetPath = resolveWorktree(ctx, worktrees, args[0])
		if targetPath == "" {
			return fmt.Errorf("worktree not found: %s\n   Run wt list to see available worktrees", args[0])
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
		short, _ := git.RunIn(targetPath, "status", "--short")
		fmt.Println(short)
		fmt.Println()
		if !ui.Confirm("Discard changes and close?", false) {
			fmt.Println("Cancelled")
			return nil
		}
	}

	// Safety: open PR
	if github.IsAvailable() && targetBranch != "" {
		pr := github.GetPRForBranch(targetBranch)
		if pr != nil && pr.State == "OPEN" {
			ui.Warn("PR #%d is still open", pr.Number)
			if !ui.Confirm("Close worktree anyway?", false) {
				fmt.Println("Cancelled")
				return nil
			}
		}
	}

	fmt.Printf("Closing worktree: %s\n", targetPath)
	fmt.Printf("Branch: %s\n", targetBranch)
	fmt.Println()

	// If we're in the worktree being closed, cd out
	if cwd == targetPath || isSubpath(cwd, targetPath) {
		ui.PrintCdHint(ctx.MainWorktree)
		fmt.Printf("Moved to main repo: %s\n", ctx.MainWorktree)
	}

	// Remove worktree
	if err := git.RemoveWorktree(targetPath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	// Delete local branch
	if targetBranch != "" && git.BranchExists(targetBranch) {
		if err := git.DeleteBranch(targetBranch); err != nil {
			ui.Warn("Could not delete local branch %s: %v", targetBranch, err)
		} else {
			ui.Success("Deleted local branch: %s", targetBranch)
		}
	}

	fmt.Println()
	ui.Success("Closed worktree")
	return nil
}
