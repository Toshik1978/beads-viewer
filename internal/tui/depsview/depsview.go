package depsview

// This file turns deps.go's columns into a bubbletea view: geometry, column
// fitting and rendering, plus the two-dimensional cursor's own bookkeeping.
// deps.go stays pure column construction; a later task adds what a keypress
// does to the cursor and the focus.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/cardfmt"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

const (
	// columnGap is the blank column rendered between two card columns.
	columnGap = 1
	// columnCount is how many columns Columns always returns. Unlike the
	// board, whose column count is data-dependent and therefore scrollable,
	// this view's is fixed at four — so there is no horizontal scroll window
	// and no hidden-column marker here.
	columnCount = 4
	// headerLines is the rows each column spends on its title before any card
	// is drawn.
	headerLines = 1
	// focusColumn is the middle column's index: the one Reveal parks the
	// cursor on, since it is the only column guaranteed non-empty after a
	// successful re-root.
	focusColumn = 1
)

// Model is the dependency view: bv's fourth pane. It satisfies tui.View.
//
// focusID and the cursor are different things. focusID is which issue the
// columns are built around; col and row are which card is highlighted. A
// later task's enter promotes the highlighted card to the focus, which is
// what makes the view walkable — and a re-rooting history is what makes that
// walk reversible.
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
func (m *Model) SetSnapshot(snap *beads.Snapshot) {
	m.snapshot = snap
	m.rebuild()
}

// Update satisfies tui.View. It ignores every message for now; a later task
// replaces this body with a key-action dispatch once there are keys to
// dispatch.
func (m *Model) Update(tea.Msg) tea.Cmd {
	return nil
}

