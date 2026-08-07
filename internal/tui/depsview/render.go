package depsview

// This file renders deps.go's columns: geometry, column fitting, the card and
// heading lines, and the vertical window that decides which entries fit. The
// cursor's own bookkeeping and everything that changes what the model is about
// stays in depsview.go, mirroring treeview's state.go / render.go split.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Toshik1978/beads-viewer/internal/tui/cardfmt"
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

// columnWidth divides the pane between four columns and their gaps. All four
// always render: unlike the board there is no column here that could be
// dropped without losing a distinct question the view exists to answer.
func (m *Model) columnWidth() int {
	avail := m.width - columnGap*(columnCount-1)

	return max(avail/columnCount, 0)
}

// renderColumn renders one column's heading and as many entries as fit in
// m.height, appending a "+N more" line counting only what is hidden below
// the window — not what has scrolled off the top above start, which is
// behind the cursor's own scroll rather than something left to tell about.
func (m *Model) renderColumn(col Column, width int, focused bool) string {
	lines := []string{m.headingLine(col, width, focused)}

	start, end := m.entryWindow(col.Entries, m.height-headerLines, focused)
	for i := start; i < end; i++ {
		lines = append(lines, m.renderEntry(col.Entries[i], width, focused && i == m.row))
	}
	if hidden := len(col.Entries) - end; hidden > 0 {
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
// Otherwise a row is set aside for the "+N more" notice: windowStart and
// fitCount both walk real per-entry costs, backwards from the cursor and
// forwards from the chosen start respectively, which is what makes "the
// cursor is inside [start, end)" true by construction rather than by an
// assumption that every entry costs the same (an earlier version of this
// function paired a cost-aware fitCount with a uniform-cost start and could
// place the cursor outside its own window — see bv-7pt.5.2 fix round 2).
func (m *Model) entryWindow(entries []Entry, avail int, focused bool) (start, end int) {
	if totalRows(entries) <= avail {
		return 0, len(entries)
	}

	budget := avail - 1
	if budget <= 0 {
		return 0, 0
	}

	start = windowStart(entries, m.row, budget, focused)
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

// windowStart picks the first index of the visible window so the cursor
// stays inside it, mirroring boardview's columnWindow scoped to a single
// column. Entry costs vary — a dangling id is one line, a labelled card
// cardfmt.Height(false)+1 — so this walks backwards from the cursor
// accumulating each entry's real cost, the same rule fitCount applies
// forwards, rather than assuming a uniform cost per entry: a uniform
// estimate on this side paired with real costs on fitCount's side is what
// let the two halves disagree about where the cursor actually landed.
//
// When the column isn't focused, when the cursor is already the first
// entry, or when starting at 0 already reaches the cursor within budget
// (fitCount from 0 gets at least that far), the window starts at 0 and
// nothing needs to scroll.
func windowStart(entries []Entry, cursorRow, budget int, focused bool) int {
	if !focused || cursorRow <= 0 || cursorRow >= len(entries) {
		return 0
	}
	if fitCount(entries, budget) > cursorRow {
		return 0
	}

	used, start := 0, cursorRow+1
	for start > 0 {
		cost := entryRows(entries[start-1])
		if used+cost > budget {
			break
		}
		used += cost
		start--
	}

	return start
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
