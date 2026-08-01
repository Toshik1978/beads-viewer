// Package tui implements bv's terminal application: the root model, the key
// map and the pane geometry. The previous tree's Model carried roughly 100
// fields and 90 methods; this package exists to keep that from happening
// again, by capping Model's own field count and holding the list, tree and
// board panes behind the View interface as peers rather than as named
// fields.
package tui

import (
	"fmt"
	"log/slog"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/boardview"
	"github.com/Toshik1978/beads-viewer/internal/tui/detail"
	"github.com/Toshik1978/beads-viewer/internal/tui/listview"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
)

// backgroundUnknownNote is the help footer for when no tea.BackgroundColorMsg
// ever arrived (normal over SSH/tmux). It must not claim bv assumed dark:
// theme.Resolve maps auto + BackgroundUnknown to SchemeAgnostic, not
// SchemeDark (theme.go) — guessing dark is what rendered near-white text on
// light terminals over SSH pre-rewrite — and both BV_THEME=light and =dark
// are named, since the agnostic palette assumes neither background.
const backgroundUnknownNote = "Terminal background not detected (normal over SSH/tmux) — using a " +
	"background-agnostic palette.\nSet BV_THEME=light or BV_THEME=dark for a tuned one."

// viewCount is the number of interchangeable panes Model switches between:
// list, tree and board.
const viewCount = 3

// boardSlot is the board view's index within Model.views.
const boardSlot = 2

// Deps carries everything NewModel needs to build the root model.
type Deps struct {
	Log      *slog.Logger
	Cfg      config.Config
	Theme    theme.Theme
	Snapshot *beads.Snapshot
	// Reload re-reads and decodes the workspace off the UI thread. The
	// watcher goroutine in cmd/bv asks for this by sending
	// ReloadRequestedMsg; Model answers by running Reload as a tea.Cmd.
	Reload func() tea.Msg
}

// SnapshotMsg is the result of a reload, successful or not.
type SnapshotMsg struct {
	Snapshot *beads.Snapshot
	Err      error
}

// ReloadRequestedMsg asks Model to run Deps.Reload as a command. The watcher
// goroutine sends this rather than decoding the file itself, so a slow decode
// never blocks the event loop.
type ReloadRequestedMsg struct{}

// Model is bv's root bubbletea model.
//
// It is capped at 12 fields by TestModelStaysSmall. That is not a stylistic
// preference: it is the mechanism that keeps the list, tree and board views
// from re-accumulating state here the way they did before the rewrite. New
// per-view state belongs inside a View implementation, not on this struct.
// detail took the cap's last slot; Tasks 7.1 and 7.2 have to grow
// statusState and overlayState instead of adding fields here.
type Model struct {
	log      *slog.Logger
	theme    theme.Theme
	keys     KeyMap
	views    [viewCount]View
	active   int
	snapshot *beads.Snapshot
	filter   beads.Filter
	layout   Layout
	status   statusState
	overlay  overlayState
	reload   func() tea.Msg
	detail   *detail.Model
}

// NewModel builds the root model from its dependencies and performs the
// initial filter pass, so every view already has issues to show before the
// first frame.
//
// It can fail: detail.New builds a glamour renderer from theme.GlamourStyle(),
// which returns "style not found" for anything glamour does not recognise.
// That style always comes from theme.New's own resolution today, so the
// error is unreachable in practice, but NewModel propagates it rather than
// swallowing it or building a pane with markdown silently disabled — a
// caller that mis-wires the theme should see why the program would not
// start, not a detail pane that quietly never renders prose.
func NewModel(deps Deps) (*Model, error) {
	snapshot := deps.Snapshot
	if snapshot == nil {
		snapshot = beads.NewSnapshot(nil)
	}

	detailPane, err := detail.New(deps.Log, deps.Theme)
	if err != nil {
		return nil, fmt.Errorf("build detail pane: %w", err)
	}

	m := &Model{
		log:   deps.Log,
		theme: deps.Theme,
		keys:  DefaultKeyMap(),
		views: [viewCount]View{
			listview.New(deps.Theme),
			treeview.New(deps.Theme),
			boardview.New(deps.Theme),
		},
		active:   activeIndex(deps.Cfg.View),
		snapshot: snapshot,
		filter:   beads.Filter{HideClosed: deps.Cfg.HideClosed},
		overlay:  overlayState{themePref: deps.Cfg.Theme},
		reload:   deps.Reload,
		detail:   detailPane,
	}
	m.applyFilter()

	return m, nil
}

