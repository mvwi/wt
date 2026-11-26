package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"pgregory.net/rapid"
)

// These tests feed the TUI models random sequences of keys, resizes and async
// results, and check invariants after every step. rapid shrinks a failure to
// the shortest sequence that still breaks the rule. Re-run one failure with
// the -rapid.failfile flag it prints; raise -rapid.checks for a longer hunt.
//
// Commands returned by Update are never run, so nothing touches git or gh.
// Their results are injected instead as random messages, in any order, which
// covers stale and out-of-order replies.

// genText draws a single-line string with letters, digits and symbols from
// any script, so names include wide runes and emoji.
var genText = rapid.OneOf(
	rapid.SampledFrom([]string{"", "a", "feat/x", "日本語のブランチ", "🦀🦀🦀", "e\u0301te\u0301", strings.Repeat("x", 70)}),
	rapid.StringMatching(`[\p{L}\p{N}\p{So} ._/-]{0,40}`),
)

var genBody = rapid.StringMatching(`([\p{L}\p{N}\p{So} .,:#-]{0,60}\n){0,8}`)

func genItems(t *rapid.T, label string) []DashItem {
	n := rapid.IntRange(0, 12).Draw(t, label+"_n")
	base := rapid.IntRange(-1, n-1).Draw(t, label+"_base")
	current := rapid.IntRange(-1, n-1).Draw(t, label+"_current")
	items := make([]DashItem, n)
	for i := range items {
		it := DashItem{
			Path:        fmt.Sprintf("/r/%d", i),
			ShortName:   genText.Draw(t, label+"_name"),
			Branch:      genText.Draw(t, label+"_branch"),
			IsBase:      i == base,
			IsCurrent:   i == current,
			StaleDimmed: rapid.Bool().Draw(t, label+"_stale"),
			Age:         rapid.SampledFrom([]string{"", "3s", "5m", "2h", "4d", "3w", "1y"}).Draw(t, label+"_age"),
			Behind:      rapid.IntRange(0, 9999).Draw(t, label+"_behind"),
			Ahead:       rapid.IntRange(0, 9999).Draw(t, label+"_ahead"),
			DirtyCount:  rapid.IntRange(0, 9999).Draw(t, label+"_dirty"),
			Unpushed:    rapid.IntRange(0, 9999).Draw(t, label+"_unpushed"),
		}
		if !it.IsBase && rapid.Bool().Draw(t, label+"_hasPR") {
			it.PR = &DashPR{
				Number: rapid.IntRange(1, 999999).Draw(t, label+"_pr"),
				State:  rapid.SampledFrom([]string{"open", "merged", "closed"}).Draw(t, label+"_prState"),
				Review: DashReview{
					Approved: rapid.IntRange(0, 20).Draw(t, label+"_approved"),
					Changes:  rapid.IntRange(0, 20).Draw(t, label+"_changes"),
					Pending:  rapid.IntRange(0, 20).Draw(t, label+"_pending"),
				},
				CI: DashCI{
					Pass:    rapid.IntRange(0, 50).Draw(t, label+"_ciPass"),
					Fail:    rapid.IntRange(0, 50).Draw(t, label+"_ciFail"),
					Pending: rapid.IntRange(0, 50).Draw(t, label+"_ciPending"),
				},
			}
			it.PR.CI.Total = it.PR.CI.Pass + it.PR.CI.Fail + it.PR.CI.Pending
		}
		items[i] = it
	}
	return items
}

var genSubject = rapid.Custom(func(t *rapid.T) string {
	prefix := rapid.SampledFrom([]string{"", "feat: ", "fix(dash): ", "refactor!: ", "chore: "}).Draw(t, "prefix")
	return prefix + genText.Draw(t, "subject")
})

func genDetail(t *rapid.T) DashDetail {
	d := DashDetail{
		PRTitle:  genText.Draw(t, "prTitle"),
		PRAuthor: genText.Draw(t, "prAuthor"),
		PRBody:   genBody.Draw(t, "prBody"),
		Stats: DashDiffStats{
			Commits:   rapid.IntRange(0, 999).Draw(t, "statCommits"),
			Files:     rapid.IntRange(0, 999).Draw(t, "statFiles"),
			Additions: rapid.IntRange(0, 99999).Draw(t, "statAdds"),
			Deletions: rapid.IntRange(0, 99999).Draw(t, "statDels"),
		},
	}
	// gatherDashDetail asks git for at most 5 commits.
	for range rapid.IntRange(0, 5).Draw(t, "commits") {
		d.Commits = append(d.Commits, DashCommit{
			// A small hash space makes commits shared across worktrees, like
			// the base branch's history.
			Hash:    fmt.Sprintf("%07x", rapid.IntRange(0, 40).Draw(t, "hash")),
			Subject: genSubject.Draw(t, "commitSubject"),
			Age:     rapid.SampledFrom([]string{"1s", "5m", "3d", "2y"}).Draw(t, "commitAge"),
		})
	}
	for range rapid.IntRange(0, 15).Draw(t, "files") {
		d.Files = append(d.Files, DashFileChange{
			Status: rapid.SampledFrom([]string{"M", "A", "D", "R"}).Draw(t, "fileStatus"),
			Path:   genText.Draw(t, "filePath"),
		})
	}
	for range rapid.IntRange(0, 10).Draw(t, "checks") {
		d.Checks = append(d.Checks, DashCheck{
			Name:  genText.Draw(t, "checkName"),
			State: rapid.SampledFrom([]string{"pass", "fail", "pending"}).Draw(t, "checkState"),
		})
	}
	return d
}

