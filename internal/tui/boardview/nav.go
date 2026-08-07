package boardview

// This file turns the static column set group.go builds into a navigable,
// windowed one: the two-dimensional cursor, the rules that keep it on a cell
// that exists, and the column- and row-window arithmetic that decides which
// part of the board a pane-sized frame shows. boardview.go stays construction
// and rendering; everything here is what a keypress does to that grouping's
// cursor and window.

import (
	"maps"
	"slices"
)

// SelectByID moves the cursor to id and reports success. A failed lookup
// leaves the cursor exactly where it was.
func (m *Model) SelectByID(id string) bool {
	col, row, ok := m.find(id)
	if !ok {
		return false
	}

	m.col, m.row = col, row
	m.clamp()
	m.syncSelected()

	return true
}

// Reveal satisfies tui.View. Every card is on the board somewhere, and
// followCursor scrolls the column window to the cursor at render time, so
// revealing is exactly selecting here too.
func (m *Model) Reveal(id string) bool { return m.SelectByID(id) }

// MoveUp moves the cursor to the previous row in its column, stopping at the
// top.
func (m *Model) MoveUp() {
	m.row--
	m.clamp()
	m.syncSelected()
}

// MoveDown moves the cursor to the next row in its column, stopping at the
// bottom.
func (m *Model) MoveDown() {
	m.row++
	m.clamp()
	m.syncSelected()
}

// MoveLeft moves the cursor to the nearest column to the left that holds at
// least one issue, and stays where it is when there is none. Empty columns
// are skipped rather than hidden: they still render with their header, count
// and stats, so nothing about the board is concealed — the cursor simply does
// not park where there is nothing to act on.
func (m *Model) MoveLeft() {
	m.col = m.nextPopulated(m.col, -1)
	m.clamp()
	m.syncSelected()
}

// MoveRight is MoveLeft's mirror; see it for why empty columns are skipped.
func (m *Model) MoveRight() {
	m.col = m.nextPopulated(m.col, 1)
	m.clamp()
	m.syncSelected()
}

// JumpToTop moves the cursor to the first row of its current column.
func (m *Model) JumpToTop() {
	m.row = 0
	m.clamp()
	m.syncSelected()
}

// JumpToBottom moves the cursor to the last row of its current column.
func (m *Model) JumpToBottom() {
	if len(m.columns) > 0 && m.col >= 0 && m.col < len(m.columns) {
		m.row = len(m.columns[m.col].Issues)
	}
	m.clamp()
	m.syncSelected()
}

// HelpKeys returns every key keyActions binds, checked against helpGroups by internal/tui/help_test.go.
func HelpKeys() []string {
	return slices.Collect(maps.Keys(keyActions(&Model{})))
}

// followCursor slides the render window so the cursor's column stays
// visible (F1). The window only grows toward the cursor rather than
// re-centring on it, which is what makes scrolling right then back left
// retrace the same firstCol positions instead of jumping around: min/max
// first raises the old firstCol to at least what is needed to keep the
// cursor inside the window from the right, caps it at the cursor itself so
// the cursor is also inside it from the left, and the outer clamp confines
// the result to columns that actually exist.
func (m *Model) followCursor(visible int) {
	first := min(max(m.firstCol, m.col-visible+1), m.col)
	m.firstCol = min(max(first, 0), max(len(m.columns)-visible, 0))
}

// nextPopulated returns the index of the nearest column in direction step
// that holds at least one issue, or from when there is none — including when
// every column is empty, which is why the caller can treat the result as
// always valid.
//
// This is a movement rule only. clamp stays responsible for confining the
// cursor to a cell that exists, because a regrouping or a reload can empty
// the column the cursor is already on, and that case must still resolve to
// somewhere valid rather than to wherever movement last left it.
func (m *Model) nextPopulated(from, step int) int {
	for i := from + step; i >= 0 && i < len(m.columns); i += step {
		if len(m.columns[i].Issues) > 0 {
			return i
		}
	}

	return from
}

// clamp confines the cursor to a cell that exists.
//
// Called at the end of every movement and after every regrouping. An empty
// column keeps the column selection with row 0 and no selected issue, rather
// than being skipped — a regrouping or a reload can strand the cursor on a
// column that just lost its only issue, and clamp has to resolve that safely
// even though MoveLeft and MoveRight no longer land there on purpose (see
// nextPopulated).
func (m *Model) clamp() {
	if len(m.columns) == 0 {
		m.col, m.row = 0, 0

		return
	}

	m.col = min(max(m.col, 0), len(m.columns)-1)

	count := len(m.columns[m.col].Issues)
	if count == 0 {
		m.row = 0

		return
	}
	m.row = min(max(m.row, 0), count-1)
}

// syncSelected derives m.selected from the current, already-clamped cursor.
func (m *Model) syncSelected() {
	m.selected = ""
	if issue := m.Selected(); issue != nil {
		m.selected = issue.ID
	}
}

// find locates id among the current columns, reporting its (col, row) and
// whether it was found at all. An empty id always misses, so a fresh
// Model's zero-valued selected does not accidentally match an issue with an
// empty ID (hand-edited JSONL can produce one).
func (m *Model) find(id string) (col, row int, ok bool) {
	if id == "" {
		return 0, 0, false
	}

	for ci, c := range m.columns {
		for ri, issue := range c.Issues {
			if issue.ID == id {
				return ci, ri, true
			}
		}
	}

	return 0, 0, false
}

// columnWindow picks the slice of a column's issues to render so the cursor
// row stays visible, mirroring treeview's viewport-follows-cursor rule but
// scoped to a single column instead of the whole pane.
func columnWindow(cursorRow, maxFit, total int, focused bool) (start, end int) {
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

// keyActions maps a key's textual representation to the method it triggers.
// Built fresh per keypress rather than cached on Model, matching treeview's
// nav.go: gochecknoglobals rules out a package-level table.
func keyActions(m *Model) map[string]func() {
	return map[string]func(){
		"up": m.MoveUp, "k": m.MoveUp,
		"down": m.MoveDown, "j": m.MoveDown,
		"left": m.MoveLeft, "h": m.MoveLeft,
		"right": m.MoveRight, "l": m.MoveRight,
		"home": m.JumpToTop, "g": m.JumpToTop,
		"end": m.JumpToBottom, "G": m.JumpToBottom,
		"s":     m.CycleSwimLane,
		"space": m.ToggleExpand,
	}
}
