package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:     "watch",
	GroupID: groupSync,
	Short:   "Watch PR until mergeable or blocked",
	Long: `Poll the GitHub API every 15 seconds, showing a live spinner with CI and
review status, and exit when the PR reaches a terminal state.

Exits successfully when the PR is ready to merge. Exits with an error on
merge conflicts, CI failures, or changes requested.

Requires the GitHub CLI (gh) to be installed.`,
	RunE: runWatch,
}

func init() {
	rootCmd.AddCommand(watchCmd)
}

// watchResult describes why the watch loop ended.
type watchResult struct {
	success bool
	message string
}

func runWatch(cmd *cobra.Command, args []string) error {
	if !github.IsAvailable() {
		return fmt.Errorf("gh CLI is required for this command (brew install gh)")
	}

	ctx, err := newContext()
	if err != nil {
		return err
	}

	branch, err := git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("not in a git repository or detached HEAD")
	}

	if ctx.isBaseBranch(branch) {
		return fmt.Errorf("no PR to watch %s the base branch (%s) has no associated PR", ui.Dash, branch)
	}

	// Initial fetch to verify PR exists
	ws, err := github.GetWatchStatus(branch)
	if err != nil {
		return fmt.Errorf("no open PR found for branch %s %s run wt submit to push and create one", branch, ui.Dash)
	}

	fmt.Printf("Watching PR #%d %s %s\n", ws.Number, ui.Dash, ui.Dim(ws.HeadRefName))

	// Check if already resolved
	if res := checkResolved(ws); res != nil {
		fmt.Println()
		printFinalStatus(ws)
		if res.success {
			return nil
		}
		return fmt.Errorf("%s", res.message)
	}

	// Set up Ctrl+C handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	// Poll loop
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	spin := ui.NewSpinnerRich(buildSpinnerMessage(ws))

	for {
		select {
		case <-sigCh:
			spin.Stop()
			fmt.Println()
			return nil

		case <-ticker.C:
			ws, err = github.GetWatchStatus(branch)
			if err != nil {
				spin.Stop()
				return fmt.Errorf("failed to fetch PR status: %w", err)
			}

			if res := checkResolved(ws); res != nil {
				spin.Stop()
				fmt.Println()
				printFinalStatus(ws)
				if res.success {
					return nil
				}
				return fmt.Errorf("%s", res.message)
			}

			// Update spinner with new status
			spin.Stop()
			spin = ui.NewSpinnerRich(buildSpinnerMessage(ws))
		}
	}
}

// checkResolved returns a result if the PR has reached a terminal state, or nil to keep polling.
func checkResolved(ws *github.WatchStatus) *watchResult {
	// Ready to merge
	if ws.MergeStateStatus == "CLEAN" {
		return &watchResult{success: true, message: "Ready to merge"}
	}

	// Merge conflict
	if ws.Mergeable == "CONFLICTING" {
		return &watchResult{success: false, message: "Merge conflict " + ui.Dash + " run wt rebase"}
	}

	// Changes requested
	if ws.ReviewDecision == "CHANGES_REQUESTED" {
		return &watchResult{success: false, message: "Changes requested"}
	}

	// Draft PR
	if ws.MergeStateStatus == "DRAFT" {
		return &watchResult{success: false, message: "PR is a draft " + ui.Dash + " mark as ready first"}
	}

	// All CI resolved with failures
	cs := ws.GetCISummary()
	if cs.Total > 0 && cs.Pending == 0 && cs.Fail > 0 {
		return &watchResult{
			success: false,
			message: fmt.Sprintf("%d check(s) failed", cs.Fail),
		}
	}

	// All CI resolved, all passed, but still BLOCKED (branch protection rules)
	if cs.Total > 0 && cs.Pending == 0 && cs.Fail == 0 && ws.MergeStateStatus == "BLOCKED" {
		return &watchResult{success: false, message: "Blocked by branch protection"}
	}

	return nil
}

