package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var rebaseCmd = &cobra.Command{
	Use:   "rebase",
	Short: "Rebase current branch onto base branch (local only)",
	Long: `Rebase the current branch onto the configured base branch.

For feature branches: stash → fetch → rebase → restore stash.
For base/main branches: fast-forward merge only.

Use --all to rebase all worktrees at once.`,
	RunE: runRebase,
}

var (
	rebaseContinueFlag bool
	rebaseAbortFlag    bool
	rebaseAllFlag      bool
)

func init() {
	rebaseCmd.Flags().BoolVar(&rebaseContinueFlag, "continue", false, "resume after resolving conflicts")
	rebaseCmd.Flags().BoolVar(&rebaseAbortFlag, "abort", false, "abort rebase and restore state")
	rebaseCmd.Flags().BoolVarP(&rebaseAllFlag, "all", "a", false, "rebase all worktrees")
	rootCmd.AddCommand(rebaseCmd)
}

const stateFileName = "wt-rebase-state"

func runRebase(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	if rebaseAllFlag {
		return rebaseAll(ctx)
	}

	branch, err := git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("not in a git repository or detached HEAD")
	}

	if rebaseContinueFlag {
		return rebaseContinue()
	}
	if rebaseAbortFlag {
		return rebaseAbort()
	}

	// Base branch: fast-forward
	if branch == ctx.Config.BaseBranch || branch == "main" || branch == "master" {
		return rebaseBaseBranch(ctx, branch)
	}

	return rebaseFeatureBranch(ctx, branch)
}

func rebaseFeatureBranch(ctx *cmdContext, branch string) error {
	inProgress, _ := git.IsRebaseInProgress()
	if inProgress {
		return fmt.Errorf("rebase already in progress\n   Resolve conflicts and run: wt rebase --continue\n   Or abort with: wt rebase --abort")
	}

	fmt.Printf("Syncing branch: %s\n\n", branch)

	// Stash uncommitted changes
	didStash := "no"
	if git.HasChanges() {
		fmt.Println("Stashing uncommitted changes...")
		if err := git.StashPush("wt rebase: auto-stash"); err != nil {
			return fmt.Errorf("failed to stash changes: %w", err)
		}
		didStash = "stashed"
	}

	// Save state for --continue
	git.SaveStateFile(stateFileName, didStash)

	// Fetch base branch
	fmt.Printf("Fetching %s...\n", ctx.baseRef())
	if err := git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch); err != nil {
		restoreStash(didStash)
		git.RemoveStateFile(stateFileName)
		return fmt.Errorf("failed to fetch: %w", err)
	}

	// Check if up to date
	ab, err := git.GetAheadBehind(ctx.baseRef())
	if err == nil && ab.Behind == 0 {
		restoreStash(didStash)
		git.RemoveStateFile(stateFileName)
		ui.Success("Already up to date with %s", ctx.Config.BaseBranch)
		return nil
	}

	// Rebase
	fmt.Printf("Rebasing onto %s (%d commit(s) behind)...\n\n", ctx.baseRef(), ab.Behind)

	if err := git.Rebase(ctx.baseRef()); err != nil {
		fmt.Println()
		ui.Warn("Rebase paused due to conflicts")
		fmt.Println()
		fmt.Println("To resolve:")
		fmt.Println("  1. Fix the conflicts in the listed files")
		fmt.Println("  2. Stage the fixes: git add <files>")
		fmt.Printf("  3. Continue sync: %s\n", ui.Cyan("wt rebase --continue"))
		fmt.Println()
		fmt.Printf("Or abort: %s\n", ui.Yellow("wt rebase --abort"))
		return nil // Don't return error — user needs to resolve
	}

	restoreStash(didStash)
	git.RemoveStateFile(stateFileName)
	fmt.Println()
	ui.Success("Synced with %s", ctx.Config.BaseBranch)
	return nil
}