func genCommitDetail(t *rapid.T, hash string) DashCommitDetail {
	cd := DashCommitDetail{
		Hash:    hash,
		Author:  genText.Draw(t, "author"),
		Date:    rapid.SampledFrom([]string{"", "2026-11-19", "3 days ago"}).Draw(t, "date"),
		Subject: genSubject.Draw(t, "cdSubject"),
		Body:    genBody.Draw(t, "cdBody"),
	}
	for range rapid.IntRange(0, 30).Draw(t, "cdFiles") {
		cd.Files = append(cd.Files, DashCommitFile{
			Status: rapid.SampledFrom([]string{"A", "M", "D", "B"}).Draw(t, "cdFileStatus"),
			Path:   genText.Draw(t, "cdFilePath"),
			Adds:   rapid.IntRange(0, 99999).Draw(t, "cdAdds"),
			Dels:   rapid.IntRange(0, 99999).Draw(t, "cdDels"),
		})
	}
	return cd
}

func runeKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

var dashKeys = []tea.KeyPressMsg{
	runeKey('j'), runeKey('k'), runeKey('g'), runeKey('G'),
	runeKey('c'), runeKey('r'), runeKey('R'), runeKey('y'), runeKey('n'),
	runeKey('?'), runeKey('h'), runeKey('x'), runeKey('s'), runeKey('q'),
	{Code: tea.KeyUp}, {Code: tea.KeyDown}, {Code: tea.KeyHome}, {Code: tea.KeyEnd},
	{Code: tea.KeyTab}, {Code: tea.KeyEnter}, {Code: tea.KeyEscape},
	{Code: 'c', Mod: tea.ModCtrl},
}

// isQuit reports whether cmd is tea.Quit itself. Commands are never called,
// since some of them sleep.
func isQuit(cmd tea.Cmd) bool {
	return cmd != nil && reflect.ValueOf(cmd).Pointer() == reflect.ValueOf(tea.Quit).Pointer()
}

func newFuzzDash(items []DashItem) dashModel {
	return newDashModel(DashConfig{
		Initial:           items,
		Gather:            func() ([]DashItem, error) { return nil, nil },
		Close:             func(string, string) error { return nil },
		FetchDetail:       func(DashItem) (DashDetail, error) { return DashDetail{}, nil },
		FetchCommitDetail: func(string, string) (DashCommitDetail, error) { return DashCommitDetail{}, nil },
		Rebase:            func(string, string) (int, error) { return 0, nil },
	})
}

// dashMsg draws one message the running program could deliver next.
func dashMsg(t *rapid.T, m dashModel) tea.Msg {
	pickPath := func(label string) string {
		if len(m.items) == 0 || rapid.IntRange(0, 9).Draw(t, label+"_stale") == 0 {
			return "/r/gone" // a reply for a row that has since disappeared
		}
		return m.items[rapid.IntRange(0, len(m.items)-1).Draw(t, label)].Path
	}
	pickKey := func(mp any, label string) (string, bool) {
		keys := reflect.ValueOf(mp).MapKeys()
		if len(keys) == 0 {
			return "", false
		}
		return keys[rapid.IntRange(0, len(keys)-1).Draw(t, label)].String(), true
	}
	var err error
	if rapid.IntRange(0, 5).Draw(t, "fails") == 0 {
		err = fmt.Errorf("%s", genText.Draw(t, "errText"))
	}

	switch rapid.SampledFrom([]string{
		"key", "key", "key", "key", "key", "key",
		"resize", "refreshed", "detail", "detail", "commitDetail",
		"closeDone", "closeCleanup", "rebaseDone", "rebaseCleanup", "tick",
	}).Draw(t, "msg") {
	case "key":
		return rapid.SampledFrom(dashKeys).Draw(t, "key")
	case "resize":
		return tea.WindowSizeMsg{
			Width:  rapid.IntRange(1, 220).Draw(t, "width"),
			Height: rapid.IntRange(1, 80).Draw(t, "height"),
		}
	case "refreshed":
		return refreshedMsg{items: genItems(t, "refresh"), err: err}
	case "detail":
		return detailLoadedMsg{path: pickPath("detailPath"), data: genDetail(t), err: err}
	case "commitDetail":
		hash := fmt.Sprintf("%07x", rapid.IntRange(0, 40).Draw(t, "cdHash"))
		return commitDetailLoadedMsg{hash: hash, data: genCommitDetail(t, hash), err: err}
	case "closeDone":
		if p, ok := pickKey(m.closes, "closePath"); ok {
			return dashCloseDoneMsg{path: p, err: err}
		}
	case "closeCleanup":
		if p, ok := pickKey(m.closes, "closePath"); ok {
			return dashCloseCleanupMsg{path: p}
		}
	case "rebaseDone":
		if p, ok := pickKey(m.rebases, "rebasePath"); ok {
			return dashRebaseDoneMsg{path: p, commits: rapid.IntRange(0, 99).Draw(t, "rebased"), err: err}
		}
	case "rebaseCleanup":
		if p, ok := pickKey(m.rebases, "rebasePath"); ok {
			return dashRebaseCleanupMsg{path: p}
		}
	case "tick":
		return tickMsg{}
	}
	return statusTickMsg{}
}

