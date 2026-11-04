package cmd

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

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
  2. PR status table with reviews and CI (requires GitHub API)

Use --output json for machine-readable output (all data in one pass).`,
	Example: `  wt list                 Show all worktrees with status
  wt list --output json   Machine-readable JSON output
  wt list --output toon   Flat YAML-like output for agents`,
	RunE: runList,
}

func init() {
	listCmd.Flags().String("output", "", "Output format: json, toon")
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

// JSON output structs

type listJSONOutput struct {
	Worktrees []listJSONEntry `json:"worktrees"`
	CTA       []string        `json:"cta,omitempty"`
}

type listJSONReview struct {
	Approved int `json:"approved"`
	Changes  int `json:"changes_requested"`
	Pending  int `json:"pending"`
}

type listJSONCI struct {
	Pass    int `json:"pass"`
	Fail    int `json:"fail"`
	Pending int `json:"pending"`
	Total   int `json:"total"`
}

type listJSONPR struct {
	Number int            `json:"number"`
	State  string         `json:"state"`
	Review listJSONReview `json:"review"`
	CI     listJSONCI     `json:"ci"`
}

type listJSONEntry struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"`
	Branch     string      `json:"branch"`
	Current    bool        `json:"current"`
	BaseBranch bool        `json:"base_branch"`
	Age        string      `json:"age,omitempty"`
	Dirty      int         `json:"dirty"`
	Behind     int         `json:"behind"`
	Ahead      int         `json:"ahead"`
	PR         *listJSONPR `json:"pr"`
}

func runList(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output")

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

	switch outputFormat {
	case "json":
		return runListJSON(ctx, cwd, worktrees)
	case "toon":
		return runListTOON(ctx, cwd, worktrees)
	}

	if len(worktrees) == 0 {
		fmt.Println("No worktrees found")
		return nil
	}

	return runListTerminal(ctx, cwd, worktrees)
}

// collectWorktreeInfos gathers branch/status data for all worktrees.
func collectWorktreeInfos(ctx *cmdContext, cwd string, worktrees []git.Worktree) ([]worktreeInfo, []string) {
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

	return infos, featureBranches
}

// fetchPRData fetches open, merged, and closed PRs for the given branches in
// parallel. err is the first failure, if any; the other lists may still hold data.
func fetchPRData(branches []string) (openPRs, mergedPRs, closedPRs []github.PR, err error) {
	if len(branches) == 0 {
		return
	}
	var openErr, mergedErr, closedErr error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); openPRs, openErr = github.ListPRs("open", branches...) }()
	go func() { defer wg.Done(); mergedPRs, mergedErr = github.ListPRs("merged", branches...) }()
	go func() { defer wg.Done(); closedPRs, closedErr = github.ListPRs("closed", branches...) }()
	wg.Wait()
	err = cmp.Or(openErr, mergedErr, closedErr)
	if openErr == nil {
		cacheOpenPRs(branches, openPRs)
	}
	return
}