func rebaseBaseBranch(ctx *cmdContext, branch string) error {
	fmt.Printf("Syncing %s (fast-forward only)...\n\n", branch)

	didStash := "no"
	if git.HasChanges() {
		short, _ := git.StatusShort()
		ui.Warn("You have uncommitted changes:")
		fmt.Println(short)
		fmt.Println()
		if !ui.Confirm("Stash changes and continue?", true) {
			fmt.Println("Cancelled")
			return nil
		}
		fmt.Println()
		fmt.Println("Stashing changes...")
		if err := git.StashPush("wt rebase: auto-stash on " + branch); err != nil {
			return fmt.Errorf("failed to stash: %w", err)
		}
		didStash = "stashed"
		fmt.Println()
	}

	// Fetch
	fmt.Printf("Fetching %s/%s...\n", ctx.Config.Remote, branch)
	if err := git.Fetch(ctx.Config.Remote, branch); err != nil {
		restoreStash(didStash)
		return fmt.Errorf("failed to fetch: %w", err)
	}

	remoteRef := ctx.Config.Remote + "/" + branch
	ab, _ := git.GetAheadBehind(remoteRef)
	if ab.Behind == 0 {
		restoreStash(didStash)
		ui.Success("Already up to date")
		return nil
	}

	fmt.Printf("Fast-forwarding (%d commit(s) behind)...\n", ab.Behind)
	if _, err := git.MergeFF(remoteRef); err != nil {
		restoreStash(didStash)
		return fmt.Errorf("cannot fast-forward: %w", err)
	}

	restoreStash(didStash)
	fmt.Println()
	ui.Success("%s synced!", branch)
	return nil
}

func rebaseContinue() error {
	if !git.StateFileExists(stateFileName) {
		return fmt.Errorf("no sync in progress")
	}

	inProgress, _ := git.IsRebaseInProgress()
	if inProgress {
		fmt.Println("Continuing rebase...")
		if err := git.RebaseContinue(); err != nil {
			fmt.Println()
			ui.Warn("Still have conflicts. Resolve them and run:")
			fmt.Printf("  %s\n", ui.Cyan("wt rebase --continue"))
			return nil
		}
	}

	didStash, _ := git.ReadStateFile(stateFileName)
	restoreStash(didStash)
	git.RemoveStateFile(stateFileName)
	fmt.Println()
	ui.Success("Synced with base branch")
	return nil
}

func rebaseAbort() error {
	inProgress, _ := git.IsRebaseInProgress()
	if inProgress {
		git.RebaseAbort()
	}

	if git.StateFileExists(stateFileName) {
		didStash, _ := git.ReadStateFile(stateFileName)
		restoreStash(didStash)
		git.RemoveStateFile(stateFileName)
	}

	ui.Success("Rebase aborted")
	return nil
}

func rebaseAll(ctx *cmdContext) error {
	worktrees, err := git.ListWorktrees()
	if err != nil {
		return err
	}

	fmt.Println("Rebasing all worktrees onto", ctx.Config.BaseBranch+"...")
	fmt.Println()

	fmt.Printf("Fetching %s...\n\n", ctx.baseRef())
	git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch)

	var rebased, skipped, uptodate, failed int

	for _, wt := range worktrees {
		branch, _ := git.CurrentBranchIn(wt.Path)
		if branch == ctx.Config.BaseBranch || branch == "main" || branch == "master" {
			continue
		}

		short := filepath.Base(wt.Path)
		fmt.Printf("  %-25s ", short)

		if git.HasChangesIn(wt.Path) {
			fmt.Printf("%s\n", ui.Yellow("⚠ has uncommitted changes (skipped)"))
			skipped++
			continue
		}

		ab, err := git.GetAheadBehindIn(wt.Path, ctx.baseRef())
		if err != nil || ab.Behind == 0 {
			fmt.Printf("%s\n", ui.Green("✓ up to date"))
			uptodate++
			continue
		}

		if err := git.RebaseIn(wt.Path, ctx.baseRef()); err != nil {
			git.RebaseAbortIn(wt.Path)
			fmt.Printf("%s\n", ui.Red("✗ conflicts (aborted, rebase manually)"))
			failed++
		} else {
			fmt.Printf("%s\n", ui.Green(fmt.Sprintf("✓ rebased (%d commits from %s)", ab.Behind, ctx.Config.BaseBranch)))
			rebased++
		}
	}

	fmt.Println()
	fmt.Println("Summary:")
	if rebased > 0 {
		fmt.Printf("  %s\n", ui.Green(fmt.Sprintf("✓ %d rebased", rebased)))
	}
	if uptodate > 0 {
		fmt.Printf("  ✓ %d already up to date\n", uptodate)
	}
	if skipped > 0 {
		fmt.Printf("  %s\n", ui.Yellow(fmt.Sprintf("⚠ %d skipped (uncommitted changes)", skipped)))
	}
	if failed > 0 {
		fmt.Printf("  %s\n", ui.Red(fmt.Sprintf("✗ %d failed (conflicts)", failed)))
	}
	return nil
}

func restoreStash(state string) {
	if state == "stashed" {
		fmt.Println("Restoring stashed changes...")
		git.StashPop()
	}
}
