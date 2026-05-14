package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DashItem describes one worktree row in the dashboard.
type DashItem struct {
	Path        string
	ShortName   string
	Branch      string
	IsCurrent   bool
	IsBase      bool
	StaleDimmed bool // old commit + no open PR — dim the whole row

	Age        string
	Behind     int
	Ahead      int
	DirtyCount int
	Unpushed   int

	PR *DashPR // nil for base branches and worktrees with no PR
}

type DashPR struct {
	Number int
	State  string // "open", "merged", "closed"
	Review DashReview
	CI     DashCI
}

type DashReview struct {
	Approved, Changes, Pending int
}

type DashCI struct {
	Pass, Fail, Pending, Total int
}

// DashDetail is what populates the bottom panel for the focused worktree.
// Any field may be zero-valued — render adapts to what's present.
type DashDetail struct {
	PRTitle  string
	PRAuthor string
	PRBody   string // truncated to the first paragraph at the fetch boundary

	Commits []DashCommit
	Stats   DashDiffStats
	Files   []DashFileChange
	Checks  []DashCheck // named CI checks
}

type DashCommit struct {
	Hash    string
	Subject string
	Age     string
}

type DashDiffStats struct {
	Commits   int
	Files     int
	Additions int
	Deletions int
}

type DashFileChange struct {
	Status string // single-letter: M/A/D/R/...
	Path   string
}

type DashCheck struct {
	Name  string
	State string // "pass", "fail", "pending"
}

// DashCommitDetail is the full info shown for one commit in the third panel
// (visible when the commits pane is focused).
type DashCommitDetail struct {
	Hash    string
	Author  string
	Date    string // formatted for display
	Subject string
	Body    string
	Files   []DashCommitFile
}

type DashCommitFile struct {
	Status string // "A", "M", "D", "B"
	Path   string
	Adds   int
	Dels   int
}

// DashResult is returned to the caller after the dashboard exits.
// SwitchTo is the worktree path the user selected with Enter; empty means quit-without-action.
type DashResult struct {
	SwitchTo string
}

// DashConfig bundles the data sources the dashboard needs. The TUI layer is
// kept independent of git/github by taking these as callbacks from the cmd layer.
type DashConfig struct {
	// Initial is rendered immediately at startup. Re-populated by Gather thereafter.
	Initial []DashItem
	// Gather is invoked on each refresh tick (and manual `r`) to repopulate the list.
	Gather func() ([]DashItem, error)
	// Close removes a worktree and (optionally) its branch. Pass nil to disable
	// the in-dashboard `c` action. Called for any close — risky or safe; the
	// dashboard handles risk-confirmation before invoking this.
	Close func(path, branch string) error
	// FetchDetail returns the bottom-panel detail for a single worktree.
	// Invoked lazily when the cursor lands on a row whose detail isn't cached,
	// and again on each refresh tick for the focused row so live state (CI, etc.)
	// stays current. Pass nil to disable the detail panel.
	FetchDetail func(item DashItem) (DashDetail, error)
	// FetchCommitDetail returns full detail (subject, body, files) for one commit.
	// Invoked lazily when the commit cursor lands on a row whose detail isn't
	// cached. Pass nil to disable the commit-detail panel.
	FetchCommitDetail func(worktreePath, hash string) (DashCommitDetail, error)
	// Rebase fetches the base ref and rebases the given worktree's branch onto
	// it. Returns the number of commits rebased and an error on conflicts or
	// other failure modes. Skips dirty worktrees with an error rather than
	// auto-stashing (the dashboard isn't the right place for surprise stash
	// operations). Pass nil to disable the `r` / `R` actions.
	Rebase func(path, branch string) (commits int, err error)
}

// RunDashboard launches the interactive worktree dashboard. The dashboard owns
// the terminal until the user presses enter (select) or q/esc/ctrl+c (quit).
func RunDashboard(cfg DashConfig) (DashResult, error) {
	m := dashModel{
		items:               cfg.Initial,
		gather:              cfg.Gather,
		close:               cfg.Close,
		fetchDetail:         cfg.FetchDetail,
		fetchCommitDetail:   cfg.FetchCommitDetail,
		rebase:              cfg.Rebase,
		closes:              map[string]*dashCloseState{},
		rebases:             map[string]*dashRebaseState{},
		details:             map[string]*dashDetailCache{},
		detailLoading:       map[string]bool{},
		commitDetails:       map[string]*dashCommitDetailCache{},
		commitDetailLoading: map[string]bool{},
		lastRefresh:         time.Now(),
	}

	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return DashResult{}, err
	}

	fm := final.(dashModel)
	if fm.switchTo != "" {
		return DashResult{SwitchTo: fm.switchTo}, nil
	}
	return DashResult{}, nil
}

// focusPane indicates which list owns navigation keys (j/k/enter).
type focusPane int

const (
	focusWorktrees focusPane = iota
	focusCommits
)

type dashModel struct {
	items               []DashItem
	gather              func() ([]DashItem, error)
	close               func(path, branch string) error
	fetchDetail         func(item DashItem) (DashDetail, error)
	fetchCommitDetail   func(worktreePath, hash string) (DashCommitDetail, error)
	rebase              func(path, branch string) (int, error)
	closes              map[string]*dashCloseState // keyed by worktree path
	rebases             map[string]*dashRebaseState
	details             map[string]*dashDetailCache
	detailLoading       map[string]bool
	commitDetails       map[string]*dashCommitDetailCache // keyed by commit hash
	commitDetailLoading map[string]bool
	cursor              int
	commitCursor        int // index within the focused worktree's commits
	focus               focusPane
	confirm             confirmKind // non-zero when a confirmation prompt is active
	// confirmCloseTarget / confirmCloseName / confirmCloseRisks are populated
	// when confirm == confirmClose; they describe the worktree the user is
	// being asked to nuke. Cleared on prompt dismissal.
	confirmCloseTarget string
	confirmCloseName   string
	confirmCloseBranch string
	confirmCloseRisks  []string
	width              int
	height             int
	refreshing         bool
	lastRefresh        time.Time
	lastErr            error
	switchTo            string
}

// confirmKind tracks which confirmation prompt (if any) is intercepting keys.
type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmRebaseAll
	confirmClose // close a single risky worktree (dirty / unpushed / open PR)
)

// dashRebaseState tracks an in-flight or recently-completed rebase for one row.
type dashRebaseState struct {
	phase   string // "running", "done", "failed"
	commits int    // number of commits rebased on success
	errMsg  string
}

type dashDetailCache struct {
	data DashDetail
	err  error
}

type dashCommitDetailCache struct {
	data DashCommitDetail
	err  error
}

// dashCloseState tracks an in-flight or recently-completed close for one row.
// Persists in the model so the UI can render feedback ("closing…", "✓ closed",
// "✗ failed") even after the gather refresh would otherwise remove the row.
type dashCloseState struct {
	phase  string // "closing", "done", "failed"
	errMsg string
}

type tickMsg time.Time

type refreshedMsg struct {
	items []DashItem
	err   error
}

type dashCloseDoneMsg struct {
	path string
	err  error
}

type dashCloseCleanupMsg struct {
	path string
}

type detailLoadedMsg struct {
	path string
	data DashDetail
	err  error
}

type commitDetailLoadedMsg struct {
	hash string
	data DashCommitDetail
	err  error
}

type dashRebaseDoneMsg struct {
	path    string
	commits int
	err     error
}

type dashRebaseCleanupMsg struct {
	path string
}

const refreshInterval = 15 * time.Second

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshCmd(gather func() ([]DashItem, error)) tea.Cmd {
	return func() tea.Msg {
		items, err := gather()
		return refreshedMsg{items: items, err: err}
	}
}

func dashCloseCmd(closeFn func(path, branch string) error, path, branch string) tea.Cmd {
	return func() tea.Msg {
		err := closeFn(path, branch)
		return dashCloseDoneMsg{path: path, err: err}
	}
}

func fetchDetailCmd(fetch func(item DashItem) (DashDetail, error), item DashItem) tea.Cmd {
	return func() tea.Msg {
		d, err := fetch(item)
		return detailLoadedMsg{path: item.Path, data: d, err: err}
	}
}

