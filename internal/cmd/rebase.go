package cmd

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var rebaseCmd = &cobra.Command{
	Use:     "rebase",
	GroupID: groupSync,
	Short:   "Rebase current branch onto base branch (local only)",
	Long: `Rebase the current branch onto the configured base branch.

For feature branches: stash → fetch → rebase → restore stash.
For base/main branches: fast-forward merge only.

Use --all to rebase all worktrees at once.`,
	Example: `  wt rebase               Rebase current branch onto base branch
  wt rebase --all          Rebase all worktrees at once
  wt rebase --continue     Resume after resolving conflicts
  wt rebase --abort        Abort rebase and restore state`,
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

type rebaseOpts struct {
	continueRebase bool
	abort          bool
	all            bool
}

func runRebase(cmd *cobra.Command, args []string) error {
	return runRebaseWith(rebaseOpts{
		continueRebase: rebaseContinueFlag,
		abort:          rebaseAbortFlag,
		all:            rebaseAllFlag,
	})
}

func runRebaseWith(opts rebaseOpts) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	if opts.all && (opts.continueRebase || opts.abort) {
		return fmt.Errorf("--all cannot be combined with --continue or --abort")
	}

	if opts.all {
		return rebaseAll(ctx)
	}

	branch, err := git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("not in a git repository or detached HEAD\n   Run this from inside a worktree")
	}

	if opts.continueRebase {
		return rebaseContinue(ctx)
	}
	if opts.abort {
		return rebaseAbort()
	}

	// Base branch: fast-forward
	if ctx.isBaseBranch(branch) {
		return rebaseBaseBranch(ctx, branch)
	}

	return rebaseFeatureBranch(ctx, branch)
}

func rebaseFeatureBranch(ctx *cmdContext, branch string) error {
	inProgress, _ := git.IsRebaseInProgress()
	if inProgress {
		return fmt.Errorf("rebase already in progress\n   Resolve conflicts and run: wt rebase --continue\n   Or abort with: wt rebase --abort")
	}

	preRef, _ := git.RevParseHead()

	fmt.Printf("Syncing branch: %s\n\n", branch)

	// Stash uncommitted changes
	didStash := false
	if git.HasChanges() {
		fmt.Println("Stashing uncommitted changes...")
		if err := git.StashPush("wt rebase: auto-stash"); err != nil {
			return fmt.Errorf("failed to stash changes: %w", err)
		}
		didStash = true
	}

	// Save state for --continue (format: "stash_state:pre_ref")
	stashState := "no"
	if didStash {
		stashState = "stashed"
	}
	_ = git.SaveStateFile(stateFileName, stashState+":"+preRef)

	// Fetch base branch
	spin := ui.NewSpinner(fmt.Sprintf("Fetching %s", ctx.baseRef()))
	if err := git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch); err != nil {
		spin.Stop()
		restoreStash(didStash)
		git.RemoveStateFile(stateFileName)
		return fmt.Errorf("failed to fetch from remote: %w", err)
	}

	spin.Stop()

	// Check if up to date
	ab, err := git.GetAheadBehind(ctx.baseRef())
	if err == nil && ab.Behind == 0 {
		restoreStash(didStash)
		git.RemoveStateFile(stateFileName)
		ui.Success("Already up to date with %s", ctx.Config.BaseBranch)
		return nil
	}

	// Conflict preview
	conflicts, conflictErr := git.PotentialConflicts(ctx.baseRef())
	if conflictErr == nil && len(conflicts) > 0 {
		fmt.Println()
		ui.Warn("These files were modified on both branches:")
		for _, f := range conflicts {
			fmt.Printf("    %s\n", f)
		}
		fmt.Println()
		if !ui.Confirm("Conflicts are possible. Continue?", true) {
			restoreStash(didStash)
			git.RemoveStateFile(stateFileName)
			fmt.Println("Cancelled")
			return nil
		}
		fmt.Println()
	}

	// Rebase
	fmt.Printf("Rebasing onto %s (%d commit(s) behind)...\n\n", ctx.baseRef(), ab.Behind)

	if err := git.Rebase(ctx.baseRef()); err != nil {
		fmt.Println()
		ui.Warn("Rebase paused due to conflicts")
		fmt.Println()
		if didStash {
			fmt.Println("Your uncommitted changes are stashed and will be restored")
			fmt.Println("after --continue or --abort.")
			fmt.Println()
		}
		fmt.Println("To resolve:")
		fmt.Println("  1. Fix the conflicts in the listed files")
		fmt.Println("  2. Stage the fixes: git add <files>")
		fmt.Printf("  3. Continue sync: %s\n", ui.Cyan("wt rebase --continue"))
		fmt.Println()
		fmt.Printf("Or abort: %s\n", ui.Yellow("wt rebase --abort"))
		ui.PrintCTA("wt rebase --continue", "wt rebase --abort")
		return nil // Don't return error — user needs to resolve
	}

	restoreStash(didStash)
	git.RemoveStateFile(stateFileName)
	autoInstallIfNeeded(ctx, preRef)
	fmt.Println()
	ui.Success("Synced with %s", ctx.Config.BaseBranch)
	return nil
}

func rebaseBaseBranch(ctx *cmdContext, branch string) error {
	preRef, _ := git.RevParseHead()

	fmt.Printf("Syncing %s (fast-forward only)...\n\n", branch)

	didStash := false
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
		didStash = true
		fmt.Println()
	}

	// Fetch
	spin := ui.NewSpinner(fmt.Sprintf("Fetching %s/%s", ctx.Config.Remote, branch))
	if err := git.Fetch(ctx.Config.Remote, branch); err != nil {
		spin.Stop()
		restoreStash(didStash)
		return fmt.Errorf("failed to fetch from remote: %w", err)
	}

	spin.Stop()

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
		return fmt.Errorf("cannot fast-forward %s: %w", branch, err)
	}

	restoreStash(didStash)
	autoInstallIfNeeded(ctx, preRef)
	fmt.Println()
	ui.Success("%s synced!", branch)
	return nil
}

