package boardview

// This file holds the renderer that turns Group's columns into a bubbletea
// view: the model's own state, card layout and column width fitting.
// group.go stays pure, bubbletea-free construction; nav.go owns the cursor
// and the windows that follow it.

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/cardfmt"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// Layout tunables shared by boardview.go's column fitting and card.go's card
// rendering.
const (
	// columnGap is the single blank column rendered between two card
	// columns, and between the last visible column and the "+N more" strip.
	columnGap = 1
	// minColumnWidth is the fewest total cells (border included) a column is
	// worth keeping at full size. Below it, visibleLayout drops columns from
	// a scrollable window instead of shrinking every card to illegibility.
	minColumnWidth = 18
	// markerWidth is the reserved width for each directional hidden-columns
	// marker ("<N" on the left, "N>" on the right; see renderMarker). Both
	// slots are reserved whenever any column is hidden, regardless of which
	// side currently has something to show, which is what keeps colWidth
	// fixed as the cursor scrolls a marker into or out of existence rather
	// than jittering by the marker's own width (see visibleLayout).
	markerWidth = 4
)

// Model is the board view: bv's kanban pane. It satisfies tui.View.
//
// columns is Group's output for the current snapshot and lane, rebuilt by
// regroup on every SetSnapshot and CycleSwimLane call. col and row are the
// cursor's position within it; clamp confines both to a cell that exists
// after every mutation, and selected mirrors whatever cell the cursor lands
// on so SelectedID and Selected agree with each other without a second
// lookup. expanded is a single board-wide toggle, not per-card: Task 6.2's
// brief lists ToggleExpand with no argument, and a per-card map would also
// need its own reconciliation on every regroup for no requested benefit.
// firstCol is the index of the leftmost rendered column; View's followCursor
// step keeps it tracking m.col so a cursor move past the edge of the
// visible window actually scrolls the board, rather than leaving the frame
// unchanged with the cursor invisibly off-screen at a narrower-than-full
// column count.
//
// lane's zero value is LaneStatus, which is always Valid(); the only method
// that ever advances it is Next(), which normalises an out-of-range receiver
// before stepping (group.go). Those two facts together are what satisfy this
// task's "normalise the swimlane before storing it" amendment: there is no
// path that stores a lane value Next() did not already produce or validate.
//
// A pin in this package's external test package (var _ tui.View =
// (*Model)(nil), in view_test.go) guards the tui.View claim above.
type Model struct {
	theme      theme.Theme
	width      int
	height     int
	snapshot   *beads.Snapshot
	lane       SwimLane
	columns    []Column
	col        int
	row        int
	selected   string
	expanded   bool
	firstCol   int
	hideClosed bool
}

// New builds an empty board view; SetSize and SetSnapshot fill it in once
// the root model knows the terminal's geometry and the workspace.
func New(th theme.Theme) *Model {
	return &Model{theme: th}
}

// SetHideClosed records the user's hide-closed preference, which this view
// applies for itself rather than receiving pre-applied — see laneSnapshot for
// why the answer depends on the lane, and tui.Model.applyFilter for the app
// side of the same arrangement.
func (m *Model) SetHideClosed(hide bool) {
	m.hideClosed = hide
	m.regroup(m.selected)
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

// SetSnapshot regroups the board from snap under the current lane,
// preserving the previously selected id when it still exists and otherwise
// clamping the existing cursor into the new grouping.
func (m *Model) SetSnapshot(snap *beads.Snapshot) {
	m.snapshot = snap
	m.regroup(m.selected)
}

// Update handles the keys that move the cursor, cycle the swimlane and
// toggle card expansion. Every other message is ignored — there is no other
// state here for a message to touch.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	if action, ok := keyActions(m)[key.String()]; ok {
		action()
	}

	return nil
}

// View renders the visible columns side by side.
//
// The zero-size guard is what keeps a degenerate terminal (width or height
// 0) from handing lipgloss a non-positive Width — Style.Render subtracts the
// border size from it internally (see card.go), and a caller-visible empty
// frame is the only answer a 0-cell pane can give regardless of how many
// issues it holds.
//
// It no longer carries its own "no issues" placeholder — see listview's
// identical note. It would be dead code besides: groupByStatus and its
// three siblings (group.go) always return their full, fixed set of columns,
// even against a wholly empty snapshot, so m.columns is only ever actually
// empty before the first SetSnapshot ever runs, which the app's own
// composition root does before any view is rendered.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	visible, colWidth, scrolls := m.visibleLayout()
	if visible <= 0 {
		return ""
	}
	m.followCursor(visible)

	blocks := make([]string, 0, visible*2+2)
	if scrolls {
		blocks = append(blocks, renderMarker(m.theme, m.firstCol, markerWidth, true),
			strings.Repeat(" ", columnGap))
	}
	for i := range visible {
		idx := m.firstCol + i
		blocks = append(blocks, m.renderColumn(m.columns[idx], colWidth, idx == m.col))
		if i < visible-1 || scrolls {
			blocks = append(blocks, strings.Repeat(" ", columnGap))
		}
	}
	if scrolls {
		hiddenRight := len(m.columns) - (m.firstCol + visible)
		blocks = append(blocks, renderMarker(m.theme, hiddenRight, markerWidth, false))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
}

// Selected returns the issue under the cursor, or nil when the cursor's
// column is empty (see clamp) or the board has no columns at all.
func (m *Model) Selected() *beads.Issue {
	if len(m.columns) == 0 || m.col >= len(m.columns) {
		return nil
	}

	issues := m.columns[m.col].Issues
	if m.row < 0 || m.row >= len(issues) {
		return nil
	}

	return issues[m.row]
}

// SelectedID returns the id of the selected issue, or "" when the cursor's
// column is empty.
func (m *Model) SelectedID() string {
	return m.selected
}

