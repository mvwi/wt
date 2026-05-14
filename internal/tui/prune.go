// Package tui contains BubbleTea-powered interactive screens.
//
// Each screen is invoked by a command in internal/cmd via a Run* entrypoint
// that takes plain Go data, runs the model, and returns the user's decision.
// Business logic stays in internal/cmd — the TUI is purely a picker/dashboard.
package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PruneItem describes one stale worktree presented to the user.
type PruneItem struct {
	Path   string
	Branch string
	Reason string
}

// PruneResult is what the picker returns to the caller.
// When Cancelled is true, Selected is empty and the caller should abort.
type PruneResult struct {
	Cancelled bool
	Selected  []int
}

// RunPrunePicker presents a multi-select picker over the given items and
// returns the user's selection. All items start selected — prune already
// curates the list to merged/closed PRs, so opt-out matches intent better
// than opt-in.
func RunPrunePicker(items []PruneItem) (PruneResult, error) {
	if len(items) == 0 {
		return PruneResult{}, nil
	}

	selected := make([]bool, len(items))
	for i := range selected {
		selected[i] = true
	}

	m := pruneModel{items: items, selected: selected, width: 80}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return PruneResult{}, err
	}

	pm := final.(pruneModel)
	if pm.cancelled {
		return PruneResult{Cancelled: true}, nil
	}

	var indices []int
	for i, s := range pm.selected {
		if s {
			indices = append(indices, i)
		}
	}
	return PruneResult{Selected: indices}, nil
}

type pruneModel struct {
	items     []PruneItem
	selected  []bool
	cursor    int
	width     int
	cancelled bool
	done      bool
}

var (
	styleCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	styleCheck  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleName   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (m pruneModel) Init() tea.Cmd { return nil }

func (m pruneModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case " ", "x":
			m.selected[m.cursor] = !m.selected[m.cursor]
		case "a":
			allOn := true
			for _, s := range m.selected {
				if !s {
					allOn = false
					break
				}
			}
			for i := range m.selected {
				m.selected[i] = !allOn
			}
		case "enter":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pruneModel) View() string {
	if m.done {
		return ""
	}

	var b strings.Builder
	rule := strings.Repeat("─", min(m.width-4, 60))

	b.WriteString("\n")
	b.WriteString("  " + styleDim.Render("Stale worktrees") + "\n")
	b.WriteString("  " + styleDim.Render(rule) + "\n")

	selectedCount := 0
	for _, s := range m.selected {
		if s {
			selectedCount++
		}
	}

	for i, it := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = styleCursor.Render(" ▸")
		}

		check := styleDim.Render("○")
		if m.selected[i] {
			check = styleCheck.Render("●")
		}

		short := filepath.Base(it.Path)
		nameRendered := styleName.Render(short)
		if !m.selected[i] {
			nameRendered = styleDim.Render(short)
		}

		fmt.Fprintf(&b, "%s %s %s  %s\n",
			cursor, check, nameRendered, styleDim.Render(it.Reason))
		fmt.Fprintf(&b, "       %s\n", styleDim.Render(it.Branch))
	}

	b.WriteString("  " + styleDim.Render(rule) + "\n")
	fmt.Fprintf(&b, "  %s\n",
		styleDim.Render(fmt.Sprintf("%d/%d selected   ↑↓ move · space toggle · a all · enter apply · q cancel",
			selectedCount, len(m.items))))
	return b.String()
}