func fetchCommitDetailCmd(fetch func(worktreePath, hash string) (DashCommitDetail, error), worktreePath, hash string) tea.Cmd {
	return func() tea.Msg {
		d, err := fetch(worktreePath, hash)
		return commitDetailLoadedMsg{hash: hash, data: d, err: err}
	}
}

func dashRebaseCmd(rebase func(path, branch string) (int, error), path, branch string) tea.Cmd {
	return func() tea.Msg {
		commits, err := rebase(path, branch)
		return dashRebaseDoneMsg{path: path, commits: commits, err: err}
	}
}

func (m dashModel) Init() tea.Cmd {
	return tea.Batch(tickCmd(), m.dispatchDetail(false))
}

// dispatchDetail returns a Cmd that fetches detail for the focused row.
// When force is false, fetches only if the row's detail isn't already cached.
// When force is true (refresh tick), fetches regardless — used to keep CI/etc.
// up to date while the cursor stays put. Returns nil if no fetch is needed
// (no callback, no focus, fetch already in flight).
func (m dashModel) dispatchDetail(force bool) tea.Cmd {
	if m.fetchDetail == nil {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	it := m.items[m.cursor]
	if m.detailLoading[it.Path] {
		return nil
	}
	if !force {
		if _, cached := m.details[it.Path]; cached {
			return nil
		}
	}
	m.detailLoading[it.Path] = true
	return fetchDetailCmd(m.fetchDetail, it)
}

func (m dashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Confirmation prompts intercept all keys.
		if m.confirm != confirmNone {
			return m.handleConfirmKey(msg.String())
		}

		// Global keys (work in any focus mode)
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "R":
			return m.startRebaseAll()
		case "tab":
			return m.toggleFocus()
		case "esc":
			// Zoom out one level. Levels (deepest first):
			//   1. commits focused          → worktrees focused (cursor preserved)
			//   2. worktrees, on a feature  → worktrees, cursor on the trunk
			//   3. worktrees, on the trunk  → quit
			// A few esc presses always exit the app no matter how deep you are.
			if m.focus == focusCommits {
				m.focus = focusWorktrees
				m.commitCursor = 0
				return m, nil
			}
			if m.cursor >= 0 && m.cursor < len(m.items) && !m.items[m.cursor].IsBase {
				for i, it := range m.items {
					if it.IsBase {
						m.cursor = i
						return m, m.dispatchDetail(false)
					}
				}
			}
			return m, tea.Quit
		case "s":
			// Switch to the highlighted worktree from anywhere — the worktree
			// cursor (m.cursor) is preserved regardless of focus mode.
			if m.cursor >= 0 && m.cursor < len(m.items) {
				m.switchTo = m.items[m.cursor].Path
				return m, tea.Quit
			}
			return m, nil
		}

		// Pane-specific keys
		if m.focus == focusCommits {
			return m.handleCommitsKey(msg.String())
		}
		return m.handleWorktreesKey(msg.String())

	case tickMsg:
		if m.refreshing {
			// Already refreshing — just reschedule the next tick.
			return m, tickCmd()
		}
		m.refreshing = true
		return m, tea.Batch(refreshCmd(m.gather), tickCmd())

	case refreshedMsg:
		m.refreshing = false
		if msg.err != nil {
			m.lastErr = msg.err
			return m, nil
		}
		m.items = msg.items
		m.lastErr = nil
		m.lastRefresh = time.Now()
		if m.cursor >= len(m.items) {
			m.cursor = max(0, len(m.items)-1)
		}
		// Refresh the focused row's detail too, so live state (CI/reviews/new
		// commits) stays in sync at the same cadence as the main table.
		return m, m.dispatchDetail(true)

	case detailLoadedMsg:
		delete(m.detailLoading, msg.path)
		m.details[msg.path] = &dashDetailCache{data: msg.data, err: msg.err}
		// Clamp commit cursor if the refreshed commit list shrunk under our feet.
		if m.cursor >= 0 && m.cursor < len(m.items) && msg.path == m.items[m.cursor].Path {
			if n := len(msg.data.Commits); m.commitCursor >= n {
				m.commitCursor = max(0, n-1)
			}
		}
		// If commits are focused, kick off the commit-detail fetch for the
		// currently-cursored commit (it may have changed).
		return m, m.dispatchCommitDetail()

	case commitDetailLoadedMsg:
		delete(m.commitDetailLoading, msg.hash)
		m.commitDetails[msg.hash] = &dashCommitDetailCache{data: msg.data, err: msg.err}
		return m, nil

	case dashRebaseDoneMsg:
		st, ok := m.rebases[msg.path]
		if !ok {
			return m, nil
		}
		if msg.err != nil {
			st.phase = "failed"
			st.errMsg = msg.err.Error()
			return m, nil
		}
		st.phase = "done"
		st.commits = msg.commits
		return m, tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
			return dashRebaseCleanupMsg{path: msg.path}
		})

	case dashRebaseCleanupMsg:
		delete(m.rebases, msg.path)
		if !m.refreshing {
			m.refreshing = true
			return m, refreshCmd(m.gather)
		}
		return m, nil

	case dashCloseDoneMsg:
		st, ok := m.closes[msg.path]
		if !ok {
			return m, nil
		}
		if msg.err != nil {
			st.phase = "failed"
			st.errMsg = msg.err.Error()
			return m, nil
		}
		st.phase = "done"
		// Hold the "✓ closed" indicator briefly, then drop it and refresh —
		// the next gather will remove the row from items.
		return m, tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
			return dashCloseCleanupMsg{path: msg.path}
		})

	case dashCloseCleanupMsg:
		delete(m.closes, msg.path)
		if !m.refreshing {
			m.refreshing = true
			return m, refreshCmd(m.gather)
		}
		return m, nil
	}

	return m, nil
}

// handleWorktreesKey processes keys when the worktree list owns focus.
// Enter zooms into the commits pane (same as Tab); use `s` to switch
// to the highlighted worktree.
func (m dashModel) handleWorktreesKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			return m, m.dispatchDetail(false)
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
			return m, m.dispatchDetail(false)
		}
	case "g", "home":
		m.cursor = 0
		return m, m.dispatchDetail(false)
	case "G", "end":
		m.cursor = max(0, len(m.items)-1)
		return m, m.dispatchDetail(false)
	case "c":
		return m.startClose()
	case "r":
		return m.startRebase()
	case "enter":
		// Zoom in: focus the commits pane (no-op if there are no commits yet).
		return m.toggleFocus()
	}
	return m, nil
}

// handleCommitsKey processes keys when the commits list owns focus.
// j/k navigate within the commits; the third panel below auto-follows the
// cursor to show full detail for whichever commit is selected.
func (m dashModel) handleCommitsKey(key string) (tea.Model, tea.Cmd) {
	commits := m.focusedCommits()
	switch key {
	case "up", "k":
		if m.commitCursor > 0 {
			m.commitCursor--
			return m, m.dispatchCommitDetail()
		}
	case "down", "j":
		if m.commitCursor < len(commits)-1 {
			m.commitCursor++
			return m, m.dispatchCommitDetail()
		}
	case "g", "home":
		m.commitCursor = 0
		return m, m.dispatchCommitDetail()
	case "G", "end":
		m.commitCursor = max(0, len(commits)-1)
		return m, m.dispatchCommitDetail()
	}
	return m, nil
}

// toggleFocus swaps focus between the worktree list and the commits list.
// No-ops if there's nothing to focus into (no commits cached for the
// currently-selected worktree yet). When entering commits focus, dispatches
// a fetch so the soon-to-be-visible commit-detail panel can render.
func (m dashModel) toggleFocus() (tea.Model, tea.Cmd) {
	if m.focus == focusWorktrees {
		if len(m.focusedCommits()) == 0 {
			return m, nil
		}
		m.focus = focusCommits
		m.commitCursor = 0
		return m, m.dispatchCommitDetail()
	}
	m.focus = focusWorktrees
	m.commitCursor = 0
	return m, nil
}

