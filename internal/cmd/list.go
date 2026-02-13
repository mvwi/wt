package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	GroupID: groupWorkflow,
	Short:   "Show all worktrees with PR status",
	Long: `Show all worktrees with branch info, PR status, reviews, and CI checks.

Output is in two phases:
  1. Worktree names and branches (instant)
  2. PR status table with reviews and CI (requires GitHub API)`,
	RunE: runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

type worktreeInfo struct {
	Path       string
	ShortName  string
	Branch     string
	IsCurrent  bool
	Age        string
	Behind     int
	Ahead      int
	DirtyCount int
}

func runList(cmd *cobra.Command, args []string) error {
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

	if len(worktrees) == 0 {
		fmt.Println("No worktrees found")
		return nil
	}

	// Collect info
	var infos []worktreeInfo
	var featureBranches []string

	for _, wt := range worktrees {
		short := ctx.shortName(wt.Path)

		branch := wt.Branch
		if branch == "" {
			branch, _ = git.CurrentBranchIn(wt.Path)
		}

		isCurrent := wt.Path == cwd || isSubpath(cwd, wt.Path)

		info := worktreeInfo{
			Path:      wt.Path,
			ShortName: short,
			Branch:    branch,
			IsCurrent: isCurrent,
		}

		// Dirty count for all worktrees
		if changes, err := git.StatusPorcelainIn(wt.Path); err == nil {
			info.DirtyCount = len(changes)
		}

		if !ctx.isBaseBranch(branch) {
			info.Age = git.LastCommitAge(wt.Path)
			ab, err := git.GetAheadBehindIn(wt.Path, ctx.baseRef())
			if err == nil {
				info.Behind = ab.Behind
				info.Ahead = ab.Ahead
			}
			featureBranches = append(featureBranches, branch)
		}

		infos = append(infos, info)
	}

	// Phase 1: Show worktree names immediately
	ui.Header("WORKTREES")

	// Calculate column width from longest short name
	nameWidth := 0
	for _, info := range infos {
		if len(info.ShortName) > nameWidth {
			nameWidth = len(info.ShortName)
		}
	}
	if nameWidth < 8 {
		nameWidth = 8
	}

	ui.DimF("  %-*s %s\n", nameWidth+2, "Name", "Branch")
	ui.DimF("  %s\n", strings.Repeat("─", nameWidth+2+28))

	for _, info := range infos {
		if info.IsCurrent {
			fmt.Printf("%s ", ui.Yellow(ui.Current))
		} else {
			fmt.Print("  ")
		}

		fmt.Printf("%-*s ", nameWidth, info.ShortName)

		if info.Branch != info.ShortName {
			fmt.Print(ui.Dim(info.Branch))
		}

		fmt.Println()
	}

	// Phase 2: PR status for feature branches
	if len(featureBranches) > 0 && github.IsAvailable() {
		fmt.Println()
		spin := ui.NewSpinner("Loading PR status")

		// Fetch open, merged, and closed PRs in parallel
		var openPRs, mergedPRs, closedPRs []github.PR
		var openErr, mergedErr, closedErr error
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); openPRs, openErr = github.ListPRs("open") }()
		go func() { defer wg.Done(); mergedPRs, mergedErr = github.ListPRs("merged") }()
		go func() { defer wg.Done(); closedPRs, closedErr = github.ListPRs("closed") }()
		wg.Wait()

		spin.Stop()

		if openErr != nil || mergedErr != nil || closedErr != nil {
			ui.Warn("Could not fetch some PR data — status may be incomplete")
		}

		staleThreshold := ctx.Config.EffectiveStaleThreshold()

		ui.DimF("  %-28s %-4s %-6s %-8s %-6s %-8s %s\n", "Branch", "Age", "Dirty", "Sync", "PR", "Review", "CI")
		ui.DimF("  %s\n", strings.Repeat("─", 76))

		hasStale := false

		for _, info := range infos {
			if ctx.isBaseBranch(info.Branch) {
				continue
			}

			openPR := github.FindPRForBranch(openPRs, info.Branch)
			mergedPR := github.FindPRForBranch(mergedPRs, info.Branch)
			closedPR := github.FindPRForBranch(closedPRs, info.Branch)

			// Check staleness: old commit + no open PR
			daysAgo := git.LastCommitDaysAgo(info.Path)
			isStale := daysAgo >= staleThreshold && openPR == nil

			// Dim the entire row if stale
			colorize := fmt.Sprintf
			if isStale {
				colorize = func(f string, a ...any) string {
					return ui.Dim(fmt.Sprintf(f, a...))
				}
			}

			branchDisplay := ui.Truncate(info.Branch, 28)
			fmt.Printf("  %s ", colorize("%-28s", branchDisplay))

			// Age
			fmt.Printf("%s ", colorize("%-4s", info.Age))

			// Dirty
			if info.DirtyCount > 0 {
				if isStale {
					fmt.Printf("%s ", colorize("%-6d", info.DirtyCount))
				} else {
					ui.YellowF("%-6d ", info.DirtyCount)
				}
			} else {
				fmt.Printf("%s ", ui.Dim(fmt.Sprintf("%-6s", ui.Dash)))
			}

			// Sync
			syncStr := buildSyncStr(info.Behind, info.Ahead)
			switch {
			case isStale:
				fmt.Printf("%s ", colorize("%-8s", syncStr))
			case info.Behind > 0:
				ui.YellowF("%-8s ", syncStr)
			default:
				ui.GreenF("%-8s ", syncStr)
			}

			// PR status
			switch {
			case openPR != nil:
				printOpenPRStatus(openPR, info.Path)
			case mergedPR != nil:
				ui.BlueF("%-6s ", fmt.Sprintf("#%d", mergedPR.Number))
				fmt.Printf("%s   %s", ui.Green("merged"), ui.Yellow("stale"))
				hasStale = true
				fmt.Println()
			case closedPR != nil:
				ui.BlueF("%-6s ", fmt.Sprintf("#%d", closedPR.Number))
				fmt.Printf("%s  %s", ui.Red("closed"), ui.Yellow("stale"))
				hasStale = true
				fmt.Println()
			default:
				if isStale {
					fmt.Print(ui.Dim(ui.Dash))
					fmt.Print(" \U0001f4a4") // 💤
				} else {
					fmt.Print(ui.Dim(ui.Dash))
				}
				fmt.Println()
			}
		}

		// Legend
		fmt.Println()
		ui.DimF("  %s pending  %s changes/fail  %s pass\n", ui.Pending, ui.Fail, ui.Pass)

		if hasStale {
			fmt.Printf("  %s\n", ui.Yellow("Tip: run 'wt prune' to clean up stale worktrees"))
		}
	}

	fmt.Println()
	return nil
}

