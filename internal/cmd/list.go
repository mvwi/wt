package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	Path      string
	ShortName string
	Branch    string
	IsCurrent bool
	Age       string
	Behind    int
	Ahead     int
}

func runList(cmd *cobra.Command, args []string) error {
	ctx, err := newContext()
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()
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
		short := filepath.Base(wt.Path)
		// Strip configured worktree prefix
		prefix := ctx.Config.EffectiveWorktreeDir(ctx.RepoName, "")
		short = strings.TrimPrefix(short, prefix)

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

		if branch != ctx.Config.BaseBranch && branch != "main" && branch != "master" {
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
	for _, info := range infos {
		if info.IsCurrent {
			fmt.Printf("%s ", ui.Yellow(ui.Current))
		} else {
			fmt.Print("  ")
		}

		fmt.Print(info.ShortName)
		if info.Branch != info.ShortName {
			fmt.Printf("  %s", ui.Dim(info.Branch))
		}
		fmt.Println()
	}

	// Phase 2: PR status for feature branches
	if len(featureBranches) > 0 && github.IsAvailable() {
		fmt.Println()
		fmt.Printf("  %s", ui.Dim("loading..."))

		openPRs, _ := github.ListPRs("open")
		mergedPRs, _ := github.ListPRs("merged")

		// Clear loading indicator
		fmt.Print("\r\033[K")

		ui.DimF("  %-28s %-4s %-8s %-6s %-8s %s\n", "Branch", "Age", "Sync", "PR", "Review", "CI")
		ui.DimF("  %s\n", strings.Repeat("─", 68))

		hasStale := false

		for _, info := range infos {
			if info.Branch == ctx.Config.BaseBranch || info.Branch == "main" || info.Branch == "master" {
				continue
			}

			branchDisplay := ui.Truncate(info.Branch, 28)
			fmt.Printf("  %-28s ", branchDisplay)

			// Age
			ui.CyanF("%-4s ", info.Age)

			// Sync
			syncStr := buildSyncStr(info.Behind, info.Ahead)
			if info.Behind > 0 {
				ui.YellowF("%-8s ", syncStr)
			} else {
				ui.GreenF("%-8s ", syncStr)
			}

			// PR status
			openPR := github.FindPRForBranch(openPRs, info.Branch)
			mergedPR := github.FindPRForBranch(mergedPRs, info.Branch)

			if openPR != nil {
				printOpenPRStatus(openPR, info.Path)
			} else if mergedPR != nil {
				ui.BlueF("%-6s ", fmt.Sprintf("#%d", mergedPR.Number))
				fmt.Printf("%s   %s", ui.Green("merged"), ui.Yellow("stale"))
				hasStale = true
				fmt.Println()
			} else {
				fmt.Println(ui.Dim(ui.Dash))
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