// Init asks the terminal for its background colour (OSC 11); Update's
// tea.BackgroundColorMsg case handles the answer, if one ever comes.
func (m *Model) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

// Update routes a message in a fixed order: window size and snapshot updates
// always apply first and reach every view; an open overlay or an in-progress
// filter edit then swallows the key ahead of the active view; only then do
// the global bindings and, finally, the active view get a look at it.
//
// This returns the concrete *Model rather than the tea.Model interface.
// bubbletea v2.0.8's tea.Model.View returns tea.View (a struct), not the
// string every test and the rest of this package deal in, so *Model does not
// itself satisfy tea.Model — and ireturn forbids returning an interface here
// regardless. cmd/bv's composition root (Task 3.5) wraps *Model in a thin
// adapter whose View() calls tea.NewView(m.View()) to satisfy tea.Program.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.applyLayout(msg.Width, msg.Height)

		return m, nil
	case SnapshotMsg:
		m.applySnapshot(msg)

		return m, nil
	case ReloadRequestedMsg:
		return m, m.reload
	case clearStatusMsg:
		if msg.token == m.status.token {
			m.setStatus("", false)
		}

		return m, nil
	case tea.BackgroundColorMsg:
		detected := theme.BackgroundLight
		if msg.IsDark() {
			detected = theme.BackgroundDark
		}
		m.applyBackground(detected)

		return m, nil
	case tea.KeyPressMsg:
		cmd := m.handleKey(msg)
		m.syncDetail()

		return m, cmd
	default:
		cmd := m.views[m.active].Update(msg)

		return m, cmd
	}
}

// View renders the active pane and, when it has room, the detail pane beside
// or below it, plus the open overlay (if any) and the status bar. Help
// replaces the body outright, unlike the single-line filter overlay: its
// content does not fit the one-row budget the chrome below the body shares.
func (m *Model) View() string {
	body := m.body()
	switch m.overlay.kind {
	case overlayHelp:
		body = m.helpOverlay()
	case overlayNone, overlayFilter:
		if line := m.overlayLine(); line != "" {
			body += "\n" + line
		}
	}

	return body + "\n" + m.statusLine()
}

// SelectedID returns the id of the active view's selected issue, or "" when
// nothing is selected — selection is tracked by id, not row index.
func (m *Model) SelectedID() string {
	if issue := m.views[m.active].Selected(); issue != nil {
		return issue.ID
	}

	return ""
}

// SetFilter replaces the active filter and re-applies it to every view. It is
// the only place a filter is applied — a view must never re-filter for
// itself, or a query could mean something different from one pane to the
// next.
func (m *Model) SetFilter(f beads.Filter) {
	m.filter = f
	m.applyFilter()
}

// LoadTreeState restores the tree view's persisted expansion, selection and
// hide-closed state for beadsDir, if any was ever saved. It is meant to run
// once, after the initial snapshot is already in place, so the ids named in
// the saved state resolve against a built tree.
//
// ApplyState only runs when found is true — a first run with nothing ever
// saved must leave the tree exactly as SetSnapshot's own Build defaults left
// it (the root open, everything below it closed), not collapsed. treeview.
// LoadState folds a missing file into a zero State the same way it folds a
// corrupt one, and a zero State's Expanded is empty; calling ApplyState on it
// unconditionally used to override Build's default with "collapse every
// node," which is worse than never having persisted anything in the first
// place — confirmed by building a variant with only this call removed and
// comparing the two: the tree opened expanded without it, and collapsed to a
// single row with it.
//
// A load failure never reaches here either — treeview.LoadState already
// folds every read or parse error into found == false — so there is nothing
// for this method itself to treat as non-fatal; it exists only to keep the
// *treeview.Model type assertion, and the knowledge that slot 1 is the tree
// view, out of cmd/bv's composition root.
//
// syncDetail runs afterward unconditionally, not only when a saved state was
// actually found: applyFilter already synced the detail pane once, against
// whatever row SetSnapshot's own default happened to pick, before this
// method ever saw the persisted selection. Skipping the resync after a real
// restore leaves the detail pane one issue behind the tree's own highlighted
// row until the next keypress — confirmed live, with a tmux capture, before
// this call was added: the tree's cursor sat correctly on the restored row
// while the pane beside it still described the row SetSnapshot had defaulted
// to.
func (m *Model) LoadTreeState(beadsDir string) {
	tree, ok := m.views[1].(*treeview.Model)
	if !ok {
		return
	}

	// The error is deliberately dropped: LoadState already turns every read
	// or parse failure into a zero, not-found State, so err is nil in
	// practice. Keeping the check rather than discarding it with `_, _, _ :=`
	// documents that this call is still expected to be infallible, not that
	// its result is unimportant.
	if state, found, err := treeview.LoadState(beadsDir); err == nil && found {
		tree.ApplyState(state)
	}
	m.syncDetail()
}