func buildSyncStr(behind, ahead int) string {
	var parts []string
	if behind > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", ui.ArrowDown, behind))
	}
	if ahead > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", ui.ArrowUp, ahead))
	}
	if len(parts) == 0 {
		return ui.Pass
	}
	return strings.Join(parts, "")
}

func printOpenPRStatus(pr *github.PR, wtPath string) {
	// PR number
	ui.BlueF("%-6s ", fmt.Sprintf("#%d", pr.Number))

	// Review glyphs
	rs := pr.GetReviewSummary()
	reviewWidth := 8
	reviewCount := 0

	if rs.Approved > 0 {
		fmt.Print(ui.Green(strings.Repeat(ui.Pass, rs.Approved)))
		reviewCount += rs.Approved
	}
	if rs.Changes > 0 {
		fmt.Print(ui.Red(strings.Repeat(ui.Fail, rs.Changes)))
		reviewCount += rs.Changes
	}
	if rs.Pending > 0 {
		fmt.Print(ui.Blue(strings.Repeat(ui.Pending, rs.Pending)))
		reviewCount += rs.Pending
	}
	if reviewCount == 0 {
		fmt.Print(ui.Dim(ui.NoReview))
		reviewCount = 1
	}

	// Pad to fixed width
	if pad := reviewWidth - reviewCount; pad > 0 {
		fmt.Printf("%*s", pad, "")
	}
	fmt.Print(" ")

	// CI glyphs
	cs := pr.GetCISummary()
	if cs.Pass > 0 {
		fmt.Print(ui.Green(strings.Repeat(ui.Pass, cs.Pass)))
	}
	if cs.Fail > 0 {
		fmt.Print(ui.Red(strings.Repeat(ui.Fail, cs.Fail)))
	}
	if cs.Pending > 0 {
		fmt.Print(ui.Yellow(strings.Repeat(ui.Pending, cs.Pending)))
	}
	if cs.Total == 0 {
		fmt.Print(ui.Dim(ui.NoReview))
	}

	// Unpushed indicator
	unpushed := git.UnpushedCountIn(wtPath)
	if unpushed > 0 {
		fmt.Printf(" %s", ui.Magenta(fmt.Sprintf("%s%d", ui.PushUp, unpushed)))
	}

	fmt.Println()
}