func runListJSON(ctx *cmdContext, cwd string, worktrees []git.Worktree) error {
	infos, branches := collectWorktreeInfos(ctx, cwd, worktrees)

	// Fetch PR data (no spinner, no terminal output)
	var openPRs, mergedPRs, closedPRs []github.PR
	if github.IsAvailable() {
		openPRs, mergedPRs, closedPRs, _ = fetchPRData(branches)
	}

	entries := make([]listJSONEntry, 0, len(infos))
	hasBehind := false
	hasStale := false
	for _, info := range infos {
		isBase := ctx.isBaseBranch(info.Branch)

		entry := listJSONEntry{
			Name:       info.ShortName,
			Path:       info.Path,
			Branch:     info.Branch,
			Current:    info.IsCurrent,
			BaseBranch: isBase,
			Age:        info.Age,
			Dirty:      info.DirtyCount,
			Behind:     info.Behind,
			Ahead:      info.Ahead,
		}

		if !isBase {
			entry.PR = findPRJSON(info.Branch, openPRs, mergedPRs, closedPRs)
			if info.Behind > 0 {
				hasBehind = true
			}
			if entry.PR != nil && (entry.PR.State == "merged" || entry.PR.State == "closed") {
				hasStale = true
			}
		}

		entries = append(entries, entry)
	}

	data, err := json.MarshalIndent(listJSONOutput{Worktrees: entries, CTA: deriveListCTA(hasStale, hasBehind)}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runListTOON(ctx *cmdContext, cwd string, worktrees []git.Worktree) error {
	infos, branches := collectWorktreeInfos(ctx, cwd, worktrees)

	var openPRs, mergedPRs, closedPRs []github.PR
	if github.IsAvailable() {
		openPRs, mergedPRs, closedPRs, _ = fetchPRData(branches)
	}

	hasBehind := false
	hasStale := false

	fmt.Println("worktrees:")
	for _, info := range infos {
		isBase := ctx.isBaseBranch(info.Branch)
		fmt.Printf("- name: %s\n", info.ShortName)
		fmt.Printf("  path: %s\n", info.Path)
		fmt.Printf("  branch: %s\n", info.Branch)
		fmt.Printf("  current: %v\n", info.IsCurrent)
		if isBase {
			fmt.Printf("  base_branch: true\n")
		}
		if info.Age != "" {
			fmt.Printf("  age: %s\n", info.Age)
		}
		fmt.Printf("  dirty: %d\n", info.DirtyCount)
		fmt.Printf("  behind: %d\n", info.Behind)
		fmt.Printf("  ahead: %d\n", info.Ahead)
		if info.Behind > 0 {
			hasBehind = true
		}
		if !isBase {
			pr := findPRJSON(info.Branch, openPRs, mergedPRs, closedPRs)
			if pr != nil {
				fmt.Printf("  pr.number: %d\n", pr.Number)
				fmt.Printf("  pr.state: %s\n", pr.State)
				if pr.State == "open" {
					fmt.Printf("  pr.review: %d✓ %d✗ %d●\n", pr.Review.Approved, pr.Review.Changes, pr.Review.Pending)
					fmt.Printf("  pr.ci: %d✓ %d✗ %d● / %d\n", pr.CI.Pass, pr.CI.Fail, pr.CI.Pending, pr.CI.Total)
				}
				if pr.State == "merged" || pr.State == "closed" {
					hasStale = true
				}
			}
		}
	}

	ui.PrintCTA(deriveListCTA(hasStale, hasBehind)...)
	return nil
}

// deriveListCTA returns CTA suggestions based on worktree state.
func deriveListCTA(hasStale, hasBehind bool) []string {
	if hasStale {
		return []string{"wt prune"}
	}
	if hasBehind {
		return []string{"wt rebase --all"}
	}
	return nil
}

// findPRJSON finds the PR for a branch and converts it to JSON format.
func findPRJSON(branch string, openPRs, mergedPRs, closedPRs []github.PR) *listJSONPR {
	if pr := github.FindPRForBranch(openPRs, branch); pr != nil {
		rs := pr.GetReviewSummary()
		cs := pr.GetCISummary()
		return &listJSONPR{
			Number: pr.Number,
			State:  "open",
			Review: listJSONReview{Approved: rs.Approved, Changes: rs.Changes, Pending: rs.Pending},
			CI:     listJSONCI{Pass: cs.Pass, Fail: cs.Fail, Pending: cs.Pending, Total: cs.Total},
		}
	}
	if pr := github.FindPRForBranch(mergedPRs, branch); pr != nil {
		return &listJSONPR{Number: pr.Number, State: "merged"}
	}
	if pr := github.FindPRForBranch(closedPRs, branch); pr != nil {
		return &listJSONPR{Number: pr.Number, State: "closed"}
	}
	return nil
}

// Column caps for the name/branch overview, so long names can't push the
// rest of the line off screen.
// shortcut: fixed caps sized for an 80-col terminal; read the real width if
// people ask for more.
const (
	listNameMax   = 32
	listBranchMax = 40
	listBranchCol = 28
)

func runListTerminal(ctx *cmdContext, cwd string, worktrees []git.Worktree) error {
	infos, featureBranches := collectWorktreeInfos(ctx, cwd, worktrees)

	// Phase 1: Show worktree names immediately
	ui.Header("WORKTREES")

	nameWidth := 8
	for _, info := range infos {
		nameWidth = max(nameWidth, utf8.RuneCountInString(info.ShortName))
	}
	nameWidth = min(nameWidth, listNameMax)

	ui.DimF("  %-*s %s\n", nameWidth+2, "Name", "Branch")
	ui.DimF("  %s\n", strings.Repeat("─", nameWidth+2+28))

	for _, info := range infos {
		if info.IsCurrent {
			fmt.Printf("%s ", ui.Yellow(ui.Current))
		} else {
			fmt.Print("  ")
		}
		fmt.Print(padRight(ui.Truncate(info.ShortName, nameWidth), nameWidth) + " ")
		if info.Branch != info.ShortName {
			fmt.Print(ui.Dim(ui.Truncate(info.Branch, listBranchMax)))
		}
		fmt.Println()
	}

	if len(featureBranches) == 0 {
		fmt.Println()
		return nil
	}

	// Phase 2: status table for feature branches
	fmt.Println()
	showPR := github.IsAvailable()
	var openPRs, mergedPRs, closedPRs []github.PR
	if showPR {
		spin := ui.NewSpinner("Loading PR status")
		var err error
		openPRs, mergedPRs, closedPRs, err = fetchPRData(featureBranches)
		spin.Stop()
		switch {
		case err == nil:
		case isNotGitHubRemote(err):
			// Not a GitHub repo: PR columns would only ever be empty.
			showPR = false
		default:
			_, reason, _ := strings.Cut(err.Error(), ": ")
			ui.DimF("  PR status may be incomplete: %s\n", ui.Truncate(reason, 100))
			ui.DimF("  Check gh with: gh auth status\n\n")
		}
	} else if ui.IsTTY() {
		ui.DimF("  PR status needs the GitHub CLI (gh): https://cli.github.com\n\n")
	}

	staleThreshold := ctx.Config.EffectiveStaleThreshold()

	header := fmt.Sprintf("  %-*s %-6s %-6s %-8s", listBranchCol, "Branch", "Commit", "Dirty", "Sync")
	if showPR {
		header += fmt.Sprintf(" %-6s %-8s %s", "PR", "Review", "CI")
	}
	ui.DimF("%s\n", header)
	ui.DimF("  %s\n", strings.Repeat("─", utf8.RuneCountInString(header)-2))

	var table strings.Builder
	hasStale := false

	for _, info := range infos {
		if ctx.isBaseBranch(info.Branch) {
			continue
		}

		var openPR, mergedPR, closedPR *github.PR
		isStale := false
		if showPR {
			openPR = github.FindPRForBranch(openPRs, info.Branch)
			mergedPR = github.FindPRForBranch(mergedPRs, info.Branch)
			closedPR = github.FindPRForBranch(closedPRs, info.Branch)
			// Stale: no recent activity and no open PR. Needs PR data, or
			// every quiet branch would look stale.
			isStale = git.WorktreeAgeDays(info.Path) >= staleThreshold && openPR == nil
		}

		// Dim the entire row if stale
		colorize := fmt.Sprintf
		if isStale {
			colorize = func(f string, a ...any) string {
				return ui.Dim(fmt.Sprintf(f, a...))
			}
		}

		row := &table
		fmt.Fprintf(row, "  %s ", colorize("%s", padRight(ui.Truncate(info.Branch, listBranchCol), listBranchCol)))
		fmt.Fprintf(row, "%s ", colorize("%-6s", info.Age))

		// Dirty
		switch {
		case info.DirtyCount == 0:
			fmt.Fprintf(row, "%s ", ui.Dim(fmt.Sprintf("%-6s", ui.Dash)))
		case isStale:
			fmt.Fprintf(row, "%s ", colorize("%-6d", info.DirtyCount))
		default:
			fmt.Fprintf(row, "%s ", ui.Yellow(fmt.Sprintf("%-6d", info.DirtyCount)))
		}

		// Sync
		syncStr := padRight(buildSyncStr(info.Behind, info.Ahead), 8)
		switch {
		case isStale:
			fmt.Fprint(row, colorize("%s", syncStr))
		case info.Behind > 0:
			fmt.Fprint(row, ui.Yellow(syncStr))
		default:
			fmt.Fprint(row, ui.Green(syncStr))
		}

		if showPR {
			row.WriteString(" ")
			switch {
			case openPR != nil:
				row.WriteString(openPRStatus(openPR, info.Path))
			case mergedPR != nil:
				fmt.Fprintf(row, "%s %s   %s", ui.Blue(fmt.Sprintf("%-6s", fmt.Sprintf("#%d", mergedPR.Number))), ui.Green("merged"), ui.Yellow("stale"))
				hasStale = true
			case closedPR != nil:
				fmt.Fprintf(row, "%s %s  %s", ui.Blue(fmt.Sprintf("%-6s", fmt.Sprintf("#%d", closedPR.Number))), ui.Red("closed"), ui.Yellow("stale"))
				hasStale = true
			case isStale:
				row.WriteString(ui.Dim(ui.Dash) + " \U0001f4a4") // 💤
			default:
				row.WriteString(ui.Dim(ui.Dash))
			}
		}
		row.WriteString("\n")
	}
	fmt.Print(table.String())

	if legend := listLegend(table.String()); legend != "" {
		fmt.Println()
		ui.DimF("  %s\n", legend)
	}

	if hasStale {
		fmt.Printf("  %s\n", ui.Yellow("Tip: run 'wt prune' to clean up stale worktrees"))
	}

	fmt.Println()
	return nil
}

// isNotGitHubRemote reports whether gh failed because no remote points at a
// GitHub host. That is a normal setup, not an error worth a warning.
func isNotGitHubRemote(err error) bool {
	return strings.Contains(err.Error(), "known GitHub host")
}

// padRight pads s with spaces to width runes. Unlike %-*s it counts runes,
// not bytes, so glyphs and non-ASCII names line up.
func padRight(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// legendEntries lists each glyph the status table can show, in print order.
var legendEntries = []struct{ glyph, meaning string }{
	{ui.ArrowDown, "behind base"},
	{ui.ArrowUp, "ahead of base"},
	{ui.Pass, "ok"},
	{ui.Fail, "fail/changes"},
	{ui.Pending, "pending"},
	{ui.NoReview, "none yet"},
	{ui.PushUp, "unpushed"},
	{"\U0001f4a4", "stale"},
}

// listLegend explains only the glyphs that appear in the rendered table.
func listLegend(table string) string {
	var parts []string
	for _, e := range legendEntries {
		if strings.Contains(table, e.glyph) {
			parts = append(parts, e.glyph+" "+e.meaning)
		}
	}
	return strings.Join(parts, "  ")
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

// openPRStatus renders the PR, Review, and CI cells for an open PR.
func openPRStatus(pr *github.PR, wtPath string) string {
	var b strings.Builder
	b.WriteString(ui.Blue(fmt.Sprintf("%-6s", fmt.Sprintf("#%d", pr.Number))) + " ")

	// Review glyphs, one per reviewer
	rs := pr.GetReviewSummary()
	reviewCount := rs.Approved + rs.Changes + rs.Pending
	if rs.Approved > 0 {
		b.WriteString(ui.Green(strings.Repeat(ui.Pass, rs.Approved)))
	}
	if rs.Changes > 0 {
		b.WriteString(ui.Red(strings.Repeat(ui.Fail, rs.Changes)))
	}
	if rs.Pending > 0 {
		b.WriteString(ui.Blue(strings.Repeat(ui.Pending, rs.Pending)))
	}
	if reviewCount == 0 {
		b.WriteString(ui.Dim(ui.NoReview))
		reviewCount = 1
	}
	if pad := 8 - reviewCount; pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteString(" ")

	// CI: one summary cell, not one glyph per check
	cs := pr.GetCISummary()
	ci := ui.CISummary(cs.Pass, cs.Fail, cs.Pending, cs.Total)
	switch {
	case cs.Total == 0:
		b.WriteString(ui.Dim(ui.NoReview))
	case cs.Fail > 0:
		b.WriteString(ui.Red(ci))
	case cs.Pending > 0:
		b.WriteString(ui.Yellow(ci))
	default:
		b.WriteString(ui.Green(ci))
	}

	if unpushed := git.UnpushedCountIn(wtPath); unpushed > 0 {
		b.WriteString(" " + ui.Magenta(fmt.Sprintf("%s%d", ui.PushUp, unpushed)))
	}
	return b.String()
}