// dispatchCommitDetail returns a Cmd that fetches detail for the commit under
// the commit cursor, or nil if no fetch is needed. Mirrors dispatchDetail's
// cache + loading guards. Commit data is immutable, so the cache never expires.
func (m dashModel) dispatchCommitDetail() tea.Cmd {
	if m.fetchCommitDetail == nil {
		return nil
	}
	if m.focus != focusCommits {
		return nil
	}
	commits := m.focusedCommits()
	if m.commitCursor < 0 || m.commitCursor >= len(commits) {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	hash := commits[m.commitCursor].Hash
	if m.commitDetailLoading[hash] {
		return nil
	}
	if _, cached := m.commitDetails[hash]; cached {
		return nil
	}
	m.commitDetailLoading[hash] = true
	return fetchCommitDetailCmd(m.fetchCommitDetail, m.items[m.cursor].Path, hash)
}

// focusedCommits returns the commit list for the worktree the cursor is on,
// or nil if none is cached / fetched / available.
func (m dashModel) focusedCommits() []DashCommit {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	cache, ok := m.details[m.items[m.cursor].Path]
	if !ok || cache.err != nil {
		return nil
	}
	return cache.data.Commits
}

// rowBusy reports whether a worktree has any async operation pending — close
// or rebase in any phase (running, done-pending-cleanup, or failed). While
// busy, the row's tail cell is displaying state for that operation and
// starting another would either clobber the display or hit a real git
// conflict. All per-row action entry points consult this.
func (m dashModel) rowBusy(path string) bool {
	if _, ok := m.closes[path]; ok {
		return true
	}
	if _, ok := m.rebases[path]; ok {
		return true
	}
	return false
}

// handleConfirmKey intercepts keys while a confirmation prompt is active.
// y/enter confirms; n/esc/any-other-key cancels.
func (m dashModel) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y", "enter":
		kind := m.confirm
		m.confirm = confirmNone
		switch kind {
		case confirmRebaseAll:
			return m.executeRebaseAll()
		case confirmClose:
			return m.executeConfirmedClose()
		}
		return m, nil
	default:
		// Cancel: clear the prompt and any close-target state we may have set
		// when entering it.
		m.confirm = confirmNone
		m.confirmCloseTarget = ""
		m.confirmCloseName = ""
		m.confirmCloseBranch = ""
		m.confirmCloseRisks = nil
		return m, nil
	}
}

// startRebase queues an async rebase for the highlighted worktree. No-ops
// when the row isn't a valid rebase target (base branch, already rebasing,
// no rebase callback).
func (m dashModel) startRebase() (tea.Model, tea.Cmd) {
	if m.rebase == nil {
		return m, nil
	}
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}
	it := m.items[m.cursor]
	if it.IsBase {
		return m, nil
	}
	if m.rowBusy(it.Path) {
		return m, nil
	}
	m.rebases[it.Path] = &dashRebaseState{phase: "running"}
	return m, dashRebaseCmd(m.rebase, it.Path, it.Branch)
}

// startRebaseAll triggers the confirmation prompt for bulk rebase. Actual
// dispatch happens in executeRebaseAll after the user answers y/enter.
func (m dashModel) startRebaseAll() (tea.Model, tea.Cmd) {
	if m.rebase == nil {
		return m, nil
	}
	// Count eligible worktrees to give the user concrete numbers.
	if m.countRebaseTargets() == 0 {
		return m, nil
	}
	m.confirm = confirmRebaseAll
	return m, nil
}

// executeRebaseAll dispatches a rebase for every non-base worktree concurrently.
// Each rebase runs in its own goroutine via dashRebaseCmd; results arrive as
// dashRebaseDoneMsg and are rendered per-row.
func (m dashModel) executeRebaseAll() (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for _, it := range m.items {
		if it.IsBase {
			continue
		}
		if m.rowBusy(it.Path) {
			continue
		}
		m.rebases[it.Path] = &dashRebaseState{phase: "running"}
		cmds = append(cmds, dashRebaseCmd(m.rebase, it.Path, it.Branch))
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}

// countRebaseTargets returns how many worktrees would be touched by `R`.
func (m dashModel) countRebaseTargets() int {
	n := 0
	for _, it := range m.items {
		if it.IsBase {
			continue
		}
		if m.rowBusy(it.Path) {
			continue
		}
		n++
	}
	return n
}

// startClose queues an async close for the highlighted worktree.
//
// Behavior depends on risk:
//   - safe (clean, no unpushed, no open PR): dispatches immediately, same
//     non-blocking UX as the old `p prune`.
//   - risky (dirty, unpushed, or open PR): sets confirmClose and shows a
//     footer prompt naming the specific risks. Actual close runs only after
//     the user confirms via handleConfirmKey.
//
// No-ops when the row isn't closeable (base branch, current worktree, already
// closing). Multiple concurrent closes are supported.
func (m dashModel) startClose() (tea.Model, tea.Cmd) {
	if m.close == nil {
		return m, nil
	}
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}
	it := m.items[m.cursor]
	if !canClose(it) {
		return m, nil
	}
	if m.rowBusy(it.Path) {
		return m, nil
	}

	risks := closeRisks(it)
	if len(risks) == 0 {
		// Safe: just do it.
		m.closes[it.Path] = &dashCloseState{phase: "closing"}
		return m, dashCloseCmd(m.close, it.Path, it.Branch)
	}

	// Risky: show confirmation prompt at the footer. Store the target so
	// handleConfirmKey can dispatch it on y/enter.
	m.confirm = confirmClose
	m.confirmCloseTarget = it.Path
	m.confirmCloseName = it.ShortName
	m.confirmCloseBranch = it.Branch
	m.confirmCloseRisks = risks
	return m, nil
}

// executeConfirmedClose dispatches the close that was waiting on confirmation.
// Called from handleConfirmKey after the user answers y/enter.
func (m dashModel) executeConfirmedClose() (tea.Model, tea.Cmd) {
	path := m.confirmCloseTarget
	branch := m.confirmCloseBranch
	m.confirmCloseTarget = ""
	m.confirmCloseName = ""
	m.confirmCloseBranch = ""
	m.confirmCloseRisks = nil
	if path == "" || m.close == nil {
		return m, nil
	}
	if m.rowBusy(path) {
		return m, nil
	}
	m.closes[path] = &dashCloseState{phase: "closing"}
	return m, dashCloseCmd(m.close, path, branch)
}

// canClose reports whether a row is eligible to be closed from the dashboard.
// Refuses the base branch (you can't remove it) and the current worktree
// (closing the dashboard's own cwd would tear out the ground beneath us).
// Everything else is fair game; risky rows get a confirmation via closeRisks.
func canClose(it DashItem) bool {
	if it.IsBase || it.IsCurrent {
		return false
	}
	return true
}