// View renders the four columns side by side.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 || len(m.columns) == 0 {
		return ""
	}

	colWidth := m.columnWidth()
	if colWidth <= 0 {
		return ""
	}

	blocks := make([]string, 0, columnCount*2)
	for i, col := range m.columns {
		blocks = append(blocks, m.renderColumn(col, colWidth, i == m.col))
		if i < len(m.columns)-1 {
			blocks = append(blocks, strings.Repeat(" ", columnGap))
		}
	}

	return m.clip(lipgloss.JoinHorizontal(lipgloss.Top, blocks...))
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
// is empty. Unlike Selected it is non-empty for a dangling blocker: an id is
// all there is to say about one, and yanking it is still useful.
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
// column around a new subject.
//
// This method is exported here, with the rest of the tui.View surface, rather
// than beside the movement methods a later task adds: it changes what the
// model is about, not where its cursor is.
func (m *Model) Reveal(id string) bool {
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

// columnWidth divides the pane between four columns and their gaps. All four
// always render: unlike the board there is no column here that could be
// dropped without losing a distinct question the view exists to answer.
func (m *Model) columnWidth() int {
	avail := m.width - columnGap*(columnCount-1)

	return max(avail/columnCount, 0)
}

// renderColumn renders one column's heading and as many entries as fit in
// m.height, appending a "+N more" line when some had to be dropped.
func (m *Model) renderColumn(col Column, width int, focused bool) string {
	lines := []string{m.headingLine(col, width, focused)}

	start, end := m.entryWindow(col.Entries, m.height-headerLines, focused)
	for i := start; i < end; i++ {
		lines = append(lines, m.renderEntry(col.Entries[i], width, focused && i == m.row))
	}
	if hidden := len(col.Entries) - (end - start); hidden > 0 {
		lines = append(lines, m.theme.Muted.Render(uitext.Truncate(fmt.Sprintf("+%d more", hidden), width)))
	}

	return strings.Join(lines, "\n")
}

// entryWindow picks the slice of a column's entries to render, honouring
// each entry's real row cost (entryRows) rather than assuming every entry
// costs the same. A "blocked by" or "related" column mixes single-line
// dangling ids, plain cards and labelled cards (one line taller), and a
// budget that assumed one fixed height per entry could believe a column fit
// when its actual, labelled cost ran past avail — clip's tail-chop still
// held the pane's outer geometry, but silently, by cutting into whichever
// column ran long instead of reporting a "+N more" the way every other
// overflow does.
//
// When every entry fits, the whole column renders and nothing is reserved.
// Otherwise a row is set aside for the "+N more" notice, and window's
// cursor-following step (sized by the smallest possible per-entry cost, so
// it never picks a start too tight to hold the cursor) decides where the
// visible run begins; fitCount then charges each entry's real cost from
// there.
func (m *Model) entryWindow(entries []Entry, avail int, focused bool) (start, end int) {
	if totalRows(entries) <= avail {
		return 0, len(entries)
	}

	budget := avail - 1
	if budget <= 0 {
		return 0, 0
	}

	maxFit := budget / cardfmt.Height(false)
	start, _ = window(m.row, maxFit, len(entries), focused)
	end = start + fitCount(entries[start:], budget)

	return start, end
}

// headingLine renders a column's title and entry count, padded to width so an
// empty column stays exactly as wide as a populated one — lipgloss.Join-
// Horizontal pads each block only to its own widest line, so an unpadded
// heading over no cards would leave that column visibly narrow.
func (m *Model) headingLine(col Column, width int, focused bool) string {
	style := m.theme.Title
	if focused {
		style = m.theme.Accent.Bold(true)
	}

	return style.Width(width).Render(
		uitext.Truncate(fmt.Sprintf("%s (%d)", col.Title, len(col.Entries)), width))
}

// renderEntry draws one card. A dangling blocker has no issue behind it, so
// it is rendered as a bare relation-tagged id rather than through cardfmt,
// which needs a *beads.Issue to read a title, status and priority from.
func (m *Model) renderEntry(e Entry, width int, selected bool) string {
	if e.Issue == nil {
		style := m.theme.Muted
		if selected {
			style = m.theme.Selected
		}

		return style.Width(width).Render(
			uitext.Truncate(uitext.Sanitize(e.ID)+" — "+string(e.Relation), width))
	}

	card := cardfmt.Render(m.theme, m.snapshot, e.Issue, width, selected, false)
	if !needsLabel(e) {
		return card
	}

	// Only the relations a reader could otherwise misread are labelled: a card
	// in the "blocked by" column is a blocker and a card in "blocks" blocks, so
	// tagging those would be noise. "via parent", "open child", "related" and
	// "discovered from" all say something the column heading does not.
	return card + "\n" + m.theme.Muted.Width(width).Render(uitext.Truncate(string(e.Relation), width))
}

// needsLabel reports whether e's relation is not already implied by the
// column heading it renders under, and therefore gets a label line of its
// own. renderEntry and entryRows both call this — never the three-way
// equality check directly — so the label a card actually gets and the row it
// is budgeted for cannot drift apart.
func needsLabel(e Entry) bool {
	return e.Relation != RelationFocus && e.Relation != RelationBlocker && e.Relation != RelationBlocks
}

// entryRows is the exact number of lines renderEntry produces for e: one for
// a dangling blocker (it has no card, only a bare id), cardfmt.Height(false)
// for a plain card, and one more again for a labelled card.
func entryRows(e Entry) int {
	if e.Issue == nil {
		return 1
	}
	if needsLabel(e) {
		return cardfmt.Height(false) + 1
	}

	return cardfmt.Height(false)
}

// clip holds the pane inside its allotted geometry. joinPanes (tui/app.go)
// assumes every pane already fits what it was given, so this is the last
// line of defence rather than a nicety.
func (m *Model) clip(pane string) string {
	lines := strings.Split(pane, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i, line := range lines {
		lines[i] = uitext.Truncate(line, m.width)
	}

	return strings.Join(lines, "\n")
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

// window picks a starting index that keeps the cursor visible within a run
// of roughly maxFit entries, mirroring boardview's columnWindow scoped to a
// single column. maxFit is sized from the smallest possible per-entry cost
// (see entryWindow), so it is a generous estimate rather than an exact
// count; fitCount is what turns start into a real, budget-accurate end.
func window(cursorRow, maxFit, total int, focused bool) (start, end int) {
	if maxFit <= 0 {
		return 0, 0
	}

	start = 0
	if focused && cursorRow >= maxFit {
		start = cursorRow - maxFit + 1
	}
	start = min(max(start, 0), max(total-maxFit, 0))
	end = min(start+maxFit, total)

	return start, end
}

// fitCount is how many of entries, in order, fit within budget rows of
// their real per-entry cost (entryRows).
func fitCount(entries []Entry, budget int) int {
	used, n := 0, 0
	for _, e := range entries {
		cost := entryRows(e)
		if used+cost > budget {
			break
		}
		used += cost
		n++
	}

	return n
}

// totalRows sums entryRows over entries, so entryWindow can tell in one
// comparison whether a column needs to reserve a "+N more" row at all.
func totalRows(entries []Entry) int {
	sum := 0
	for _, e := range entries {
		sum += entryRows(e)
	}

	return sum
}
