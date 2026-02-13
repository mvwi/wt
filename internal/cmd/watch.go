package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

const watchCheckColWidth = 28

var watchCmd = &cobra.Command{
	Use:     "watch [branch or PR number]",
	GroupID: groupSync,
	Short:   "Watch PR until mergeable or blocked",
	Long: `Poll the GitHub API every 15 seconds, showing a live table with CI and
review status, and exit when the PR reaches a terminal state.

Optionally pass a branch name or PR number to watch any PR in the repo,
even if the branch isn't checked out locally.

Exits successfully when the PR is ready to merge. Exits with an error on
merge conflicts, CI failures, or changes requested.

Requires the GitHub CLI (gh) to be installed.`,
	Args: cobra.MaximumNArgs(1),
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

	// Resolve what to watch: explicit argument (branch name or PR number) or current branch
	var ref string
	if len(args) > 0 {
		ref = args[0]
	} else {
		ctx, err := newContext()
		if err != nil {
			return err
		}

		branch, err := git.CurrentBranch()
		if err != nil {
			return fmt.Errorf("not in a git repository or detached HEAD\n   Run this from inside a worktree")
		}

		if ctx.isBaseBranch(branch) {
			return fmt.Errorf("can't watch the base branch (%s) %s it has no associated PR\n   Switch to a feature worktree first", branch, ui.Dash)
		}
		ref = branch
	}

	// Initial fetch to verify PR exists
	ws, err := github.GetWatchStatus(ref)
	if err != nil {
		return fmt.Errorf("no open PR found for %s", ref)
	}

	// Print header (once)
	printWatchHeader(ws)

	// Check if already resolved
	if res := checkResolved(ws); res != nil {
		renderWatchTable(ws)
		printWatchVerdict(ws)
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

	tableLines := renderWatchTable(ws)
	spin := ui.NewSpinnerRich(buildSpinnerMessage(ws))

	for {
		select {
		case <-sigCh:
			spin.Stop()
			fmt.Println()
			return nil

		case <-ticker.C:
			ws, err = github.GetWatchStatus(ref)
			if err != nil {
				spin.Stop()
				return fmt.Errorf("failed to fetch PR status: %w", err)
			}

			// Clear and redraw
			spin.Stop()
			ui.ClearLines(tableLines)

			if res := checkResolved(ws); res != nil {
				renderWatchTable(ws)
				printWatchVerdict(ws)
				sendNotification(ws)
				if res.success {
					return nil
				}
				return fmt.Errorf("%s", res.message)
			}

			tableLines = renderWatchTable(ws)
			spin = ui.NewSpinnerRich(buildSpinnerMessage(ws))
		}
	}
}

// checkResolved returns a result if the PR has reached a terminal state, or nil to keep polling.
func checkResolved(ws *github.WatchStatus) *watchResult {
	// Already merged
	if ws.State == "MERGED" {
		return &watchResult{success: true, message: "Already merged"}
	}

	// Closed without merge
	if ws.State == "CLOSED" {
		return &watchResult{success: false, message: "PR was closed"}
	}

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

// printWatchHeader prints the PR title and branch (called once at start).
func printWatchHeader(ws *github.WatchStatus) {
	fmt.Println()
	title := ws.Title
	if title == "" {
		title = fmt.Sprintf("PR #%d", ws.Number)
	} else {
		title = fmt.Sprintf("PR #%d %s %s", ws.Number, ui.Dash, title)
	}
	fmt.Printf("  %s\n", ui.Bold(title))
	fmt.Printf("  %s\n", ui.Dim(ws.HeadRefName))
}

// renderWatchTable prints the CI and Review sections and returns the number of lines printed.
func renderWatchTable(ws *github.WatchStatus) int {
	lines := 0

	// CI section
	pass, fail, pending := ws.ChecksByStatus()
	total := len(pass) + len(fail) + len(pending)
	if total > 0 {
		fmt.Println()
		lines++

		fmt.Printf("  %s  %s\n", ui.Bold("CI"), ui.Dim(fmt.Sprintf("%d/%d", len(pass), total)))
		lines++

		// Build sorted list: pass, fail, pending
		type checkEntry struct {
			glyph string
			name  string
		}
		var entries []checkEntry
		for _, c := range pass {
			entries = append(entries, checkEntry{glyph: ui.Green(ui.Pass), name: c.Name})
		}
		for _, c := range fail {
			entries = append(entries, checkEntry{glyph: ui.Red(ui.Fail), name: c.Name})
		}
		for _, c := range pending {
			entries = append(entries, checkEntry{glyph: ui.Yellow(ui.Pending), name: c.Name})
		}

		// Render in two columns
		for i := 0; i < len(entries); i += 2 {
			left := entries[i]
			leftName := ui.Truncate(left.name, watchCheckColWidth)
			if i+1 < len(entries) {
				right := entries[i+1]
				rightName := ui.Truncate(right.name, watchCheckColWidth)
				fmt.Printf("    %s  %-*s  %s  %s\n", left.glyph, watchCheckColWidth, leftName, right.glyph, rightName)
			} else {
				fmt.Printf("    %s  %s\n", left.glyph, leftName)
			}
			lines++
		}
	}

	// Review section
	items := ws.ReviewItems()
	showReview := len(items) > 0 || ws.ReviewDecision == "REVIEW_REQUIRED"
	if showReview {
		fmt.Println()
		lines++

		fmt.Printf("  %s\n", ui.Bold("Review"))
		lines++

		if len(items) == 0 {
			fmt.Printf("    %s  %s\n", ui.Blue(ui.Pending), ui.Dim("review required"))
			lines++
		} else {
			for i := 0; i < len(items); i += 2 {
				lg, lt := formatReviewItem(items[i])
				if i+1 < len(items) {
					rg, rt := formatReviewItem(items[i+1])
					fmt.Printf("    %s  %-*s  %s  %s\n", lg, watchCheckColWidth, lt, rg, rt)
				} else {
					fmt.Printf("    %s  %s\n", lg, lt)
				}
				lines++
			}
		}
	}

	fmt.Println()
	lines++
	return lines
}

// formatReviewItem returns the glyph and text for a review item separately,
// so the caller can pad the text correctly (ANSI codes break %-*s alignment).
func formatReviewItem(item github.ReviewItem) (glyph, text string) {
	login := ui.Truncate(item.Login, 16)
	switch item.State {
	case "approved":
		return ui.Green(ui.Pass), login + "  " + ui.Dim("approved")
	case "changes_requested":
		return ui.Red(ui.Fail), login + "  " + ui.Dim("changes")
	default:
		return ui.Blue(ui.Pending), login + "  " + ui.Dim("pending")
	}
}

// buildSpinnerMessage creates a concise status message for the spinner line.
func buildSpinnerMessage(ws *github.WatchStatus) string {
	cs := ws.GetCISummary()

	ciWaiting := cs.Pending > 0 || cs.Total == 0
	reviewWaiting := ws.ReviewDecision == "REVIEW_REQUIRED"

	switch {
	case ciWaiting && reviewWaiting:
		return ui.Dim("Waiting for checks and review...")
	case ciWaiting:
		if cs.Total > 0 {
			return ui.Dim(fmt.Sprintf("Waiting for %d check(s)...", cs.Pending))
		}
		return ui.Dim("Waiting for status...")
	case reviewWaiting:
		return ui.Dim("Waiting for review...")
	default:
		return ui.Dim("Waiting for status...")
	}
}

// printWatchVerdict outputs the final success/error line after resolution.
func printWatchVerdict(ws *github.WatchStatus) {
	cs := ws.GetCISummary()

	// Terminal bell
	fmt.Print("\a")

	switch {
	case ws.State == "MERGED":
		ui.Success("Already merged")
	case ws.State == "CLOSED":
		ui.Error("PR was closed")
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

// sendNotification sends a desktop notification on macOS.
func sendNotification(ws *github.WatchStatus) {
	if runtime.GOOS != "darwin" {
		return
	}

	title := fmt.Sprintf("PR #%d", ws.Number)
	var message string
	switch {
	case ws.State == "MERGED":
		message = "Already merged"
	case ws.State == "CLOSED":
		message = "PR was closed"
	case ws.MergeStateStatus == "CLEAN":
		message = "Ready to merge"
	case ws.Mergeable == "CONFLICTING":
		message = "Merge conflict"
	case ws.ReviewDecision == "CHANGES_REQUESTED":
		message = "Changes requested"
	default:
		cs := ws.GetCISummary()
		if cs.Fail > 0 {
			message = fmt.Sprintf("%d check(s) failed", cs.Fail)
		} else {
			message = "Status resolved"
		}
	}

	script := fmt.Sprintf(`display notification %q with title %q`, message, title)
	_ = exec.Command("osascript", "-e", script).Run()
}
