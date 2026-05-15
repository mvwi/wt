package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mvwi/wt/internal/git"
	"github.com/mvwi/wt/internal/github"
	"github.com/mvwi/wt/internal/tui"
	"github.com/mvwi/wt/internal/ui"
	"github.com/spf13/cobra"
)

var dashCmd = &cobra.Command{
	Use:     "dash",
	Aliases: []string{"dashboard"},
	GroupID: groupWorkflow,
	Short:   "Live worktree dashboard (TUI)",
	Long: `Interactive worktree dashboard.

Renders the same data as 'wt list' but refreshes periodically and supports
keyboard navigation. Press enter on a row to switch to that worktree.

For one-shot or scripted output, use 'wt list' instead.`,
	Example: `  wt dash               Open the live dashboard`,
	RunE:    runDash,
}

func init() {
	rootCmd.AddCommand(dashCmd)
}

func runDash(cmd *cobra.Command, args []string) error {
	if !ui.IsTTY() {
		return fmt.Errorf("wt dash requires a terminal\n   For scripted output, use: wt list --output json")
	}

	ctx, err := newContext()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot determine current directory: %w", err)
	}

	// Initial load — show a spinner while we collect the slow PR data so the
	// TUI starts up with a fully populated table.
	spin := ui.NewSpinner("Loading dashboard")
	initial, err := gatherDashItems(ctx, cwd)
	spin.Stop()
	if err != nil {
		return err
	}

	gather := func() ([]tui.DashItem, error) {
		// On each tick, re-read cwd in case the user switched in another shell.
		// We don't update the closure's cwd value because IsCurrent is a UX hint,
		// not a correctness requirement.
		return gatherDashItems(ctx, cwd)
	}

	closeFn := func(path, branch string) error {
		// RemoveWorktree passes --force, so dirty/unpushed worktrees are
		// removable from here. The dashboard already gates risky closes behind
		// a confirmation prompt — by the time we get here, the user agreed.
		if err := git.RemoveWorktree(path); err != nil {
			return err
		}
		if branch != "" && git.BranchExists(branch) {
			_ = git.DeleteBranch(branch)
		}
		return nil
	}

	fetchDetail := func(item tui.DashItem) (tui.DashDetail, error) {
		return gatherDashDetail(ctx, item)
	}

	rebase := func(path, branch string) (int, error) {
		return dashRebase(ctx, path, branch)
	}

	result, err := tui.RunDashboard(tui.DashConfig{
		Initial:           initial,
		Gather:            gather,
		Close:             closeFn,
		FetchDetail:       fetchDetail,
		FetchCommitDetail: gatherCommitDetail,
		Rebase:            rebase,
	})
	if err != nil {
		return err
	}

	if result.SwitchTo != "" && result.SwitchTo != cwd {
		savePreviousWorktree(cwd)
		ui.PrintCdHint(result.SwitchTo)
		short := ctx.shortName(result.SwitchTo)
		fmt.Printf("%s %s\n", ui.Yellow(ui.Current), short)
	}

	return nil
}

// gatherDashDetail produces the bottom-panel detail for one worktree.
// Skips PR lookup when the row has no associated PR. All errors from
// individual git/gh calls are tolerated — the partial result is still useful.
func gatherDashDetail(ctx *cmdContext, item tui.DashItem) (tui.DashDetail, error) {
	var d tui.DashDetail

	if item.IsBase {
		return d, nil
	}

	base := ctx.baseRef()

	// Recent commits (best-effort)
	if entries, err := git.LogOnelineIn(item.Path, base, 5); err == nil {
		for _, e := range entries {
			d.Commits = append(d.Commits, tui.DashCommit{
				Hash:    e.Hash,
				Subject: e.Subject,
				Age:     e.Age,
			})
		}
	}

	// Diff stats (best-effort)
	if stats, err := git.DiffStatsIn(item.Path, base); err == nil {
		d.Stats = tui.DashDiffStats{
			Commits:   stats.Commits,
			Files:     stats.Files,
			Additions: stats.Additions,
			Deletions: stats.Deletions,
		}
	}

	// File-by-file changes (best-effort)
	if files, err := git.DiffFilesIn(item.Path, base); err == nil {
		for _, f := range files {
			d.Files = append(d.Files, tui.DashFileChange{Status: f.Status, Path: f.Path})
		}
	}

	// PR detail — title/author/body + named CI checks come from one gh call.
	if item.PR != nil && github.IsAvailable() {
		if pr, err := github.GetPRDetail(item.PR.Number); err == nil && pr != nil {
			d.PRTitle = pr.Title
			d.PRAuthor = pr.Author.Login
			d.PRBody = firstParagraph(pr.Body)
			if item.PR.State == "open" {
				for _, c := range pr.StatusChecks {
					if c.Name == "" {
						continue
					}
					d.Checks = append(d.Checks, tui.DashCheck{Name: c.Name, State: c.Result()})
				}
			}
		}
	}

	return d, nil
}

// dashRebase syncs a worktree with its upstream. For feature branches that
// means rebasing onto the configured base branch; for the base-branch worktree
// itself it means fast-forwarding from the remote (since rebasing the base
// onto itself is meaningless). Async-safe for use from the dashboard's
// tea.Cmd: never prompts, never auto-stashes, aborts cleanly on conflict.
// Returns the number of commits applied and an error describing why the
// operation didn't proceed (dirty, conflicts, etc.).
func dashRebase(ctx *cmdContext, path, branch string) (int, error) {
	if git.HasChangesIn(path) {
		return 0, fmt.Errorf("uncommitted changes")
	}

	if ctx.isBaseBranch(branch) {
		return dashFastForwardBase(ctx, path)
	}

	// Fetch the base ref so the rebase target is current. Failure here is
	// non-fatal — fall through to rebase against whatever's local.
	_ = git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch)

	ab, err := git.GetAheadBehindIn(path, ctx.baseRef())
	if err != nil {
		return 0, err
	}
	if ab.Behind == 0 {
		return 0, nil // up to date
	}
	if err := git.RebaseIn(path, ctx.baseRef()); err != nil {
		git.RebaseAbortIn(path)
		return 0, fmt.Errorf("conflicts — wt rebase --continue")
	}
	return ab.Behind, nil
}

