package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func pruneWith(n int) pruneModel {
	items := make([]PruneItem, n)
	for i := range items {
		items[i] = PruneItem{Path: "/r/wt" + string(rune('a'+i)), Branch: "b", Reason: "merged"}
	}
	selected := make([]bool, n)
	for i := range selected {
		selected[i] = true
	}
	return pruneModel{items: items, selected: selected, width: 80}
}

func press(m pruneModel, keys ...tea.KeyPressMsg) (pruneModel, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(k)
		m = next.(pruneModel)
	}
	return m, cmd
}

var (
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	keyDown  = tea.KeyPressMsg{Code: 'j', Text: "j"}
	keyA     = tea.KeyPressMsg{Code: 'a', Text: "a"}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyQ     = tea.KeyPressMsg{Code: 'q', Text: "q"}
)

func TestPruneSpaceToggles(t *testing.T) {
	m, _ := press(pruneWith(3), keyDown, keySpace)
	if want := []bool{true, false, true}; !slices.Equal(m.selected, want) {
		t.Errorf("selected = %v, want %v", m.selected, want)
	}
}

func TestPruneToggleAll(t *testing.T) {
	m, _ := press(pruneWith(3), keyA)
	if !slices.Equal(m.selected, []bool{false, false, false}) {
		t.Errorf("a with all selected should clear all, got %v", m.selected)
	}
	m, _ = press(m, keySpace, keyA)
	if !slices.Equal(m.selected, []bool{true, true, true}) {
		t.Errorf("a with some unselected should select all, got %v", m.selected)
	}
}

func TestPruneEnterAndCancel(t *testing.T) {
	m, cmd := press(pruneWith(2), keyEnter)
	if !m.done || m.cancelled || cmd == nil {
		t.Errorf("enter: done=%v cancelled=%v cmd=%v", m.done, m.cancelled, cmd != nil)
	}
	if m.View().Content != "" {
		t.Error("view should be empty once done")
	}

	m, _ = press(pruneWith(2), keyQ)
	if !m.cancelled {
		t.Error("q did not cancel")
	}
}

func TestPruneViewCountsSelection(t *testing.T) {
	m, _ := press(pruneWith(3), keySpace)
	if out := plain(m.View().Content); !strings.Contains(out, "2/3 selected") {
		t.Errorf("missing selection count:\n%s", out)
	}
}
