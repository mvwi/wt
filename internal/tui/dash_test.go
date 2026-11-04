package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func plain(s string) string { return ansiRe.ReplaceAllString(s, "") }

// commitDash returns a dashboard with one worktree and one commit, focused on
// the commits pane, sized to width×height.
func commitDash(t *testing.T, body string, files int, width, height int) dashModel {
	t.Helper()
	noop := func(DashItem) (DashDetail, error) { return DashDetail{}, nil }
	noopCommit := func(string, string) (DashCommitDetail, error) { return DashCommitDetail{}, nil }
	m := newDashModel(DashConfig{
		Initial:           []DashItem{{Path: "/r/wt", ShortName: "wt", Branch: "feat/x", IsCurrent: true}},
		FetchDetail:       noop,
		FetchCommitDetail: noopCommit,
	})
	m.details["/r/wt"] = &dashDetailCache{data: DashDetail{
		Commits: []DashCommit{{Hash: "abc", Subject: "fix: thing", Age: "1m"}},
	}}
	cd := DashCommitDetail{Hash: "abc", Author: "me", Subject: "fix: thing", Body: body}
	for i := range files {
		cd.Files = append(cd.Files, DashCommitFile{Status: "M", Path: fmt.Sprintf("file%d.go", i), Adds: 1})
	}
	m.commitDetails["abc"] = &dashCommitDetailCache{data: cd}

	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	next, _ = next.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = next.(dashModel)
	if m.focus != focusCommits {
		t.Fatal("tab did not focus the commits pane")
	}
	return m
}

func viewLines(m dashModel) []string {
	return strings.Split(plain(m.View().Content), "\n")
}

func TestDashCommitPanelFitsHeight(t *testing.T) {
	long := strings.Repeat("A long hard-wrapped commit body line that goes on.\n", 30)
	for _, h := range []int{18, 24, 40} {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			lines := viewLines(commitDash(t, long, 15, 80, h))
			if len(lines) > h {
				t.Fatalf("frame has %d lines, terminal has %d", len(lines), h)
			}
			if !strings.Contains(strings.Join(lines[:3], "\n"), "WORKTREES") {
				t.Errorf("worktree table clipped from the top:\n%s", strings.Join(lines, "\n"))
			}
			if !strings.Contains(lines[len(lines)-1], "esc back") {
				t.Errorf("footer missing, last line: %q", lines[len(lines)-1])
			}
		})
	}
}

func TestDashCommitPanelUncappedWhenRoomy(t *testing.T) {
	out := strings.Join(viewLines(commitDash(t, "Short body.", 2, 80, 60)), "\n")
	for _, want := range []string{"Short body.", "file0.go", "file1.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "…") || strings.Contains(out, "more") {
		t.Errorf("trimmed despite room:\n%s", out)
	}
}

func TestFitCommitPanel(t *testing.T) {
	n := func(k int, p string) []string {
		var s []string
		for i := range k {
			s = append(s, fmt.Sprintf("%s%d", p, i))
		}
		return s
	}
	tests := []struct {
		name                string
		body, files         int
		room                int
		wantBody, wantFiles int
		wantMore            string
	}{
		{"fits", 3, 2, 7, 3, 2, ""},
		{"trim body", 10, 2, 7, 3, 2, ""},
		{"drop body, keep files", 10, 4, 5, 0, 4, ""},
		{"trim files", 10, 10, 5, 0, 4, "+7 more"},
		{"only more line", 0, 5, 2, 0, 1, "+5 more"},
		{"no files, tiny room", 5, 0, 1, 0, 0, ""},
		{"no room", 5, 5, 0, 0, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, files := fitCommitPanel(n(tt.body, "b"), n(tt.files, "f"), tt.room)
			if len(body) != tt.wantBody || len(files) != tt.wantFiles {
				t.Fatalf("got %d body, %d files; want %d, %d", len(body), len(files), tt.wantBody, tt.wantFiles)
			}
			if tt.wantMore != "" && plain(files[len(files)-1]) != tt.wantMore {
				t.Errorf("last file row = %q, want %q", plain(files[len(files)-1]), tt.wantMore)
			}
			used := len(body) + len(files)
			if len(body) > 0 {
				used++
			}
			if len(files) > 0 {
				used++
			}
			if used > max(tt.room, 0) {
				t.Errorf("uses %d lines, room is %d", used, tt.room)
			}
		})
	}
}