// closeRisks returns human-readable reasons a worktree is risky to close.
// Empty slice means safe (proceed without confirmation). Order is stable
// (dirty → unpushed → PR state) so the prompt reads consistently.
func closeRisks(it DashItem) []string {
	var risks []string
	if it.DirtyCount > 0 {
		risks = append(risks, fmt.Sprintf("%d uncommitted change%s", it.DirtyCount, plural(it.DirtyCount)))
	}
	if it.Unpushed > 0 {
		risks = append(risks, fmt.Sprintf("%d unpushed commit%s", it.Unpushed, plural(it.Unpushed)))
	}
	if it.PR != nil && it.PR.State == "open" {
		risks = append(risks, fmt.Sprintf("PR #%d still open", it.PR.Number))
	}
	return risks
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// --- styles ---

var (
	// Color "8" (ANSI bright black) renders unreliably across terminals — on
	// macOS Terminal's default themes it's a near-white light gray. The 256-
	// color palette gives us a predictable dark gray that reads as "dim"
	// everywhere. 244 ≈ medium-dark gray; 238 ≈ very dark gray (used for
	// disabled-key strikethrough).
	dStyleHeader    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	dStyleCurrent   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	dStyleCursor    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	dStyleName      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dStyleBranch    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	dStyleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	dStyleDisabled  = lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Strikethrough(true)
	dStyleGreen     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dStyleYellow    = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dStyleRed       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dStyleBlue      = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	dStyleMagenta   = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	dStyleCyan      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	dStyleCursorRow = lipgloss.NewStyle().Background(lipgloss.Color("237"))
	dStyleSection   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	dStyleBold      = lipgloss.NewStyle().Bold(true)
	dStylePlain     = lipgloss.NewStyle()
)

// issueRefRe matches GitHub-style issue/PR references in free text (e.g. "#42"
// in "Closes #42"). We don't try to be clever about preceded-by-letter cases
// (foo#42); GitHub itself is lenient here.
var issueRefRe = regexp.MustCompile(`#\d+`)

// colorizeBody renders free-form text in dim while highlighting issue
// references (`#123`) in blue. Used for PR bodies and commit bodies.
func colorizeBody(s string) string {
	matches := issueRefRe.FindAllStringIndex(s, -1)
	if len(matches) == 0 {
		return dStyleDim.Render(s)
	}
	var b strings.Builder
	pos := 0
	for _, m := range matches {
		if m[0] > pos {
			b.WriteString(dStyleDim.Render(s[pos:m[0]]))
		}
		b.WriteString(dStyleBlue.Render(s[m[0]:m[1]]))
		pos = m[1]
	}
	if pos < len(s) {
		b.WriteString(dStyleDim.Render(s[pos:]))
	}
	return b.String()
}

// styleForAge returns a foreground style based on how recent a commit is.
// Fresh worktrees (hours/minutes) pop in green; normal age (days) is default;
// older (weeks/months/years) stays dim — same as the stale-row behavior.
func styleForAge(age string) lipgloss.Style {
	if age == "" {
		return dStyleDim
	}
	var unit string
	for i, c := range age {
		if c < '0' || c > '9' {
			unit = age[i:]
			break
		}
	}
	switch unit {
	case "s", "m", "h":
		return dStyleGreen
	case "d":
		return dStylePlain
	default:
		return dStyleDim
	}
}

// stylesForFilePath colors paths by their semantic category. Tests stand out
// in magenta so churn there is distinguishable from real source changes;
// configs are cyan; docs are dim; everything else (code) uses the default.
func styleForFilePath(path string) lipgloss.Style {
	lower := strings.ToLower(path)
	// Test / spec files
	base := lower
	if i := strings.LastIndex(lower, "/"); i >= 0 {
		base = lower[i+1:]
	}
	if strings.Contains(base, "test") || strings.Contains(base, "spec") {
		return dStyleMagenta
	}
	// Docs
	switch {
	case strings.HasSuffix(lower, ".md"),
		strings.HasSuffix(lower, ".txt"),
		strings.HasSuffix(lower, ".rst"):
		return dStyleDim
	}
	// Config
	switch {
	case strings.HasSuffix(lower, ".toml"),
		strings.HasSuffix(lower, ".yaml"),
		strings.HasSuffix(lower, ".yml"),
		strings.HasSuffix(lower, ".json"),
		strings.HasSuffix(lower, ".ini"),
		strings.HasSuffix(lower, ".env"):
		return dStyleCyan
	}
	return dStylePlain
}

// Column widths. Branch flexes between min/max based on terminal width; the
// rest are fixed because their content is bounded (numbers, glyphs, short
// states). When the terminal can't fit PR+Review+CI, we collapse them into a
// single Status cell (colStatus) — see dashLayout / layoutMode below.
const (
	colBranchMin = 16
	colBranchMax = 64
	colAge       = 4
	colDirty     = 4
	colSync      = 8
	colPR        = 6
	colReview    = 8
	colCI        = 8
	colStatus    = 16
)

// layoutMode describes how the dashboard responds to terminal width. Modes
// degrade as the terminal shrinks: full → narrow → tiny.
type layoutMode int

const (
	layoutFull   layoutMode = iota // PR + Review + CI as three columns
	layoutNarrow                   // PR/Review/CI collapsed into a Status cell
	layoutTiny                     // below the narrow threshold — show resize hint
)

// dashLayout is the computed column geometry for one render pass. Computed
// once per View() from m.width + the current items so every renderer sees the
// same dimensions. termWidth=0 (before the first WindowSizeMsg) is handled by
// falling back to an 80-col baseline in computeLayout.
type dashLayout struct {
	mode         layoutMode
	termWidth    int
	contentWidth int // total visible row width, including the leading "  " indent
	nameWidth    int
	branchWidth  int
	ageWidth     int
	dirtyWidth   int
	syncWidth    int
	prWidth      int
	reviewWidth  int
	ciWidth      int
	statusWidth  int // used only when mode == layoutNarrow
}

// tailWidth is the width of the trailing PR/Review/CI cluster (full mode) or
// the Status cell (narrow mode). Used by per-row overlays (prune, rebase) so
// they fill the same horizontal slot.
func (l dashLayout) tailWidth() int {
	if l.mode == layoutNarrow {
		return l.statusWidth
	}
	return l.prWidth + 1 + l.reviewWidth + 1 + l.ciWidth
}

// ruleWidth is the number of "─" characters that fit between the leading
// "  " margin and the row's right edge. Used for dividers and for aligning
// the footer's right-edge status.
func (l dashLayout) ruleWidth() int { return l.contentWidth - 2 }

// detailWidth is the width budget passed into the detail/commit-detail
// panels — same as ruleWidth so panel content lines up under the dividers.
func (l dashLayout) detailWidth() int { return l.contentWidth - 2 }

// minNarrowWidth returns the smallest terminal width that fits the narrow
// layout for the given name column — used by the tiny-mode hint message.
func minNarrowWidth(nameWidth int) int {
	// "  " + cursor(2) + current(2) + name + "  " + minBranch + " " + age + " "
	// + dirty + " " + sync + " " + status
	return 2 + 2 + 2 + nameWidth + 2 + colBranchMin + 1 + colAge + 1 + colDirty + 1 + colSync + 1 + colStatus
}

// computeLayout chooses a mode and column widths for the given terminal size.
// Branch column flexes to consume any slack; PR/Review/CI collapse to a single
// Status column when there isn't enough room. termWidth <= 0 (initial render
// before WindowSizeMsg) falls back to 80 columns — a conservative default
// that fits in tmux, vsplit panes, and most "default" terminal sizes.
func computeLayout(items []DashItem, termWidth int) dashLayout {
	nameWidth := 8
	for _, it := range items {
		nameWidth = max(nameWidth, lipgloss.Width(it.ShortName))
	}

	if termWidth <= 0 {
		termWidth = 80
	}
	avail := termWidth - 2 // leading "  " indent

	// Fixed widths excluding the branch column.
	fixedFull := 2 + 2 + nameWidth + 2 + 1 + colAge + 1 + colDirty + 1 + colSync + 1 + (colPR + 1 + colReview + 1 + colCI)
	fixedNarrow := 2 + 2 + nameWidth + 2 + 1 + colAge + 1 + colDirty + 1 + colSync + 1 + colStatus

	lo := dashLayout{
		termWidth:   termWidth,
		nameWidth:   nameWidth,
		ageWidth:    colAge,
		dirtyWidth:  colDirty,
		syncWidth:   colSync,
		prWidth:     colPR,
		reviewWidth: colReview,
		ciWidth:     colCI,
		statusWidth: colStatus,
	}

	switch {
	case avail >= fixedFull+colBranchMin:
		lo.mode = layoutFull
		lo.branchWidth = clampInt(avail-fixedFull, colBranchMin, colBranchMax)
		lo.contentWidth = 2 + fixedFull + lo.branchWidth // "  " + row content
	case avail >= fixedNarrow+colBranchMin:
		lo.mode = layoutNarrow
		lo.branchWidth = clampInt(avail-fixedNarrow, colBranchMin, colBranchMax)
		lo.contentWidth = 2 + fixedNarrow + lo.branchWidth
	default:
		lo.mode = layoutTiny
	}
	return lo
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// bodyLineCap returns how many PR body lines the detail panel may render.
// Caps at 6 in comfortable terminals; scales down on short ones so the body
// doesn't push commits/files off the bottom of the screen. The "-20" is a
// rough reservation for the table, sections, and footer — not exact, just
// predictable: 26-row terminals get the full 6 lines, 22 rows give 2, 20 or
// fewer drop the body entirely. termHeight=0 (before the first WindowSizeMsg)
// falls back to the ceiling.
func bodyLineCap(termHeight int) int {
	const ceiling = 6
	if termHeight <= 0 {
		return ceiling
	}
	n := termHeight - 20
	switch {
	case n < 0:
		return 0
	case n > ceiling:
		return ceiling
	}
	return n
}

func (m dashModel) View() string {
	var b strings.Builder
	lo := computeLayout(m.items, m.width)

	// Below the narrow threshold: render a minimal hint and stop. We don't try
	// to squeeze content into a too-narrow viewport — the columns end up wrapped
	// and unreadable. The user just resizes and the next WindowSizeMsg recovers.
	if lo.mode == layoutTiny {
		b.WriteString("\n")
		b.WriteString("  " + dStyleHeader.Render("WORKTREES") + "\n\n")
		b.WriteString("  " + dStyleYellow.Render("terminal too narrow") + "\n")
		b.WriteString("  " + dStyleDim.Render(fmt.Sprintf("need %d cols, have %d — resize and the dashboard returns",
			minNarrowWidth(lo.nameWidth), m.width)) + "\n\n")
		b.WriteString("  " + dStyleDim.Render("q quit") + "\n")
		return b.String()
	}

	ruleWidth := lo.ruleWidth()

	// Header
	b.WriteString("\n")
	b.WriteString("  " + dStyleHeader.Render("WORKTREES") + "\n")
	b.WriteString("  " + dStyleDim.Render(strings.Repeat("─", ruleWidth)) + "\n")

	// Column headers — shape depends on mode.
	var header string
	if lo.mode == layoutFull {
		header = fmt.Sprintf("    %-*s  %-*s %-*s %-*s %-*s %-*s %-*s %-*s",
			lo.nameWidth, "Name",
			lo.branchWidth, "Branch",
			lo.ageWidth, "Age",
			lo.dirtyWidth, "Dirty",
			lo.syncWidth, "Sync",
			lo.prWidth, "PR",
			lo.reviewWidth, "Review",
			lo.ciWidth, "CI",
		)
	} else {
		header = fmt.Sprintf("    %-*s  %-*s %-*s %-*s %-*s %-*s",
			lo.nameWidth, "Name",
			lo.branchWidth, "Branch",
			lo.ageWidth, "Age",
			lo.dirtyWidth, "Dirty",
			lo.syncWidth, "Sync",
			lo.statusWidth, "Status",
		)
	}
	b.WriteString(dStyleDim.Render(header) + "\n")

	// Rows
	for i, it := range m.items {
		b.WriteString(m.renderRow(it, i == m.cursor, lo))
		b.WriteString("\n")
	}

	// Detail panel for the focused row
	if m.fetchDetail != nil {
		b.WriteString("  " + dStyleDim.Render(strings.Repeat("─", ruleWidth)) + "\n")
		b.WriteString(m.renderDetail(lo.detailWidth(), bodyLineCap(m.height)))
	}

	// Third panel: commit detail — only when commits pane is focused.
	if m.focus == focusCommits && m.fetchCommitDetail != nil {
		b.WriteString("  " + dStyleDim.Render(strings.Repeat("─", ruleWidth)) + "\n")
		b.WriteString(m.renderCommitDetail(lo.detailWidth()))
	}

	// Footer
	b.WriteString("  " + dStyleDim.Render(strings.Repeat("─", ruleWidth)) + "\n")

	if m.confirm != confirmNone {
		b.WriteString("  " + renderConfirmPrompt(m) + "\n")
	} else {
		status := buildStatusLine(m)
		// footerKeys returns pre-styled output (per-key enabled/disabled
		// rendering), so don't wrap again with dStyleDim here.
		keys := footerKeys(m)
		b.WriteString("  " + keys)
		if status != "" {
			pad := max(2, ruleWidth-lipgloss.Width(keys)-lipgloss.Width(status))
			b.WriteString(strings.Repeat(" ", pad) + status)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderConfirmPrompt renders the inline footer prompt shown while a
// confirmation is active. y/enter confirms; any other key cancels.
func renderConfirmPrompt(m dashModel) string {
	switch m.confirm {
	case confirmRebaseAll:
		// Find base branch name for the prompt — falls back to "base" if absent.
		base := "base"
		for _, it := range m.items {
			if it.IsBase {
				base = it.Branch
				if base == "" {
					base = it.ShortName
				}
				break
			}
		}
		n := m.countRebaseTargets()
		return fmt.Sprintf("%s %s   %s %s",
			dStyleBold.Render(fmt.Sprintf("Rebase %d worktrees onto %s?", n, base)),
			dStyleDim.Render("·"),
			dStyleGreen.Render("[y]es"),
			dStyleDim.Render("[n]o · any other key cancels"),
		)

	case confirmClose:
		// Risks list reads as "X, Y, and Z" — matches what the row already shows
		// in the Dirty/Sync/PR cells, so the prompt is a confirmation of visible
		// state, not a surprise revelation.
		risks := strings.Join(m.confirmCloseRisks, ", ")
		return fmt.Sprintf("%s %s %s %s   %s %s",
			dStyleBold.Render("Close "+m.confirmCloseName+"?"),
			dStyleDim.Render("·"),
			dStyleYellow.Render(risks),
			dStyleDim.Render("·"),
			dStyleGreen.Render("[y]es"),
			dStyleDim.Render("[n]o · any other key cancels"),
		)
	}
	return ""
}

// footerEntry is one keybinding hint in the footer bar. enabled=false renders
// dim+strikethrough so the user can see at a glance which keys won't do
// anything for the currently-focused row.
type footerEntry struct {
	keys    string
	label   string
	enabled bool
}

// footerKeys returns the keybinding hint line, already styled. Per-key
// styling reflects current state: r/R/c get a struck-through disabled look
// when the cursor row can't accept them (busy with another op, base branch,
// or current worktree). Navigation and quit keys are always enabled.
func footerKeys(m dashModel) string {
	var entries []footerEntry

	if m.focus == focusCommits {
		// Commits pane has no per-row actions to disable.
		entries = []footerEntry{
			{"↑↓", "move", true},
			{"esc", "back", true},
			{"s", "switch", true},
			{"q", "quit", true},
		}
	} else {
		// Resolve the current row's state. cursor out of range only happens
		// when items is empty — treat actions as disabled in that case.
		var cur DashItem
		var rowExists bool
		if m.cursor >= 0 && m.cursor < len(m.items) {
			cur = m.items[m.cursor]
			rowExists = true
		}
		busy := rowExists && m.rowBusy(cur.Path)

		canR := rowExists && !cur.IsBase && !busy
		canRAll := m.countRebaseTargets() > 0
		canC := rowExists && canClose(cur) && !busy

		entries = []footerEntry{
			{"↑↓", "move", true},
			{"enter", "commits", true},
			{"s", "switch", rowExists},
			{"r", "rebase", canR},
			{"R", "rebase all", canRAll},
			{"c", "close", canC},
			{"q", "quit", true},
		}
	}

	sep := dStyleDim.Render(" · ")
	var parts []string
	for _, e := range entries {
		text := e.keys + " " + e.label
		if e.enabled {
			parts = append(parts, dStyleDim.Render(text))
		} else {
			parts = append(parts, dStyleDisabled.Render(text))
		}
	}
	return strings.Join(parts, sep)
}

func buildStatusLine(m dashModel) string {
	if m.lastErr != nil {
		return dStyleRed.Render("refresh failed: " + truncate(m.lastErr.Error(), 30))
	}
	if m.refreshing {
		return dStyleDim.Render("refreshing…")
	}
	elapsed := time.Since(m.lastRefresh).Truncate(time.Second)
	return dStyleDim.Render(fmt.Sprintf("updated %s ago", elapsed))
}

func (m dashModel) renderRow(it DashItem, isCursor bool, lo dashLayout) string {
	currentGlyph := "  "
	if it.IsCurrent {
		currentGlyph = dStyleCurrent.Render("● ")
	}
	cursorGlyph := "  "
	if isCursor {
		cursorGlyph = dStyleCursor.Render("▸ ")
	}

	short := truncate(it.ShortName, lo.nameWidth)
	shortRendered := dStyleName.Render(fmt.Sprintf("%-*s", lo.nameWidth, short))
	if it.StaleDimmed {
		shortRendered = dStyleDim.Render(fmt.Sprintf("%-*s", lo.nameWidth, short))
	}

	branch := ""
	if it.Branch != it.ShortName {
		branch = it.Branch
	}
	branchRendered := dStyleBranch.Render(fmt.Sprintf("%-*s", lo.branchWidth, truncate(branch, lo.branchWidth)))

	age := fmt.Sprintf("%-*s", lo.ageWidth, it.Age)
	// Recent commits get a green tint so fresh worktrees pop. Stale rows always
	// stay dim (StaleDimmed is set by the cmd layer based on the threshold).
	ageStyle := styleForAge(it.Age)
	if it.StaleDimmed {
		ageStyle = dStyleDim
	}
	ageRendered := ageStyle.Render(age)

	dirty := dStyleDim.Render(fmt.Sprintf("%-*s", lo.dirtyWidth, "—"))
	if it.DirtyCount > 0 {
		dirty = dStyleYellow.Render(fmt.Sprintf("%-*d", lo.dirtyWidth, it.DirtyCount))
	}

	sync := buildSync(it)
	syncRendered := padAnsi(sync, lo.syncWidth)

	// Tail cluster: PR/Review/CI in full mode, single Status cell in narrow mode.
	// Per-row overlays (close, rebase) fill the same slot width so they line up
	// with the column header above.
	var tail string
	tailW := lo.tailWidth()
	switch {
	case m.closes[it.Path] != nil:
		tail = renderCloseCell(m.closes[it.Path], tailW)
	case m.rebases[it.Path] != nil:
		tail = renderRebaseCell(m.rebases[it.Path], tailW)
	case lo.mode == layoutNarrow:
		tail = padAnsi(buildStatusCell(it), lo.statusWidth)
	default:
		pr, review, ci := buildPRCells(it)
		tail = padAnsi(pr, lo.prWidth) + " " + padAnsi(review, lo.reviewWidth) + " " + padAnsi(ci, lo.ciWidth)
	}

	row := fmt.Sprintf("%s%s%s  %s %s %s %s %s",
		cursorGlyph, currentGlyph, shortRendered, branchRendered,
		ageRendered, dirty, syncRendered, tail,
	)

	if isCursor {
		// Re-render the whole row with a background tint. Lipgloss will preserve
		// the inner foreground colors and just add a background.
		row = dStyleCursorRow.Render(row)
	}

	return "  " + row
}

// renderDetail renders the bottom panel for the focused worktree. width is
// the panel width budget (typically the rule width); bodyMax is how many PR
// body lines may be shown before truncation kicks in. Callers compute bodyMax
// from terminal height — see View().
func (m dashModel) renderDetail(width, bodyMax int) string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	it := m.items[m.cursor]

	var b strings.Builder

	cache, hasCache := m.details[it.Path]
	hasPR := hasCache && cache.err == nil && it.PR != nil && cache.data.PRTitle != ""

	// Title line:
	//   PR rows:     "PR #N  <title>            @author"   (name/branch already in row above)
	//   non-PR rows: "<name> · <branch>"
	// Truncate the PR title with an ellipsis if it would push @author off the
	// right edge. Plain ANSI strings can't be truncated safely, so we slice the
	// raw title first and re-apply the bold style.
	var prefix, titleRendered, authorRendered string
	if hasPR {
		prefix = dStyleBlue.Render(fmt.Sprintf("PR #%d", it.PR.Number)) + "  "
		if cache.data.PRAuthor != "" {
			authorRendered = dStyleCyan.Render("@" + cache.data.PRAuthor)
		}
		// Budget for the title: width - leading "  " - prefix - "  " separator - author
		titleMax := width - 2 - lipgloss.Width(prefix)
		if authorRendered != "" {
			titleMax -= 2 + lipgloss.Width(authorRendered)
		}
		titleRendered = dStyleBold.Render(truncate(cache.data.PRTitle, titleMax))
	} else {
		name := dStyleName.Render(it.ShortName)
		if it.Branch != "" && it.Branch != it.ShortName {
			name += "  " + dStyleDim.Render("· "+it.Branch)
		}
		titleRendered = name
	}

	titleLine := "  " + prefix + titleRendered
	if authorRendered != "" {
		pad := max(2, width-lipgloss.Width(titleLine)-lipgloss.Width(authorRendered)+2)
		titleLine += strings.Repeat(" ", pad) + authorRendered
	}
	b.WriteString(titleLine + "\n")

	if !hasCache {
		fmt.Fprintf(&b, "  %s\n", dStyleDim.Render("loading detail…"))
		return b.String()
	}
	if cache.err != nil {
		fmt.Fprintf(&b, "  %s\n", dStyleRed.Render("detail unavailable: "+truncate(cache.err.Error(), width-22)))
		return b.String()
	}

	d := cache.data

	// PR body (first paragraph) — wraps each source line independently so that
	// bullet lists stay readable. Capped at bodyMax visible lines so a long
	// body doesn't push CHANGES/COMMITS/FILES off-screen. Issue refs are
	// colored blue by colorizeBody. firstParagraph (in cmd/dash.go) already
	// stripped leading markdown headers.
	if d.PRBody != "" && bodyMax > 0 {
		var lines []string
		for raw := range strings.SplitSeq(d.PRBody, "\n") {
			raw = strings.TrimRight(raw, " \t")
			if raw == "" {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, wrapText(raw, width-4)...)
		}
		truncated := false
		if len(lines) > bodyMax {
			lines = lines[:bodyMax]
			truncated = true
		}
		b.WriteString("\n")
		for i, line := range lines {
			if truncated && i == len(lines)-1 {
				line = strings.TrimRight(line, " ") + dStyleDim.Render(" …")
			}
			fmt.Fprintf(&b, "  %s\n", colorizeBody(line))
		}
	}

	// CHANGES line — diff vs base. Behind count comes from the row data (no
	// extra git call needed) and renders only when nonzero so simple branches
	// stay tidy. We also render when ahead=0 but behind>0, since "you're 2
	// commits stale" is useful info even without a diff.
	if d.Stats.Commits > 0 || d.Stats.Files > 0 || it.Behind > 0 {
		fmt.Fprintf(&b, "\n  %s   %s\n",
			dStyleSection.Render("CHANGES"),
			buildStatsLine(d.Stats, it.Behind),
		)
	}

	// COMMITS section (with focus-aware cursor)
	if len(d.Commits) > 0 {
		header := "COMMITS"
		if m.focus == focusCommits {
			header += dStyleDim.Render("  ▸ j/k navigate · esc back")
		}
		fmt.Fprintf(&b, "\n  %s\n", dStyleSection.Render(header))
		typeWidth := commitTypeWidth(d.Commits)
		ageWidth := commitAgeWidth(d.Commits)
		subjectWidth := max(20, width-4-2-typeWidth-1-1-ageWidth)
		for i, c := range d.Commits {
			b.WriteString(m.renderCommitRow(c, i, typeWidth, subjectWidth, ageWidth))
			b.WriteString("\n")
		}
	}

	// FILES line
	if len(d.Files) > 0 {
		fmt.Fprintf(&b, "\n  %s     %s\n",
			dStyleSection.Render("FILES"),
			buildFilesLine(d.Files, width-12),
		)
	}

	// CI line
	if len(d.Checks) > 0 {
		fmt.Fprintf(&b, "  %s        %s\n",
			dStyleSection.Render("CI"),
			buildChecksLine(d.Checks, width-12),
		)
	}

	return b.String()
}

// renderCommitDetail renders the third panel — the full detail for the commit
// under the commit cursor when the commits pane is focused.
func (m dashModel) renderCommitDetail(width int) string {
	commits := m.focusedCommits()
	if m.commitCursor < 0 || m.commitCursor >= len(commits) {
		return ""
	}
	hash := commits[m.commitCursor].Hash
	cache, hasCache := m.commitDetails[hash]

	var b strings.Builder

	// COMMIT section — subject (with conventional-commit color), then author/date,
	// then wrapped body.
	if !hasCache {
		fmt.Fprintf(&b, "  %s    %s\n",
			dStyleSection.Render("COMMIT"),
			dStyleDim.Render("loading…"),
		)
		return b.String()
	}
	if cache.err != nil {
		fmt.Fprintf(&b, "  %s    %s\n",
			dStyleSection.Render("COMMIT"),
			dStyleRed.Render("error: "+truncate(cache.err.Error(), width-20)),
		)
		return b.String()
	}

	d := cache.data
	const labelCol = 12 // "COMMIT    " + indent
	contentWidth := max(20, width-labelCol)

	// Render subject with conventional-commit type colored.
	typeCell, subjectRest := formatCommitType(d.Subject, 0)
	subjectRendered := strings.TrimSpace(typeCell) + " " + subjectRest
	if strings.TrimSpace(typeCell) == "" {
		subjectRendered = subjectRest
	}
	fmt.Fprintf(&b, "  %s    %s\n",
		dStyleSection.Render("COMMIT"),
		truncate(subjectRendered, contentWidth),
	)

	// Author · date line — author handle in cyan, date dim.
	meta := dStyleCyan.Render("@" + d.Author)
	if d.Date != "" {
		meta += dStyleDim.Render("  ·  " + shortenDate(d.Date))
	}
	fmt.Fprintf(&b, "  %s%s\n", strings.Repeat(" ", labelCol), meta)

	// Body (wrapped) — dim background text with issue refs colored blue.
	if d.Body != "" {
		fmt.Fprintln(&b)
		for _, line := range wrapText(d.Body, contentWidth) {
			fmt.Fprintf(&b, "  %s%s\n", strings.Repeat(" ", labelCol), colorizeBody(line))
		}
	}

	// FILES section — per-file row with status, path, +adds −dels right-aligned.
	if len(d.Files) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "  %s     %s\n",
			dStyleSection.Render("FILES"),
			renderCommitFileRow(d.Files[0], contentWidth-1),
		)
		for _, f := range d.Files[1:] {
			fmt.Fprintf(&b, "  %s%s\n", strings.Repeat(" ", labelCol), renderCommitFileRow(f, contentWidth-1))
		}
	}

	return b.String()
}

// renderCommitFileRow renders one file row in the FILES section:
//
//	M  path/to/file.go                            +12 −3
//
// Status letter is colored, counts are right-aligned within `width`.
func renderCommitFileRow(f DashCommitFile, width int) string {
	var statusGlyph string
	switch f.Status {
	case "A":
		statusGlyph = dStyleGreen.Render("A")
	case "D":
		statusGlyph = dStyleRed.Render("D")
	case "M":
		statusGlyph = dStyleYellow.Render("M")
	case "B":
		statusGlyph = dStyleMagenta.Render("B")
	default:
		statusGlyph = dStyleDim.Render(f.Status)
	}

	counts := ""
	if f.Status != "B" {
		counts = dStyleGreen.Render(fmt.Sprintf("+%d", f.Adds)) + "  " +
			dStyleRed.Render(fmt.Sprintf("−%d", f.Dels))
	} else {
		counts = dStyleDim.Render("binary")
	}

	// Path takes whatever's left after status (1) + gap (2) + counts.
	// Type-aware path coloring: tests pop in magenta, configs in cyan,
	// docs are dim, code is default.
	pathMax := max(10, width-1-2-lipgloss.Width(counts)-1)
	pathStr := truncate(f.Path, pathMax)
	pathRendered := styleForFilePath(f.Path).Render(pathStr)
	padding := max(1, width-1-2-lipgloss.Width(pathRendered)-lipgloss.Width(counts))
	return fmt.Sprintf("%s  %s%s%s", statusGlyph, pathRendered, strings.Repeat(" ", padding), counts)
}

// shortenDate trims a `%ai` ISO date (e.g. "2026-05-12 14:32:18 -0700") down
// to its date portion. Leaves anything it doesn't recognise untouched.
func shortenDate(s string) string {
	if i := strings.Index(s, " "); i > 0 {
		return s[:i]
	}
	return s
}

// wrapText word-wraps a string to lines no wider than `width`, preserving
// paragraph breaks (double newlines become an empty line).
func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var out []string
	for paragraph := range strings.SplitSeq(s, "\n") {
		if paragraph == "" {
			out = append(out, "")
			continue
		}
		var line string
		for word := range strings.FieldsSeq(paragraph) {
			if line == "" {
				line = word
				continue
			}
			if lipgloss.Width(line)+1+lipgloss.Width(word) > width {
				out = append(out, line)
				line = word
				continue
			}
			line += " " + word
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// renderCommitRow renders one commit line. typeWidth is the padded width of
// the "type:" column; subjectWidth is the budget for the subject after the
// type column. When focus is on commits and this row is selected, the row
// gets a background tint.
func (m dashModel) renderCommitRow(c DashCommit, idx, typeWidth, subjectWidth, ageWidth int) string {
	cursorMarker := "  "
	if m.focus == focusCommits && idx == m.commitCursor {
		cursorMarker = dStyleCursor.Render("▸ ")
	}

	typeCell, rest := formatCommitType(c.Subject, typeWidth)
	subject := truncate(rest, subjectWidth)
	subjectPadded := subject + strings.Repeat(" ", max(0, subjectWidth-lipgloss.Width(subject)))
	age := dStyleDim.Render(fmt.Sprintf("%*s", ageWidth, c.Age))

	row := fmt.Sprintf("    %s%s %s  %s", cursorMarker, typeCell, subjectPadded, age)
	if m.focus == focusCommits && idx == m.commitCursor {
		row = dStyleCursorRow.Render(row)
	}
	return row
}

// commitTypeWidth returns the width needed to align the type column across a
// set of commits. Includes the trailing colon and one space of padding.
func commitTypeWidth(commits []DashCommit) int {
	w := 6 // sensible minimum (covers "feat:", "fix:", etc.)
	for _, c := range commits {
		m := conventionalRe.FindStringSubmatch(c.Subject)
		if m == nil {
			continue
		}
		// m[1] is the type word; +1 for the colon.
		if l := len(m[1]) + 1; l > w {
			w = l
		}
	}
	return w
}

func commitAgeWidth(commits []DashCommit) int {
	w := 2
	for _, c := range commits {
		if l := lipgloss.Width(c.Age); l > w {
			w = l
		}
	}
	return w
}

// conventionalRe matches a Conventional Commits prefix at the start of a
// commit subject — e.g. "feat:", "fix(scope):", "refactor!:".
var conventionalRe = regexp.MustCompile(`^(\w+)(\([^)]+\))?(!)?:\s*`)

// formatCommitType extracts and colors the conventional-commit type word.
// Returns the padded, colored type cell and the remainder of the subject.
// Subjects without a conventional prefix get an empty padded cell, so column
// alignment is preserved across the commit list.
func formatCommitType(subject string, width int) (cell, rest string) {
	m := conventionalRe.FindStringSubmatch(subject)
	if m == nil {
		return strings.Repeat(" ", width), subject
	}
	typeWord := m[1]
	rest = subject[len(m[0]):]
	style := commitTypeStyle(typeWord)
	label := typeWord + ":"
	cell = style.Render(label) + strings.Repeat(" ", max(0, width-len(label)))
	return cell, rest
}

func commitTypeStyle(t string) lipgloss.Style {
	switch t {
	case "feat":
		return dStyleGreen
	case "fix":
		return dStyleYellow
	case "perf":
		return dStyleCyan
	case "refactor":
		return dStyleBlue
	case "test":
		return dStyleMagenta
	case "revert":
		return dStyleRed
	default:
		return dStyleDim
	}
}

// buildStatsLine renders the CHANGES line content. behind comes from the row
// data (it.Behind) so we don't issue an extra rev-list call. behind=0 omits
// the "N behind" clause entirely; ahead=0 omits the "N ahead" clause; both
// being zero means the caller wouldn't have invoked this. The behind number
// is yellow (same tint as the row's ⇣N glyph) so it pops as a rebase signal.
func buildStatsLine(s DashDiffStats, behind int) string {
	unit := func(n int, w string) string {
		if n == 1 {
			return w
		}
		return w + "s"
	}
	num := func(v int) string { return dStyleBold.Render(fmt.Sprintf("%d", v)) }
	yellowNum := func(v int) string { return dStyleYellow.Render(fmt.Sprintf("%d", v)) }

	var parts []string

	switch {
	case s.Commits > 0 && behind > 0:
		// Drop the word "commits" to keep the line compact when both sides are
		// present — the units are obvious in context.
		parts = append(parts,
			num(s.Commits)+" "+dStyleDim.Render("ahead")+", "+
				yellowNum(behind)+" "+dStyleDim.Render("behind"),
		)
	case s.Commits > 0:
		parts = append(parts,
			num(s.Commits)+" "+dStyleDim.Render(unit(s.Commits, "commit")+" ahead"),
		)
	case behind > 0:
		parts = append(parts,
			yellowNum(behind)+" "+dStyleDim.Render(unit(behind, "commit")+" behind base"),
		)
	}

	if s.Files > 0 {
		parts = append(parts,
			dStyleGreen.Render(fmt.Sprintf("+%d", s.Additions))+" "+
				dStyleRed.Render(fmt.Sprintf("−%d", s.Deletions))+" "+
				dStyleDim.Render("across")+" "+num(s.Files)+" "+dStyleDim.Render(unit(s.Files, "file")),
		)
	}

	return strings.Join(parts, dStyleDim.Render(", "))
}

func buildFilesLine(files []DashFileChange, maxWidth int) string {
	var parts []string
	used := 0
	for i, f := range files {
		var glyph string
		switch f.Status {
		case "A":
			glyph = dStyleGreen.Render("A")
		case "D":
			glyph = dStyleRed.Render("D")
		case "R":
			glyph = dStyleBlue.Render("R")
		default:
			glyph = dStyleYellow.Render(f.Status)
		}
		piece := glyph + " " + f.Path
		// approx visible width = len(status) + 1 + len(path)
		w := 1 + 1 + len(f.Path) + 2 // +2 for the separator
		if used+w > maxWidth && i > 0 {
			parts = append(parts, dStyleDim.Render(fmt.Sprintf("+%d more", len(files)-i)))
			break
		}
		parts = append(parts, piece)
		used += w
	}
	return strings.Join(parts, "  ")
}

func buildChecksLine(checks []DashCheck, maxWidth int) string {
	var parts []string
	used := 0
	for i, c := range checks {
		var glyph string
		switch c.State {
		case "pass":
			glyph = dStyleGreen.Render("✓")
		case "fail":
			glyph = dStyleRed.Render("✗")
		default:
			glyph = dStyleYellow.Render("◐")
		}
		piece := glyph + " " + c.Name
		w := 1 + 1 + len(c.Name) + 3
		if used+w > maxWidth && i > 0 {
			parts = append(parts, dStyleDim.Render(fmt.Sprintf("+%d more", len(checks)-i)))
			break
		}
		parts = append(parts, piece)
		used += w
	}
	return strings.Join(parts, " · ")
}

// renderRebaseCell renders the ephemeral rebase state that replaces the
// [pr][review][ci] columns while a rebase is in flight or recently completed.
func renderRebaseCell(r *dashRebaseState, width int) string {
	var s string
	switch r.phase {
	case "running":
		s = dStyleYellow.Render("rebasing…")
	case "done":
		if r.commits == 0 {
			s = dStyleGreen.Render("✓ up to date")
		} else {
			s = dStyleGreen.Render(fmt.Sprintf("✓ rebased (%d)", r.commits))
		}
	case "failed":
		msg := r.errMsg
		if msg == "" {
			msg = "failed"
		}
		s = dStyleRed.Render("✗ " + truncate(msg, width-2))
	}
	return padAnsi(s, width)
}

// renderCloseCell renders the ephemeral close state that replaces the
// [pr][review][ci] columns while a close is in flight or recently completed.
func renderCloseCell(p *dashCloseState, width int) string {
	var s string
	switch p.phase {
	case "closing":
		s = dStyleYellow.Render("closing…")
	case "done":
		s = dStyleGreen.Render("✓ closed")
	case "failed":
		msg := p.errMsg
		if msg == "" {
			msg = "failed"
		}
		s = dStyleRed.Render("✗ " + truncate(msg, width-2))
	}
	return padAnsi(s, width)
}

func buildSync(it DashItem) string {
	if it.IsBase {
		return dStyleDim.Render("—")
	}
	if it.Behind == 0 && it.Ahead == 0 {
		return dStyleGreen.Render("✓")
	}
	var parts []string
	if it.Behind > 0 {
		parts = append(parts, dStyleYellow.Render(fmt.Sprintf("⇣%d", it.Behind)))
	}
	if it.Ahead > 0 {
		parts = append(parts, dStyleGreen.Render(fmt.Sprintf("⇡%d", it.Ahead)))
	}
	return strings.Join(parts, "")
}

// buildStatusCell collapses PR + review + CI into one short cell for the
// narrow layout. The single most important signal wins: failing CI/changes
// requested beat pending which beats approved which beats "open". A worktree
// with no PR renders the same em-dash placeholder as the wide layout's PR
// column so the table stays consistent.
func buildStatusCell(it DashItem) string {
	if it.IsBase || it.PR == nil {
		return dStyleDim.Render("—")
	}
	pr := dStyleBlue.Render(fmt.Sprintf("#%d", it.PR.Number))
	switch it.PR.State {
	case "merged":
		return pr + " " + dStyleGreen.Render("merged")
	case "closed":
		return pr + " " + dStyleRed.Render("closed")
	}

	switch {
	case it.PR.CI.Fail > 0:
		return pr + " " + dStyleRed.Render("✗ ci")
	case it.PR.Review.Changes > 0:
		return pr + " " + dStyleRed.Render("✗ rev")
	case it.PR.CI.Pending > 0:
		return pr + " " + dStyleYellow.Render("◐ ci")
	case it.PR.Review.Approved > 0:
		return pr + " " + dStyleGreen.Render("✓")
	case it.PR.Review.Pending > 0:
		return pr + " " + dStyleBlue.Render("◐ rev")
	}
	return pr
}

func buildPRCells(it DashItem) (pr, review, ci string) {
	if it.IsBase {
		return dStyleDim.Render("—"), "", ""
	}
	if it.PR == nil {
		return dStyleDim.Render("—"), "", ""
	}
	pr = dStyleBlue.Render(fmt.Sprintf("#%d", it.PR.Number))
	switch it.PR.State {
	case "merged":
		review = dStyleGreen.Render("merged")
		ci = dStyleYellow.Render("stale")
		return
	case "closed":
		review = dStyleRed.Render("closed")
		ci = dStyleYellow.Render("stale")
		return
	}

	review = buildReviewGlyphs(it.PR.Review)
	ci = buildCIGlyphs(it.PR.CI, it.Unpushed)
	return
}

func buildReviewGlyphs(r DashReview) string {
	var parts []string
	if r.Approved > 0 {
		parts = append(parts, dStyleGreen.Render(strings.Repeat("✓", r.Approved)))
	}
	if r.Changes > 0 {
		parts = append(parts, dStyleRed.Render(strings.Repeat("✗", r.Changes)))
	}
	if r.Pending > 0 {
		parts = append(parts, dStyleBlue.Render(strings.Repeat("◐", r.Pending)))
	}
	if len(parts) == 0 {
		return dStyleDim.Render("○")
	}
	return strings.Join(parts, "")
}

func buildCIGlyphs(c DashCI, unpushed int) string {
	if c.Total == 0 {
		return dStyleDim.Render("○")
	}
	var parts []string
	if c.Pass > 0 {
		parts = append(parts, dStyleGreen.Render(strings.Repeat("✓", c.Pass)))
	}
	if c.Fail > 0 {
		parts = append(parts, dStyleRed.Render(strings.Repeat("✗", c.Fail)))
	}
	if c.Pending > 0 {
		parts = append(parts, dStyleYellow.Render(strings.Repeat("◐", c.Pending)))
	}
	s := strings.Join(parts, "")
	if unpushed > 0 {
		s += " " + dStyleMagenta.Render(fmt.Sprintf("⬆%d", unpushed))
	}
	return s
}

// padAnsi pads an ANSI-containing string to a visible width.
func padAnsi(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}