// SaveTreeState persists the tree view's current expansion, selection and
// hide-closed state for beadsDir. Failures never stop the exit — that would
// be worse than forgetting it next time — but are logged (I7).
func (m *Model) SaveTreeState(beadsDir string) {
	if tree, ok := m.views[1].(*treeview.Model); ok {
		if err := tree.ExportState().Save(beadsDir); err != nil && m.log != nil {
			m.log.Warn("save tree state", slog.String("dir", beadsDir), slog.Any("error", err))
		}
	}
}

// body renders the active pane, or a centred explanation in its place when
// there is nothing to show. An empty workspace, a filter that matched
// nothing and a reload that failed before anything ever loaded are three
// different problems — see emptyReason's doc comment — and conflating them
// into one "No issues" sends the reader looking for a bug in br instead of
// at their own filter. This is the single place that decision is made; the
// three views no longer carry their own copy of it.
func (m *Model) body() string {
	switch {
	case m.snapshot.Len() == 0 && m.status.IsError:
		return renderEmpty(emptyLoadError, m.filter, m.status.Message, m.theme, m.layout.Width, m.layout.BodyHeight)
	case m.snapshot.Len() == 0:
		return renderEmpty(emptyWorkspace, m.filter, "", m.theme, m.layout.Width, m.layout.BodyHeight)
	case !m.filter.Any(m.snapshot):
		// Any, not Apply(...).Len() == 0: Apply rebuilds and re-indexes a
		// whole second snapshot just to answer a yes/no question this method
		// asks on every single frame — benchmarked on a 758-record fixture at
		// 149us/818KB/1,655 allocs per call, 57% of this method's per-frame
		// allocated bytes. Any short-circuits on the first match instead.
		return renderEmpty(emptyFilter, m.filter, "", m.theme, m.layout.Width, m.layout.BodyHeight)
	default:
		return m.joinPanes()
	}
}

// joinPanes lays the active view and the detail pane out side by side — each
// framed, or separated by a blank gutter when the terminal cannot afford a
// frame (separator below) — one above the other when layout.Stacked, or
// renders the active view alone when there is no room for a detail pane
// (DetailWidth of 0, or a nil detail in a test that builds Model directly).
func (m *Model) joinPanes() string {
	listHeight, detailHeight := m.layout.paneHeights()
	listPane := m.frame(m.views[m.active].View(), m.layout.ListWidth, listHeight, m.overlay.focus == focusView)
	if m.detail == nil || m.layout.DetailWidth <= 0 {
		return listPane
	}

	detailPane := m.frame(m.detail.View(), m.layout.DetailWidth, detailHeight, m.overlay.focus == focusDetail)
	if m.layout.Stacked {
		return lipgloss.JoinVertical(lipgloss.Left, listPane, detailPane)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, listPane, m.separator(), detailPane)
}

// applySnapshot handles the result of a reload. An error leaves the previous
// snapshot on screen and surfaces on the status line — unadorned, since I3
// moved the fix for a decode failure burying its own reason behind a
// workspace path to the source (jsonl.go). Runtime errors must never tear
// down the UI. A success re-applies the current filter and hands the result
// to every view, which is what lets each view restore its selection by id
// even though a new issue may have shifted every row.
func (m *Model) applySnapshot(msg SnapshotMsg) {
	if msg.Err != nil {
		m.setStatus(msg.Err.Error(), true)

		return
	}

	m.snapshot = msg.Snapshot
	m.setStatus("", false)
	m.applyFilter()
}