func TestReflowBody(t *testing.T) {
	in := "The pin made GOTOOLCHAIN fall back to the\nminimum, so releases shipped old.\n\n- item one\n- item two\n  continued\n\nSigned-off-by: me\nRefs: #12"
	want := "The pin made GOTOOLCHAIN fall back to the minimum, so releases shipped old.\n\n- item one\n- item two\n  continued\n\nSigned-off-by: me\nRefs: #12"
	if got := reflowBody(in); got != want {
		t.Errorf("reflowBody:\n got %q\nwant %q", got, want)
	}
}

func TestDashKeys(t *testing.T) {
	m := commitDash(t, "", 0, 80, 30)

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if next.(dashModel).focus != focusWorktrees {
		t.Error("esc did not return focus to worktrees")
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if got := next.(dashModel).switchTo; got != "/r/wt" {
		t.Errorf("s: switchTo = %q, want /r/wt", got)
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c did not quit")
	}
}

func TestDashViewUsesAltScreen(t *testing.T) {
	if !commitDash(t, "", 0, 80, 30).View().AltScreen {
		t.Error("dashboard should render in the alt screen")
	}
}

func TestBuildChecksLineFailingFirst(t *testing.T) {
	checks := []DashCheck{
		{Name: "lint", State: "pass"},
		{Name: "build", State: "pass"},
		{Name: "e2e", State: "pending"},
		{Name: "unit", State: "fail"},
	}
	got := plain(buildChecksLine(checks, 20))
	if !strings.HasPrefix(got, "✗ unit · ◐ e2e") {
		t.Errorf("buildChecksLine = %q, want failing then pending first", got)
	}
	if checks[0].Name != "lint" {
		t.Errorf("buildChecksLine reordered the caller's slice")
	}
}

func TestLayoutFooterFitsWidth(t *testing.T) {
	m := dashModel{items: []DashItem{{Path: "/a", Branch: "feat"}}}
	for _, width := range []int{40, 60, 78, 120} {
		lines := layoutFooter(footerKeys(m), dStyleDim.Render("updated 3s ago"), width)
		joined := plain(strings.Join(lines, "\n"))
		for _, l := range lines {
			if w := lipgloss.Width(l); w > width {
				t.Errorf("width %d: line %q is %d wide", width, plain(l), w)
			}
		}
		for _, want := range []string{"q quit", "? help", "updated 3s ago"} {
			if !strings.Contains(joined, want) {
				t.Errorf("width %d: footer %q lacks %q", width, joined, want)
			}
		}
	}
}

func TestEnterWithoutCommitsShowsNote(t *testing.T) {
	m := dashModel{
		items:   []DashItem{{Path: "/a", Branch: "feat"}},
		details: map[string]*dashDetailCache{"/a": {}},
	}
	next, _ := m.handleWorktreesKey("enter")
	dm := next.(dashModel)
	if dm.focus != focusWorktrees || dm.flash == "" {
		t.Errorf("focus=%v flash=%q, want worktrees focus and a note", dm.focus, dm.flash)
	}
}

func TestHelpOverlayToggles(t *testing.T) {
	m := dashModel{items: []DashItem{{Path: "/a", Branch: "feat"}}}
	next, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	dm := next.(dashModel)
	if !dm.showHelp || !strings.Contains(plain(dm.render()), "GLYPHS") {
		t.Fatalf("? did not open help")
	}
	next, _ = dm.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if next.(dashModel).showHelp {
		t.Errorf("any key should close help")
	}
}