func rebaseContinue(ctx *cmdContext) error {
	if !git.StateFileExists(stateFileName) {
		return fmt.Errorf("no rebase in progress\n   Start one with: wt rebase")
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

	didStash, preRef := parseRebaseState()
	restoreStash(didStash)
	git.RemoveStateFile(stateFileName)

	if preRef != "" {
		autoInstallIfNeeded(ctx, preRef)
	}

	fmt.Println()
	ui.Success("Synced with base branch")
	return nil
}

func rebaseAbort() error {
	inProgress, _ := git.IsRebaseInProgress()
	hasState := git.StateFileExists(stateFileName)

	if !inProgress && !hasState {
		return fmt.Errorf("no rebase in progress\n   Start one with: wt rebase")
	}

	if inProgress {
		if err := git.RebaseAbort(); err != nil {
			return fmt.Errorf("failed to abort rebase: %w", err)
		}
	}

	if hasState {
		didStash, _ := parseRebaseState()
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

	spin := ui.NewSpinner(fmt.Sprintf("Fetching %s", ctx.baseRef()))
	if err := git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch); err != nil {
		spin.Stop()
		ui.Warn("Fetch failed: %v", err)
	} else {
		spin.Stop()
	}
	fmt.Println()

	var rebased, skipped, uptodate, failed int

	for _, wt := range worktrees {
		branch, _ := git.CurrentBranchIn(wt.Path)
		if ctx.isBaseBranch(branch) {
			short := ctx.shortName(wt.Path)
			fmt.Printf("  %-25s ", short)
			remoteRef := ctx.Config.Remote + "/" + ctx.Config.BaseBranch
			ab, err := git.GetAheadBehindIn(wt.Path, remoteRef)
			if err != nil || ab.Behind == 0 {
				fmt.Printf("%s\n", ui.Green("✓ up to date"))
				uptodate++
			} else {
				preRef, _ := git.RevParseHeadIn(wt.Path)
				if err := git.MergeFFIn(wt.Path, remoteRef); err != nil {
					fmt.Printf("%s\n", ui.Red("✗ fast-forward failed"))
					failed++
				} else {
					msg := fmt.Sprintf("✓ fast-forwarded (%d commits)", ab.Behind)
					suffix := lockfileChangedSuffix(wt.Path, preRef)
					fmt.Printf("%s%s\n", ui.Green(msg), suffix)
					rebased++
				}
			}
			continue
		}

		short := ctx.shortName(wt.Path)
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

		preRef, _ := git.RevParseHeadIn(wt.Path)
		if err := git.RebaseIn(wt.Path, ctx.baseRef()); err != nil {
			git.RebaseAbortIn(wt.Path)
			fmt.Printf("%s\n", ui.Red("✗ conflicts (aborted, rebase manually)"))
			failed++
		} else {
			msg := fmt.Sprintf("✓ rebased (%d commits from %s)", ab.Behind, ctx.Config.BaseBranch)
			suffix := lockfileChangedSuffix(wt.Path, preRef)
			fmt.Printf("%s%s\n", ui.Green(msg), suffix)
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

// parseRebaseState reads the state file and returns whether changes were stashed
// and the pre-rebase HEAD ref. Format: "stash_state:pre_ref" (e.g., "stashed:abc123").
// Backwards-compatible with old format that had no colon.
func parseRebaseState() (didStash bool, preRef string) {
	state, _ := git.ReadStateFile(stateFileName)
	parts := strings.SplitN(state, ":", 2)
	didStash = parts[0] == "stashed"
	if len(parts) == 2 {
		preRef = parts[1]
	}
	return
}

func restoreStash(didStash bool) {
	if didStash {
		fmt.Println("Restoring stashed changes...")
		if err := git.StashPop(); err != nil {
			ui.Warn("Failed to restore stash — run 'git stash pop' manually")
		}
	}
}

// lockfileChangedSuffix checks if a lockfile changed in a worktree between
// preRef and HEAD. Returns a warning suffix string, or "" if unchanged.
func lockfileChangedSuffix(dir, preRef string) string {
	if preRef == "" {
		return ""
	}
	lockfile, _ := detectInstallCommand(dir)
	if lockfile == "" {
		return ""
	}
	changed, err := git.DiffNameOnlyIn(dir, preRef, "HEAD")
	if err != nil {
		return ""
	}
	if slices.Contains(changed, lockfile) {
		return ui.Yellow(" — lockfile changed, run wt init")
	}
	return ""
}

// autoInstallIfNeeded checks if a lockfile changed between preRef and HEAD,
// and if so, runs the detected install command. Warns on failure but never
// returns an error (the rebase already succeeded).
func autoInstallIfNeeded(ctx *cmdContext, preRef string) {
	if !ctx.Config.EffectiveAutoInstall() {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		return
	}

	lockfile, command := detectInstallCommand(cwd)
	if lockfile == "" {
		return
	}

	changed, err := git.DiffNameOnly(preRef, "HEAD")
	if err != nil {
		return
	}

	if !slices.Contains(changed, lockfile) {
		return
	}

	fmt.Println()
	fmt.Printf("Lockfile changed (%s) — running install...\n", lockfile)
	fmt.Printf("  %s %s\n", ui.Dim("$"), command)
	if err := runShellString(cwd, command); err != nil {
		fmt.Println()
		ui.Warn("Install failed — run manually: %s", command)
	}
}