// dashFastForwardBase fast-forwards the base-branch worktree from its remote.
// Mirrors the CLI's rebaseBaseBranch but without any interactive prompts: we
// already bailed on dirty in dashRebase, so this just fetches and merges.
func dashFastForwardBase(ctx *cmdContext, path string) (int, error) {
	if err := git.Fetch(ctx.Config.Remote, ctx.Config.BaseBranch); err != nil {
		return 0, fmt.Errorf("fetch failed: %w", err)
	}
	remoteRef := ctx.Config.Remote + "/" + ctx.Config.BaseBranch
	ab, err := git.GetAheadBehindIn(path, remoteRef)
	if err != nil {
		return 0, err
	}
	if ab.Behind == 0 {
		return 0, nil // up to date
	}
	if err := git.MergeFFIn(path, remoteRef); err != nil {
		return 0, fmt.Errorf("fast-forward failed: %w", err)
	}
	return ab.Behind, nil
}

// gatherCommitDetail loads full metadata + files for one commit. Wraps
// git.CommitDetailIn and adapts the result into TUI-layer types.
func gatherCommitDetail(worktreePath, hash string) (tui.DashCommitDetail, error) {
	raw, err := git.CommitDetailIn(worktreePath, hash)
	if err != nil {
		return tui.DashCommitDetail{}, err
	}
	d := tui.DashCommitDetail{
		Hash:    raw.Hash,
		Author:  raw.Author,
		Date:    raw.Date,
		Subject: raw.Subject,
		Body:    raw.Body,
	}
	for _, f := range raw.Files {
		d.Files = append(d.Files, tui.DashCommitFile{
			Status: f.Status,
			Path:   f.Path,
			Adds:   f.Adds,
			Dels:   f.Dels,
		})
	}
	return d, nil
}

// firstParagraph returns the first content paragraph of a PR body, skipping
// leading markdown headers ("## Summary" etc.). Many PR templates start with a
// "## Summary" heading followed by the actual content — we want the content,
// not the heading.
//
// Returns "" if the body is empty or only contains headings.
func firstParagraph(body string) string {
	for {
		body = strings.TrimSpace(body)
		if body == "" {
			return ""
		}
		// Leading line is a markdown header — peel it off and look at what
		// follows. Loop to handle stacked headers like "## Summary\n### Subsection".
		if strings.HasPrefix(body, "#") {
			nl := strings.IndexByte(body, '\n')
			if nl < 0 {
				return ""
			}
			body = body[nl+1:]
			continue
		}
		break
	}
	if idx := strings.Index(body, "\n\n"); idx >= 0 {
		body = body[:idx]
	}
	return strings.TrimSpace(body)
}

// gatherDashItems collects worktree + PR data and converts to TUI items.
// Reuses the existing list helpers so dash and list stay in sync.
func gatherDashItems(ctx *cmdContext, cwd string) ([]tui.DashItem, error) {
	worktrees, err := git.ListWorktrees()
	if err != nil {
		return nil, err
	}

	infos, _ := collectWorktreeInfos(ctx, cwd, worktrees)

	var openPRs, mergedPRs, closedPRs []github.PR
	if github.IsAvailable() {
		openPRs, mergedPRs, closedPRs, _ = fetchPRData()
	}

	staleThreshold := ctx.Config.EffectiveStaleThreshold()

	items := make([]tui.DashItem, 0, len(infos))
	for _, info := range infos {
		isBase := ctx.isBaseBranch(info.Branch)

		item := tui.DashItem{
			Path:       info.Path,
			ShortName:  info.ShortName,
			Branch:     info.Branch,
			IsCurrent:  info.IsCurrent,
			IsBase:     isBase,
			Age:        info.Age,
			Behind:     info.Behind,
			Ahead:      info.Ahead,
			DirtyCount: info.DirtyCount,
		}

		if !isBase {
			openPR := github.FindPRForBranch(openPRs, info.Branch)
			mergedPR := github.FindPRForBranch(mergedPRs, info.Branch)
			closedPR := github.FindPRForBranch(closedPRs, info.Branch)

			daysAgo := git.LastCommitDaysAgo(info.Path)
			item.StaleDimmed = daysAgo >= staleThreshold && openPR == nil

			// Unpushed commits are the canonical data-loss signal for close —
			// fetch for every non-base worktree, not just ones with an open PR.
			// Cheap call (single rev-list --count); returns 0 if there's no
			// upstream so no need to special-case.
			item.Unpushed = git.UnpushedCountIn(info.Path)

			switch {
			case openPR != nil:
				rs := openPR.GetReviewSummary()
				cs := openPR.GetCISummary()
				item.PR = &tui.DashPR{
					Number: openPR.Number,
					State:  "open",
					Review: tui.DashReview{Approved: rs.Approved, Changes: rs.Changes, Pending: rs.Pending},
					CI:     tui.DashCI{Pass: cs.Pass, Fail: cs.Fail, Pending: cs.Pending, Total: cs.Total},
				}
			case mergedPR != nil:
				item.PR = &tui.DashPR{Number: mergedPR.Number, State: "merged"}
			case closedPR != nil:
				item.PR = &tui.DashPR{Number: closedPR.Number, State: "closed"}
			}
		}

		items = append(items, item)
	}

	return items, nil
}