// buildSpinnerMessage creates a glyph-based status string matching wt list's style.
func buildSpinnerMessage(ws *github.WatchStatus) string {
	cs := ws.GetCISummary()
	rs := ws.GetReviewSummary()

	var parts []string

	// Review glyphs (same colors as printOpenPRStatus in list.go)
	reviewTotal := rs.Approved + rs.Changes + rs.Pending
	if reviewTotal > 0 {
		glyphs := ""
		if rs.Approved > 0 {
			glyphs += ui.Green(strings.Repeat(ui.Pass, rs.Approved))
		}
		if rs.Changes > 0 {
			glyphs += ui.Red(strings.Repeat(ui.Fail, rs.Changes))
		}
		if rs.Pending > 0 {
			glyphs += ui.Blue(strings.Repeat(ui.Pending, rs.Pending))
		}
		parts = append(parts, ui.Dim("Review ")+glyphs)
	}

	// CI glyphs
	if cs.Total > 0 {
		glyphs := ""
		if cs.Pass > 0 {
			glyphs += ui.Green(strings.Repeat(ui.Pass, cs.Pass))
		}
		if cs.Fail > 0 {
			glyphs += ui.Red(strings.Repeat(ui.Fail, cs.Fail))
		}
		if cs.Pending > 0 {
			glyphs += ui.Yellow(strings.Repeat(ui.Pending, cs.Pending))
		}
		parts = append(parts, ui.Dim("CI ")+glyphs)
	}

	if len(parts) == 0 {
		return ui.Dim("Waiting for status...")
	}
	return strings.Join(parts, ui.Dim("  "))
}

// printFinalStatus outputs the detailed resolution display.
func printFinalStatus(ws *github.WatchStatus) {
	cs := ws.GetCISummary()
	rs := ws.GetReviewSummary()

	// CI line
	if cs.Total > 0 {
		fmt.Printf("  CI      ")
		if cs.Pass > 0 {
			fmt.Print(ui.Green(strings.Repeat(ui.Pass, cs.Pass)))
		}
		if cs.Fail > 0 {
			fmt.Print(ui.Red(strings.Repeat(ui.Fail, cs.Fail)))
		}
		if cs.Pending > 0 {
			fmt.Print(ui.Yellow(strings.Repeat(ui.Pending, cs.Pending)))
		}

		// Summary text
		var ciParts []string
		if cs.Fail > 0 {
			ciParts = append(ciParts, fmt.Sprintf("%d/%d passed, %d failed", cs.Pass, cs.Total, cs.Fail))
		} else if cs.Pending > 0 {
			ciParts = append(ciParts, fmt.Sprintf("%d/%d passed, %d pending", cs.Pass, cs.Total, cs.Pending))
		} else {
			ciParts = append(ciParts, fmt.Sprintf("%d/%d passed", cs.Pass, cs.Total))
		}
		fmt.Printf("  %s\n", ui.Dim(strings.Join(ciParts, "")))

		// List failed checks
		for _, name := range ws.FailedCheckNames() {
			fmt.Printf("    %s %s\n", ui.Red(ui.Fail), name)
		}
	}

	// Review line
	reviewTotal := rs.Approved + rs.Changes + rs.Pending
	if reviewTotal > 0 {
		fmt.Printf("  Review  ")
		if rs.Approved > 0 {
			fmt.Print(ui.Green(strings.Repeat(ui.Pass, rs.Approved)))
		}
		if rs.Changes > 0 {
			fmt.Print(ui.Red(strings.Repeat(ui.Fail, rs.Changes)))
		}
		if rs.Pending > 0 {
			fmt.Print(ui.Blue(strings.Repeat(ui.Pending, rs.Pending)))
		}

		var rParts []string
		if rs.Approved > 0 {
			rParts = append(rParts, fmt.Sprintf("%d approved", rs.Approved))
		}
		if rs.Changes > 0 {
			rParts = append(rParts, fmt.Sprintf("%d changes requested", rs.Changes))
		}
		if rs.Pending > 0 {
			rParts = append(rParts, fmt.Sprintf("%d pending", rs.Pending))
		}
		fmt.Printf("  %s\n", ui.Dim(strings.Join(rParts, ", ")))
	}

	// Final verdict
	fmt.Println()
	switch {
	case ws.MergeStateStatus == "CLEAN":
		ui.Success("Ready to merge")
	case ws.Mergeable == "CONFLICTING":
		ui.Error("Merge conflict %s run wt rebase", ui.Dash)
	case ws.ReviewDecision == "CHANGES_REQUESTED":
		ui.Error("Changes requested")
	case ws.MergeStateStatus == "DRAFT":
		ui.Error("PR is a draft %s mark as ready first", ui.Dash)
	case cs.Fail > 0:
		ui.Error("%d check(s) failed", cs.Fail)
	case ws.MergeStateStatus == "BLOCKED":
		ui.Error("Blocked by branch protection")
	}
}