// minCheckedWidth is the narrowest window whose line widths we check. Smaller
// windows are still drawn, so a panic there still fails the test.
const minCheckedWidth = 40

func checkDash(t *rapid.T, m dashModel) {
	if n := len(m.items); n > 0 && (m.cursor < 0 || m.cursor >= n) {
		t.Fatalf("cursor %d outside %d rows", m.cursor, n)
	}
	if m.focus == focusCommits {
		if n := len(m.focusedCommits()); n > 0 && (m.commitCursor < 0 || m.commitCursor >= n) {
			t.Fatalf("commit cursor %d outside %d commits", m.commitCursor, n)
		}
	}

	view := plain(m.View().Content)
	if m.width < minCheckedWidth {
		return // size unknown before the first WindowSizeMsg, or too small to care
	}
	lines := strings.Split(view, "\n")
	if len(lines) > m.height {
		t.Fatalf("frame has %d lines, window has %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Fatalf("line %d is %d wide, window is %d:\n%q\nframe:\n%s", i, w, m.width, l, view)
		}
	}
	// The selected row must stay on screen in any window tall enough for the
	// table plus a footer.
	if len(m.items) > 0 && !m.showHelp && m.height >= len(m.items)+8 &&
		computeLayout(m.items, m.width).mode != layoutTiny && !strings.Contains(view, "▸") {
		t.Fatalf("selected row %d is off screen:\n%s", m.cursor, view)
	}
}

func TestDashProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		m := newFuzzDash(genItems(t, "initial"))
		checkDash(t, m)
		for range rapid.IntRange(1, 80).Draw(t, "steps") {
			msg := dashMsg(t, m)
			next, cmd := m.Update(msg)
			m = next.(dashModel)
			checkDash(t, m)
			if isQuit(cmd) {
				return
			}
			if m.switchTo != "" {
				t.Fatalf("switchTo set to %q without quitting", m.switchTo)
			}
		}
	})
}

var pruneKeys = []tea.KeyPressMsg{
	runeKey('j'), runeKey('k'), runeKey('x'), runeKey('a'), runeKey('z'), runeKey('q'),
	{Code: tea.KeyUp}, {Code: tea.KeyDown}, {Code: tea.KeySpace, Text: " "},
	{Code: tea.KeyEnter}, {Code: tea.KeyEscape},
}

func TestPruneProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// RunPrunePicker returns early on an empty list, so there's always a row.
		n := rapid.IntRange(1, 10).Draw(t, "n")
		items := make([]PruneItem, n)
		selected := make([]bool, n)
		for i := range items {
			items[i] = PruneItem{
				Path:   "/r/" + genText.Draw(t, "name"),
				Branch: genText.Draw(t, "branch"),
				Reason: rapid.SampledFrom([]string{"merged", "closed", "PR #123 merged"}).Draw(t, "reason"),
			}
			selected[i] = true
		}
		m := pruneModel{items: items, selected: selected, width: 80}

		for range rapid.IntRange(1, 40).Draw(t, "steps") {
			var msg tea.Msg = rapid.SampledFrom(pruneKeys).Draw(t, "key")
			if rapid.IntRange(0, 4).Draw(t, "resize") == 0 {
				msg = tea.WindowSizeMsg{Width: rapid.IntRange(1, 200).Draw(t, "width"), Height: 24}
			}
			next, cmd := m.Update(msg)
			m = next.(pruneModel)

			if m.cursor < 0 || m.cursor >= len(m.items) {
				t.Fatalf("cursor %d outside %d rows", m.cursor, len(m.items))
			}
			view := plain(m.View().Content)
			if m.width < minCheckedWidth {
				continue
			}
			for i, l := range strings.Split(view, "\n") {
				if w := lipgloss.Width(l); w > m.width {
					t.Fatalf("line %d is %d wide, window is %d:\n%q", i, w, m.width, l)
				}
			}
			if isQuit(cmd) {
				return
			}
		}
	})
}
