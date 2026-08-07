package depsview

// This file holds the model's own state: its fields, the two-dimensional
// cursor's bookkeeping, and Reveal, which changes what the model is about
// rather than where its cursor is. Rendering — geometry, column fitting and
// the card and heading lines — lives in render.go. deps.go stays pure column
// construction; nav.go is what a keypress does to the cursor and the focus.

import (
	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// Model is the dependency view: bv's fourth pane. It satisfies tui.View.
//
// focusID and the cursor are different things. focusID is which issue the
// columns are built around; col and row are which card is highlighted.
// nav.go's Descend promotes the highlighted card to the focus, which is what
// makes the view walkable — and history is what makes that walk reversible.
//
// There is no filter field: every Filter dimension is the app's, applied once
// upstream and arriving here as a narrower snapshot.
//
// A pin in this package's external test package (var _ tui.View =
// (*Model)(nil), in view_test.go) guards the tui.View claim above.
type Model struct {
	theme    theme.Theme
	width    int
	height   int
	snapshot *beads.Snapshot
	focusID  string
	columns  []Column
	col      int
	row      int
	selected string
	// history is the stack of focus ids Descend has walked away from, most
	// recent last, so Back can retrace the walk. Nothing but nav.go reads or
	// writes it — it stayed off Model entirely until there were keys that
	// needed it, since an unused field fails golangci-lint's unused check.
	history []string
}

// New builds an empty dependency view; SetSize, SetSnapshot and Reveal fill it
// in once the root model knows the terminal's geometry, the workspace and
// which issue the user was looking at.
func New(th theme.Theme) *Model {
	return &Model{theme: th}
}

// SetTheme restyles the pane; theme is read fresh from m on every render.
func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

// SetSize records the pane's allotted geometry.
func (m *Model) SetSize(width, height int) {
	m.width = max(width, 0)
	m.height = max(height, 0)
}

// SetSnapshot rebuilds the columns from snap, keeping the same focus. A
// reload must not change which issue the view is about — only what it says
// about it.
//
// A view that has never had a focus is the exception, since nothing else
// seeds one: Reveal only ever runs from a view switch (keys.go), so
// `--view deps`/`view: deps`/BV_VIEW=deps all reach this view through
// NewModel's own applyFilter call with focusID still "" and no switch having
// happened, which used to render four permanently empty columns. Adopting
// snap's first issue in canonical order — Issues()'s own order, the one
// listview.SetSnapshot opens on — is what makes starting on deps and
// starting on list then pressing 4 agree on where the cursor lands.
func (m *Model) SetSnapshot(snap *beads.Snapshot) {
	m.snapshot = snap
	if m.focusID == "" {
		if id, ok := firstIssueID(snap); ok {
			m.focusID = id
			m.col, m.row = focusColumn, 0
		}
	}
	m.rebuild()
}

// firstIssueID returns snap's first issue's id in canonical order, or "" and
// false when snap is nil or empty.
func firstIssueID(snap *beads.Snapshot) (string, bool) {
	if snap == nil {
		return "", false
	}
	issues := snap.Issues()
	if len(issues) == 0 {
		return "", false
	}

	return issues[0].ID, true
}

// Selected returns the issue under the cursor, or nil when the cursor's
// column is empty or the entry is a dangling id with no issue behind it.
func (m *Model) Selected() *beads.Issue {
	entry, ok := m.currentEntry()
	if !ok {
		return nil
	}

	return entry.Issue
}

// SelectedID returns the id under the cursor, or "" when the cursor's column
// is empty. Unlike Selected it is non-empty for a dangling blocker, so the
// pane can still say what the cursor is on — but that id is all there is:
// tui.yank goes through Selected, not SelectedID, so y on a dangling entry
// is silently a no-op rather than copying its id.
func (m *Model) SelectedID() string {
	return m.selected
}

// FocusID reports which issue the columns are currently built around.
func (m *Model) FocusID() string {
	return m.focusID
}

// Reveal re-roots the view on id and reports success. It satisfies tui.View,
// and it is why that method is not named SelectByID: the other views move a
// cursor among rows that already exist, whereas this one rebuilds every
// column around a new subject. It is exported here, with the rest of the
// tui.View surface, rather than beside nav.go's movement methods, for that
// same reason.
//
// A view switch (keys.go's carrySelection) is the only caller from outside
// this package, and it always hands Reveal an entry from *outside* this
// view's own walk — so Reveal clears history here, while nav.go's Descend
// and Back call the unexported reveal below and leave it alone. Without that
// split, re-rooting on an id from another view mid-walk would leave
// backspace retracing hops this visit never took: walk A to C, switch to the
// list, move to Z, switch back, and backspace would land on B.
func (m *Model) Reveal(id string) bool {
	if !m.reveal(id) {
		return false
	}
	m.history = nil

	return true
}

// reveal is Reveal's mechanics minus the history reset, shared with Descend
// and Back (nav.go) so a hop within this view's own walk never clears it.
func (m *Model) reveal(id string) bool {
	if id == "" || m.snapshot == nil || !m.exists(id) {
		return false
	}
	if id == m.focusID {
		return true
	}

	m.focusID = id
	m.selected = id
	m.col, m.row = focusColumn, 0
	m.rebuild()

	return true
}

// rebuild recomputes the columns for the current focus and reconciles the
// cursor against them.
func (m *Model) rebuild() {
	m.columns = Columns(m.snapshot, m.focusID)
	if idx := m.findSelected(); idx.ok {
		m.col, m.row = idx.col, idx.row
	}
	m.clamp()
	m.syncSelected()
}

// clamp confines the cursor to a cell that exists. An empty column keeps the
// column selection with row 0 and no selected entry, rather than being
// skipped: a reload or a re-root can strand the cursor on a column that just
// emptied.
func (m *Model) clamp() {
	if len(m.columns) == 0 {
		m.col, m.row = 0, 0

		return
	}

	m.col = min(max(m.col, 0), len(m.columns)-1)

	count := len(m.columns[m.col].Entries)
	if count == 0 {
		m.row = 0

		return
	}
	m.row = min(max(m.row, 0), count-1)
}

// syncSelected derives m.selected from the already-clamped cursor.
func (m *Model) syncSelected() {
	m.selected = ""
	if entry, ok := m.currentEntry(); ok {
		m.selected = entry.ID
	}
}

// currentEntry returns the entry under the cursor.
func (m *Model) currentEntry() (Entry, bool) {
	if m.col < 0 || m.col >= len(m.columns) {
		return Entry{}, false
	}
	entries := m.columns[m.col].Entries
	if m.row < 0 || m.row >= len(entries) {
		return Entry{}, false
	}

	return entries[m.row], true
}

// cursorAt is findSelected's result: a cell, and whether one was found.
type cursorAt struct {
	col, row int
	ok       bool
}

// findSelected locates m.selected among the current columns, so a rebuild
// keeps the cursor on the same card rather than at the same coordinates —
// the entry at (col, row) can be a different issue after a reload.
func (m *Model) findSelected() cursorAt {
	if m.selected == "" {
		return cursorAt{}
	}
	for ci, col := range m.columns {
		for ri, entry := range col.Entries {
			if entry.ID == m.selected {
				return cursorAt{col: ci, row: ri, ok: true}
			}
		}
	}

	return cursorAt{}
}

// exists reports whether id is an issue in the current snapshot. Reveal
// refuses an absent id rather than re-rooting on nothing: four empty columns
// headed by an issue that is not there says less than leaving the view where
// it was. ByID is used rather than a scan of Issues() because duplicate-id
// resolution is defined in input order, while Issues() is sorted.
func (m *Model) exists(id string) bool {
	if m.snapshot == nil {
		return false
	}
	_, ok := m.snapshot.ByID(id)

	return ok
}