// Lane reports the board's current swimlane mode, for the status bar's
// indicator (see SwimLane.Name).
func (m *Model) Lane() SwimLane {
	return m.lane
}

// CycleSwimLane advances to the next grouping mode and regroups the board,
// keeping the same issue selected by id when it still exists under the new
// grouping rather than resetting the cursor to the first cell.
func (m *Model) CycleSwimLane() {
	m.lane = m.lane.Next()
	m.regroup(m.selected)
}

// ToggleExpand flips the board-wide card detail toggle: collapsed cards show
// only the id, priority and title; expanded cards add assignee, labels and a
// blocked-by or readiness line.
func (m *Model) ToggleExpand() {
	m.expanded = !m.expanded
}

// regroup rebuilds m.columns from the current snapshot and lane, then
// restores the cursor: first by looking up preserveID in the new grouping,
// and only if that id is gone, by re-clamping whatever column and row the
// cursor already held. Both SetSnapshot and CycleSwimLane call this, since a
// reload and a regrouping share exactly this recovery rule — see the Model
// comment on why selected must be looked up by id rather than assumed to
// have stayed at the same (col, row).
func (m *Model) regroup(preserveID string) {
	m.columns = Group(m.laneSnapshot(), m.lane)

	if col, row, ok := m.find(preserveID); ok {
		m.col, m.row = col, row
	}
	m.clamp()
	m.syncSelected()
}

// laneSnapshot applies the user's hide-closed preference for every lane but
// the status one, which is exempt.
//
// The app hands this view a snapshot its own hide-closed pass deliberately
// skipped (see tui.Model.applyFilter), because what hiding closed work means
// on a board depends on the lane: the status lane groups it into a Closed
// column of its own, where it is already set apart from the live work by the
// only thing hide-closed is trying to set it apart from — so honouring the
// preference there would leave a labelled column permanently empty and hide
// the very count a reader looks at that column for. Every other lane folds
// closed cards in among the rest (a closed P1 sits with the open P1s), which
// is exactly what the preference exists to prevent, so they honour it.
//
// Deciding here rather than at SetSnapshot is what makes the lane key work:
// CycleSwimLane regroups without the app being involved, so a decision taken
// when the snapshot arrived would be stale one keypress later. The cost is a
// re-filter per regrouping, which happens on a reload or a lane change, not
// per frame. Derived blockedness is unaffected either way — a narrowed
// snapshot resolves it against the workspace it was narrowed from (see
// beads.Snapshot's unfiltered field), so filtering twice cannot change what
// a card reports.
func (m *Model) laneSnapshot() *beads.Snapshot {
	if m.snapshot == nil || !m.hideClosed || m.lane == LaneStatus {
		return m.snapshot
	}

	return beads.Filter{HideClosed: true}.Apply(m.snapshot)
}

// visibleLayout decides how many columns fit at m.width and how wide each
// one is, windowing to a scrollable subset (see followCursor) rather than
// shrinking cards past minColumnWidth. scrolls reports whether any column
// is hidden at all; the reserve below is the same fixed 2*(columnGap+
// markerWidth) whether n is 1 or total-1 hidden columns short of total, so
// colWidth never changes as the cursor scrolls a marker into existence on
// one side or the other (F1's directional-marker ruling).
func (m *Model) visibleLayout() (visible, colWidth int, scrolls bool) {
	total := len(m.columns)
	if total == 0 || m.width <= 0 {
		return 0, 0, false
	}

	for n := total; n >= 1; n-- {
		reserve := 0
		if n < total {
			reserve = 2 * (columnGap + markerWidth)
		}
		avail := m.width - reserve - columnGap*(n-1)
		if avail <= 0 {
			continue
		}

		width := avail / n
		if width >= 1 && (width >= minColumnWidth || n == 1) {
			return n, width, n < total
		}
	}

	// Every candidate above failed the avail<=0 guard: m.width cannot hold
	// even one column plus both marker slots reserved. Reserving room the
	// terminal cannot afford would let the reserved-but-unrendered slots
	// alone exceed m.width once colWidth is clamped to 0 — this is F7's
	// regression, where the old fallback handed the single column the full
	// width and then a strip was appended on top of it. Dropping the
	// markers here instead keeps the whole frame inside m.width even at
	// degenerate sizes, at the cost of not announcing the hidden columns
	// when the terminal is this narrow.
	return 1, max(m.width, 0), false
}

// renderColumn renders one column's header and as many of its cards as fit
// in m.height, appending a "+N more" line when some had to be dropped.
// cursorRow is only meaningful when focused, since it addresses a row in
// this specific column.
func (m *Model) renderColumn(col Column, width int, focused bool) string {
	lines := columnHeaderLines(m.theme, col, width, focused)
	avail := m.height - len(lines)
	if avail <= 0 {
		return strings.Join(lines[:min(len(lines), max(m.height, 0))], "\n")
	}

	height := cardfmt.Height(m.expanded)
	maxFit := avail / height
	if len(col.Issues) > maxFit {
		// Cards will not all fit: reserve one line for the "+N more"
		// indicator appended below, or the column's own total would run one
		// line past avail — the height budget every other column in the row
		// is also held to.
		maxFit = max((avail-1)/height, 0)
	}
	start, end := columnWindow(m.row, maxFit, len(col.Issues), focused)

	for i := start; i < end; i++ {
		selected := focused && i == m.row
		lines = append(lines, cardfmt.Render(m.theme, m.snapshot, col.Issues[i], width, selected,
			m.expanded))
	}

	if hidden := len(col.Issues) - (end - start); hidden > 0 {
		lines = append(lines, m.theme.Muted.Render(uitext.Truncate(moreLabel(hidden), width)))
	}

	return strings.Join(lines, "\n")
}
