package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/tui"
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
  - Worktrees with uncommitted changes or unpushed commits
  - Base/main branches`,
	Example: `  wt prune             Interactively remove stale worktrees
  wt prune --dry-run   Show what would be removed
  wt prune --yes       Remove all stale worktrees without prompts`,
	RunE: runPrune,
}

var pruneDryRun bool

func init() {
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "show what would be removed without removing anything")
	rootCmd.AddCommand(pruneCmd)
}

type staleWorktree struct {
	Path   string
	Branch string
	Reason string
	Kept   string // why prune left it alone, for skipped worktrees
}

func runPrune(cmd *cobra.Command, args []string) error {
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

	// Candidates: feature worktrees other than main and the current one.
	var candidates []git.Worktree
	var branches []string
	for _, wt := range worktrees {
		if wt.Path == ctx.MainWorktree || wt.Path == cwd || isSubpath(cwd, wt.Path) {
			continue
		}
		branch := wt.Branch
		if branch == "" {
			branch, _ = git.CurrentBranchIn(wt.Path)
		}
		if ctx.isBaseBranch(branch) {
			continue
		}
		candidates = append(candidates, git.Worktree{Path: wt.Path, Branch: branch})
		branches = append(branches, branch)
	}

	spin := ui.NewSpinner("Scanning for stale worktrees")

	reasons, partial, ghErr := prReasons(branches)
	local := !github.IsAvailable() || ghErr != nil
	if local {
		reasons, err = mergedReasons(ctx, branches)
		if err != nil {
			spin.Stop()
			return err
		}
	}

	var stale []staleWorktree
	var skippedDirty []staleWorktree

	for _, wt := range candidates {
		reason := reasons[wt.Branch]
		if reason == "" {
			continue
		}

		// Stale but holds work that exists nowhere else — track separately.
		// A merged or closed PR says nothing about what came after it.
		if git.HasChangesIn(wt.Path) {
			skippedDirty = append(skippedDirty, staleWorktree{wt.Path, wt.Branch, reason, "has uncommitted changes"})
			continue
		}
		if n := git.UnpushedCountIn(wt.Path); n > 0 {
			skippedDirty = append(skippedDirty, staleWorktree{wt.Path, wt.Branch, reason, fmt.Sprintf("has %d unpushed commit(s)", n)})
			continue
		}

		stale = append(stale, staleWorktree{Path: wt.Path, Branch: wt.Branch, Reason: reason})
	}

	git.PruneWorktrees()

	spin.Stop()

	if local {
		why := "gh is not installed"
		switch {
		case ghErr != nil && isNotGitHubRemote(ghErr):
			why = "the remote isn't on GitHub"
		case ghErr != nil:
			why = fmt.Sprintf("could not fetch PR data (%s)", ui.Truncate(ghErr.Error(), 80))
		}
		ui.Warn("%s, so only branches merged into %s count as stale.", why, ctx.baseRef())
		fmt.Println("   Squash-merged branches can't be detected without gh.")
		fmt.Println()
	} else if partial {
		ui.Warn("Could not fetch some PR data %s results may be incomplete", ui.Dash)
	}

	if len(stale) == 0 {
		ui.Success("No stale worktrees found")
		printSkippedDirty(skippedDirty)
		return nil
	}

	fmt.Printf("Found %d stale worktree(s):\n\n", len(stale))

	// Dry-run: list what would be removed and exit
	if pruneDryRun {
		for _, s := range stale {
			short := filepath.Base(s.Path)
			fmt.Printf("  %s  %s\n", ui.Yellow(short), s.Reason)
			fmt.Printf("     %s\n", ui.Dim(s.Branch))
		}
		printSkippedDirty(skippedDirty)
		fmt.Println()
		fmt.Println("No changes made (--dry-run)")
		return nil
	}

	// Phase 1: collect decisions
	toRemove, cancelled, err := selectPruneTargets(stale)
	if err != nil {
		return err
	}
	if cancelled {
		fmt.Println("Cancelled")
		return nil
	}

	// Phase 2: execute removals
	if len(toRemove) == 0 {
		fmt.Println("No worktrees to remove")
		return nil
	}

	warnRepoHooks(ctx)
	fmt.Printf("Removing %d worktree(s)...\n", len(toRemove))

	removedCount := 0
	for _, i := range toRemove {
		s := stale[i]
		short := filepath.Base(s.Path)

		if _, err := closeWorktree(ctx, s.Path, s.Branch, false); err != nil {
			fmt.Printf("  %s %s (failed: %s)\n", ui.Red(ui.Fail), short, firstLine(err.Error()))
			continue
		}
		fmt.Printf("  %s %s\n", ui.Green(ui.Pass), short)
		removedCount++
	}

	fmt.Println()
	ui.Success("Removed %d worktree(s)", removedCount)
	printSkippedDirty(skippedDirty)
	ui.PrintCTA("wt list")
	return nil
}

// prReasons maps each branch with a merged or closed PR, and no open one, to a
// reason. partial is true when the merged or the closed lookup failed; err is
// set when both did, or when the open lookup did: without it a reused branch
// with a new open PR would look stale.
func prReasons(branches []string) (reasons map[string]string, partial bool, err error) {
	reasons = make(map[string]string)
	if !github.IsAvailable() || len(branches) == 0 {
		return reasons, false, nil
	}

	var openPRs, mergedPRs, closedPRs []github.PR
	var openErr, mergedErr, closedErr error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); openPRs, openErr = github.ListPRs("open", branches...) }()
	go func() { defer wg.Done(); mergedPRs, mergedErr = github.ListPRs("merged", branches...) }()
	go func() { defer wg.Done(); closedPRs, closedErr = github.ListPRs("closed", branches...) }()
	wg.Wait()

	if openErr != nil {
		return nil, false, openErr
	}
	if mergedErr != nil && closedErr != nil {
		return nil, false, mergedErr
	}

	for _, b := range branches {
		if github.FindPRForBranch(openPRs, b) != nil {
			continue
		}
		if pr := github.FindPRForBranch(mergedPRs, b); pr != nil {
			reasons[b] = fmt.Sprintf("PR #%d merged", pr.Number)
		} else if pr := github.FindPRForBranch(closedPRs, b); pr != nil {
			reasons[b] = fmt.Sprintf("PR #%d closed", pr.Number)
		}
	}
	return reasons, mergedErr != nil || closedErr != nil, nil
}

// mergedReasons is the fallback without gh: a branch is stale when the base
// branch already contains it. Branches with no commits since creation are
// skipped, since git also counts them as merged.
func mergedReasons(ctx *cmdContext, branches []string) (map[string]string, error) {
	_ = git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch) // best effort; a stale base only misses branches
	merged, err := git.MergedBranches(ctx.baseRef())
	if err != nil {
		return nil, err
	}
	reasons := make(map[string]string)
	for _, b := range branches {
		if merged[b] && git.BranchMoved(b) {
			reasons[b] = "merged into " + ctx.baseRef()
		}
	}
	return reasons, nil
}

// selectPruneTargets returns the indices of stale worktrees the user wants to remove.
//
//   - With --yes: all stale worktrees.
//   - With a TTY: interactive multi-select via the TUI picker.
//   - Without a TTY and no --yes: nothing selected (preserves existing
//     non-interactive behavior, where each Confirm() returned false).
func selectPruneTargets(stale []staleWorktree) (indices []int, cancelled bool, err error) {
	if ui.YesFlag {
		all := make([]int, len(stale))
		for i := range stale {
			all[i] = i
		}
		return all, false, nil
	}

	if !ui.IsTTY() {
		return nil, false, nil
	}

	items := make([]tui.PruneItem, len(stale))
	for i, s := range stale {
		items[i] = tui.PruneItem{Path: s.Path, Branch: s.Branch, Reason: s.Reason}
	}

	result, err := tui.RunPrunePicker(items)
	if err != nil {
		return nil, false, err
	}
	return result.Selected, result.Cancelled, nil
}

func printSkippedDirty(skipped []staleWorktree) {
	if len(skipped) == 0 {
		return
	}
	fmt.Println()
	for _, s := range skipped {
		short := filepath.Base(s.Path)
		fmt.Printf("  %s %s — %s, skipped (%s)\n", ui.Yellow("!"), short, s.Reason, s.Kept)
	}
}
