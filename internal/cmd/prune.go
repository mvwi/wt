package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:     "prune",
	GroupID: groupManage,
	Short:   "Clean up stale worktrees (merged/closed PRs)",
	Long: `Interactively remove worktrees whose PRs have been merged or closed.

Skips:
  - Main worktree
  - Current worktree
  - Worktrees with uncommitted changes
  - Base/main branches`,
	RunE: runPrune,
}

func init() {
	rootCmd.AddCommand(pruneCmd)
}

type staleWorktree struct {
	Path   string
	Branch string
	Reason string
}

func runPrune(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()

	fmt.Println("Scanning for stale worktrees...")

	mergedPRs, _ := github.ListPRs("merged")
	closedPRs, _ := github.ListPRs("closed")

	worktrees, err := git.ListWorktrees()
	if err != nil {
		return err
	}

	var stale []staleWorktree

	for _, wt := range worktrees {
		// Skip main
		if wt.Path == ctx.MainWorktree {
			continue
		}
		// Skip current
		if wt.Path == cwd || isSubpath(cwd, wt.Path) {
			continue
		}

		branch := wt.Branch
		if branch == "" {
			branch, _ = git.CurrentBranchIn(wt.Path)
		}
		if branch == ctx.Config.BaseBranch || branch == "main" || branch == "master" {
			continue
		}

		// Skip worktrees with changes
		if git.HasChangesIn(wt.Path) {
			continue
		}

		// Check PR status
		mergedPR := github.FindPRForBranch(mergedPRs, branch)
		closedPR := github.FindPRForBranch(closedPRs, branch)

		if mergedPR != nil {
			stale = append(stale, staleWorktree{wt.Path, branch, fmt.Sprintf("PR #%d merged", mergedPR.Number)})
		} else if closedPR != nil {
			stale = append(stale, staleWorktree{wt.Path, branch, fmt.Sprintf("PR #%d closed", closedPR.Number)})
		}
	}

	git.PruneWorktrees()

	if len(stale) == 0 {
		ui.Success("No stale worktrees found")
		return nil
	}

	fmt.Printf("Found %d stale worktree(s):\n\n", len(stale))

	// Phase 1: collect decisions
	var toRemove []int

	for i, s := range stale {
		short := filepath.Base(s.Path)
		fmt.Printf("%s\n", ui.Yellow(short))
		fmt.Printf("  └─ %s\n", s.Branch)
		fmt.Printf("  └─ %s\n", s.Reason)
		if ui.Confirm("  Remove?", false) {
			toRemove = append(toRemove, i)
			fmt.Println("  → queued")
		} else {
			fmt.Println("  → skipped")
		}
		fmt.Println()
	}

	// Phase 2: execute removals
	if len(toRemove) == 0 {
		fmt.Println("No worktrees to remove")
		return nil
	}

	fmt.Printf("Removing %d worktree(s)...\n", len(toRemove))

	removedCount := 0
	for _, i := range toRemove {
		s := stale[i]
		short := filepath.Base(s.Path)

		if err := git.RemoveWorktree(s.Path); err != nil {
			fmt.Printf("  %s %s (failed)\n", ui.Red(ui.Fail), short)
			continue
		}

		if git.BranchExists(s.Branch) {
			_ = git.DeleteBranch(s.Branch)
		}
		fmt.Printf("  %s %s\n", ui.Green(ui.Pass), short)
		removedCount++
	}

	fmt.Println()
	ui.Success("Removed %d worktree(s)", removedCount)
	return nil
}
