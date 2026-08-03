// Package listview is bv's default pane: a flat, virtualised issue list.
//
// Virtualisation comes from bubbles/v2/list, which already windows its
// rendering to the visible rows — a flat list has no need for a hand-rolled
// viewport the way the tree view's variable-height rows do.
package listview

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// Model is the flat list view. It satisfies tui.View.
type Model struct {
	theme theme.Theme
	list  list.Model
	// snap lets SetTheme rebuild the delegate without a snapshot argument.
	snap *beads.Snapshot
}

// New builds an empty list view sized to zero; SetSize and SetSnapshot fill
// it in once the root model knows the terminal's geometry and the workspace.
func New(th theme.Theme) *Model {
	l := list.New(nil, newDelegate(th, nil), 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	// SetShowPagination(false): list.paginationView wraps its dots in
	// PaginationStyle (PaddingLeft(2)) only after comparing the unpadded width
	// against m.width, so the check always underestimates by two cells and
	// lipgloss.JoinVertical then pads every row up to that oversized footer —
	// every row in the pane, not just the footer, ends up too wide. Task 7.1's
	// status bar owns counts, so there is nothing this footer needs to show.
	l.SetShowPagination(false)
	// SetFilteringEnabled(false): bubbles/v2/list defaults filteringEnabled to
	// true, which keeps bubbles/v2/textinput's clipboard support (and its
	// shell-out to pbcopy/xclip via github.com/atotto/clipboard) reachable —
	// bv must never spawn a subprocess. keys.go binds "/" globally first, so
	// this path is unreachable today, but leaving it enabled is a latent
	// conflict with that invariant, not a proven-safe one.
	l.SetFilteringEnabled(false)
	// DisableQuitKeybindings: bubbles/v2/list.DefaultKeyMap binds both "q" and
	// "esc" to Quit, which list.Model.Update returns as tea.Quit directly.
	// keys.go's handleGlobalKey already intercepts "q" before any keypress
	// reaches a view, but nothing intercepts "esc" — so without this, pressing
	// Escape in bv's default view quit the whole program. Disabling it here,
	// once, is permanent: DisableQuitKeybindings also flips m.KeyMap.Quit off
	// immediately, and every later updateKeybindings() call (SetSize,
	// SetItems, ...) re-derives it from the same disabled flag rather than
	// re-enabling it. See listview_test.go's TestEscapeDoesNotQuitTheListView.
	l.DisableQuitKeybindings()
	// PrevPage/NextPage additionally bind pgup/b/u (prev) and pgdown/f/d
	// (next), plus left/h and right/l. pgup/pgdown are already routed to the
	// detail pane by keys.go's ScrollUp/ScrollDown, so those two spellings
	// were merely redundant; the rest silently paged this view on keys the
	// README and help overlay document only for the tree and board views.
	// Unbinding removes the surface rather than growing the README to match
	// it, consistent with SetFilteringEnabled(false) and
	// DisableQuitKeybindings above turning off list's inherited chrome
	// instead of documenting all of it.
	l.KeyMap.PrevPage.Unbind()
	l.KeyMap.NextPage.Unbind()

	return &Model{theme: th, list: l}
}

// SetSize resizes the underlying list.
func (m *Model) SetSize(width, height int) {
	m.list.SetSize(max(width, 0), max(height, 0))
}

// SetTheme restyles the pane; the delegate is rebuilt since list.Model
// caches it by value at SetDelegate time.
func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
	m.list.SetDelegate(newDelegate(th, m.snap))
}

// SetSnapshot replaces the issues this pane shows, preserving the selected
// id across the swap when it still exists in the new set.
func (m *Model) SetSnapshot(snap *beads.Snapshot) {
	selected := m.SelectedID()

	m.snap = snap
	m.list.SetDelegate(newDelegate(m.theme, snap))

	var issues []*beads.Issue
	if snap != nil {
		issues = snap.Issues()
	}
	items := make([]list.Item, len(issues))
	for i, issue := range issues {
		items[i] = item{issue: issue}
	}
	m.list.SetItems(items)

	if selected == "" || !m.SelectByID(selected) {
		m.list.Select(0)
	}
}

// Update routes a message to the underlying list. list.Model.Update takes a
// value receiver, so the result is reassigned rather than mutated in place.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)

	return cmd
}

// View renders the list.
//
// It no longer carries its own "no issues" placeholder: internal/tui's
// Model.body (empty.go) decides that at the app level now, from the same
// filtered snapshot every view was handed, and reaches for it before this
// pane's own View is ever called. Every issue in the filtered snapshot lands
// in m.list one for one (SetSnapshot), so an empty list here can only mean
// the app-level check already caught it.
func (m *Model) View() string {
	return m.list.View()
}

// Selected returns the issue under the cursor, or nil when the list is empty.
func (m *Model) Selected() *beads.Issue {
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		return nil
	}

	return it.issue
}

// SelectByID moves the cursor to id and reports success. A failed lookup
// leaves the cursor exactly where it was.
func (m *Model) SelectByID(id string) bool {
	for i, listItem := range m.list.Items() {
		it, ok := listItem.(item)
		if ok && it.issue.ID == id {
			m.list.Select(i)

			return true
		}
	}

	return false
}

// Reveal satisfies tui.View. A flat list hides nothing, so revealing an id is
// exactly selecting it; the two names differ only for the views where they
// diverge (see the View interface's own comment).
func (m *Model) Reveal(id string) bool { return m.SelectByID(id) }

// SelectedID returns the id of the selected issue, or "" when empty.
func (m *Model) SelectedID() string {
	if issue := m.Selected(); issue != nil {
		return issue.ID
	}

	return ""
}