// applyBackground records the terminal's reported background and, if the
// resolved scheme changes, rebuilds the theme, restyles every view
// (View.SetTheme) and rebuilds the detail pane's glamour renderer — glamour
// v2 bakes its stylesheet name in at construction, so a stale detail pane
// would keep the old scheme after every other pane had moved on.
//
// An explicit --theme/BV_THEME preference always wins in theme.Resolve, so
// this is a scheme no-op whenever one was set; it still records background,
// which lets the help footer state that detection never happened even
// though it made no visible difference this time.
func (m *Model) applyBackground(detected theme.DetectedBackground) {
	m.overlay.background = detected

	next := theme.New(m.overlay.themePref, detected)
	if next.Scheme == m.theme.Scheme {
		return
	}

	// The same unreachable-in-practice case NewModel documents: detail.New
	// only fails on a glamour style theme.New already produced, so err just
	// keeps the old theme rather than tearing down the UI — logged (I7).
	detailPane, err := detail.New(m.log, next)
	if err != nil {
		if m.log != nil {
			m.log.Warn("rebuild detail pane for theme change", slog.Any("error", err))
		}
		return
	}

	m.theme = next
	for _, v := range m.views {
		v.SetTheme(next)
	}
	m.detail = detailPane
	_, detailHeight := m.layout.paneHeights()
	m.detail.SetSize(m.layout.DetailWidth, detailHeight)
	m.syncDetail()
}

// applyFilter is filtering's single call site: it narrows the current
// snapshot once and hands the identical result to every view.
func (m *Model) applyFilter() {
	filtered := m.filter.Apply(m.snapshot)
	for _, v := range m.views {
		v.SetSnapshot(filtered)
	}
	m.syncDetail()
}

// syncDetail keeps the detail pane showing the active view's current
// selection. It runs after anything that might have moved the cursor —
// filtering, a reload, a keypress — cheaply: detail.Model.SetIssue is a
// no-op unless the selected id or the snapshot has actually changed, so
// calling this after every keypress does not re-render markdown for the
// keys (help, an unmatched key) that never touch selection. The full
// (unfiltered) snapshot is passed, not the filtered one views render from,
// so a blocker hidden by the active filter still resolves to a name and
// status here rather than reading as a dangling reference.
func (m *Model) syncDetail() {
	if m.detail == nil {
		return
	}
	m.detail.SetIssue(m.views[m.active].Selected(), m.snapshot)
}

// statusLine renders the status bar for the current frame. m.status only
// ever carries Message, IsError and token persistently (set by setStatus);
// the rest of statusState is recomputed here from live state on every
// render, which is what lets renderStatus read beads.Counts and the filter
// directly instead of this method keeping a parallel copy of them.
func (m *Model) statusLine() string {
	st := m.status
	st.Counts = m.snapshot.Counts()
	st.Filter = m.filter
	st.View = viewKindAt(m.active)
	st.Lane = m.boardLaneName()
	st.Hidden = m.treeHiddenCount()

	return renderStatus(st, m.theme, m.layout.Width)
}

// boardLaneName returns the active board's swimlane name for the status
// bar's indicator (Tab cycled the grouping with nothing on screen saying
// why before this), or "" when the board is not the active view.
func (m *Model) boardLaneName() string {
	board, ok := m.views[boardSlot].(*boardview.Model)
	if !ok || m.active != boardSlot {
		return ""
	}

	return board.Lane().Name()
}

// helpOverlay assembles the overlay Model actually shows: renderHelpBody's
// grouped bindings, plus — only when the background was never detected —
// the note that makes BV_THEME discoverable, passed in so it is centred
// together with the rest rather than appended after.
//
// backgroundUnknownNote's first line alone is 94 cells: unwrapped, it
// overflowed every terminal narrower than that, including 80x24 (C2).
func (m *Model) helpOverlay() string {
	note := ""
	if m.overlay.background == theme.BackgroundUnknown {
		note = m.theme.Muted.Render(lipgloss.Wrap(backgroundUnknownNote, m.layout.Width, ""))
	}

	return renderHelpBody(m.keys, m.theme, m.layout.Width, m.layout.BodyHeight, note)
}

// viewKinds pairs each Model.views slot with the config.ViewKind it
// represents. activeIndex and viewKindAt read it in opposite directions
// instead of each maintaining its own switch, which is what kept the two
// from drifting out of sync when board's slot last moved.
func viewKinds() [viewCount]config.ViewKind {
	return [viewCount]config.ViewKind{config.ViewList, config.ViewTree, config.ViewBoard}
}

// activeIndex maps the configured starting view to its slot in Model.views.
// An unrecognised kind — config.Load validates it, but activeIndex must still
// be total — falls back to the list, the same as ViewList itself.
func activeIndex(kind config.ViewKind) int {
	for i, k := range viewKinds() {
		if k == kind {
			return i
		}
	}

	return 0
}

// viewKindAt is activeIndex's inverse, for the status bar.
func viewKindAt(i int) config.ViewKind {
	if kinds := viewKinds(); i >= 0 && i < len(kinds) {
		return kinds[i]
	}

	return config.ViewList
}
