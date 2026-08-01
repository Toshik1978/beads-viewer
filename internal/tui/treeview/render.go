package treeview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/rowfmt"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// minTitleWidth is the fewest cells a truncated title is worth keeping.
// Below it, columns drops the next column in the ladder first rather than
// truncating the title down to an unreadable sliver — that is what makes a
// deeply nested row degrade gracefully instead of collapsing.
const minTitleWidth = 10

// visibleRow pairs a node with the prefix metadata Prefix needs: the guide
// bars inherited from its ancestors, and whether it is its own parent's last
// child. Node carries neither — both are only knowable from a parent's
// children slice during the walk that produces this list, which is what
// flattenRows exists to do.
type visibleRow struct {
	node      *Node
	ancestors []bool
	isLast    bool
}

// Model is the tree view: bv's hierarchical pane. It satisfies tui.View.
//
// rows, cursor and offset (nav.go) are what make the view navigable and
// windowed: rows is flattenRows(roots, nil) kept in step with roots so a row
// index stays valid, cursor is the selected row's index into it, and offset
// is the first row View actually renders. rows carries the same prefix
// metadata (ancestors, isLast) View renders with, computed by the single walk
// refreshRows and rebuild both run — View slices m.rows directly rather than
// re-walking m.roots on its own, so the row window nav.go bounds against and
// the rows View actually draws can never drift out of step with each other.
// There is no filter field here at all: every Filter dimension, hide-closed
// included, is the app's, applied once upstream and arriving as a narrower
// snapshot. expandedIDs is the persistent record of every node's own
// expansion choice, keyed by issue id rather than by *Node: a rebuild
// replaces every *Node in m.roots, and a value captured fresh from the
// immediately-prior m.roots would lose a subtree's expansion the moment the
// filter excluded it entirely — nothing but an explicit mutation
// (ToggleExpand, ExpandAll, CollapseAll, ApplyState) ever changes it. nil
// distinguishes the very first Build/Retain pass, whose depth==0 Expanded
// default must stand untouched, from every later rebuild.
//
// A pin in this package's external test package (var _ tui.View =
// (*Model)(nil), in view_test.go) guards the tui.View claim above. It creates
// treeview_test -> tui -> treeview, not a cycle: treeview_test sits outside
// every package's real import graph.
type Model struct {
	theme       theme.Theme
	width       int
	height      int
	snapshot    *beads.Snapshot
	roots       []*Node
	expandedIDs map[string]bool
	selected    string
	rows        []visibleRow
	cursor      int
	offset      int
}

// New builds an empty tree view; SetSize and SetSnapshot fill it in once the
// root model knows the terminal's geometry and the workspace.
func New(th theme.Theme) *Model {
	return &Model{theme: th}
}

// SetTheme restyles the pane; theme is read fresh from m on every render.
func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

// SetSize records the pane's allotted geometry. A shrinking height can push
// the cursor outside the window the same way a shrinking row count does, so
// this re-clamps the viewport rather than leaving a stale offset in place
// until the next movement happens to fix it.
func (m *Model) SetSize(width, height int) {
	m.width = max(width, 0)
	m.height = max(height, 0)
	m.ensureCursorVisible()
}

// SetSnapshot rebuilds the tree from snap, preserving the previously
// selected id when it still exists and otherwise falling back to the nearest
// surviving row.
func (m *Model) SetSnapshot(snap *beads.Snapshot) {
	m.snapshot = snap
	m.rebuild()
}

// ExpandAll opens every node so the whole tree renders.
func (m *Model) ExpandAll() {
	setExpanded(m.roots, true)
	m.rememberAllVisible()
	m.refreshRows()
}

// CollapseAll closes every node, including the roots themselves, leaving
// only the top level visible.
func (m *Model) CollapseAll() {
	setExpanded(m.roots, false)
	m.rememberAllVisible()
	m.refreshRows()
}

// View renders the rows inside the current viewport.
//
// The height clamp is what keeps a tall tree from overflowing the pane's
// allotted space: without it, a 41-row tree at height 5 renders all 41 lines,
// and joinPanes assumes every pane already fits the height it was given.
//
// It carries no "no issues" placeholder of its own, exactly as listview and
// boardview carry none — Model.body (internal/tui, empty.go) decides that at
// the app level, before joinPanes ever calls this View. The tree kept one only
// while hide-closed was a narrowing Model.body could not see, and that
// message ("press c to show closed") now names the key that hides them.
func (m *Model) View() string {
	start, end := m.visibleRange()
	visible := m.rows[start:end]

	lines := make([]string, len(visible))
	for i, row := range visible {
		selected := row.node.Issue.ID == m.selected
		lines[i] = m.renderRow(row.node, row.ancestors, row.isLast, selected, m.width)
	}

	return strings.Join(lines, "\n")
}

// Selected returns the issue under the cursor, or nil when the tree is
// empty.
func (m *Model) Selected() *beads.Issue {
	if node, ok := FindNode(m.roots, m.selected); ok {
		return node.Issue
	}

	return nil
}

// renderRow composes one row: the prefix, then columns's type glyph, id,
// priority, status, age and truncated title. Three rendering paths, not two
// — a selected row and a retained non-matching ancestor each render under a
// single style, since both are a statement about the whole row rather than
// about any one column; every other row composes its columns each in their
// own style instead (see rowfmt.Columns.Styled's own doc comment for why a
// row under a single style cannot also be coloured per column).
//
// ancestors' length disagreeing with node.Depth is a bug in the walk that
// built rows, not a case to render around — silently drawing the wrong
// number of bars is the hardest version of that bug to spot, so this asserts
// the invariant instead of trusting the caller.
func (m *Model) renderRow(node *Node, ancestors []bool, isLast, selected bool, width int) string {
	if len(ancestors) != node.Depth {
		panic(fmt.Sprintf(
			"treeview: node %s at depth %d rendered with %d ancestors",
			node.Issue.ID, node.Depth, len(ancestors)))
	}

	prefix := Prefix(ancestors, isLast, len(node.Children) > 0, node.Expanded)
	remaining := max(width-ansi.StringWidth(prefix), 0)
	cols := m.columns(node.Issue, remaining)

	switch {
	case selected:
		return m.theme.Selected.Render(uitext.Truncate(prefix+cols.Plain(remaining), width))
	case !node.MatchesFilter:
		// Kept only so a matching descendant stays reachable, not a match
		// itself. That is a statement about the row rather than about any one
		// column, so it renders under a single style exactly as selection
		// does — per-column colour here would say the opposite.
		return m.theme.Muted.Render(uitext.Truncate(prefix+cols.Plain(remaining), width))
	default:
		// remaining already floors at zero, so cols.Styled never overruns its
		// own share of the line — but a prefix alone can still exceed width at
		// extreme depth (remaining is then already zero, so it has nowhere
		// left to give). This final truncate is ansi-aware, the same guarantee
		// rowfmt.Columns.Styled documents for its own internal one, so it
		// trims the guide bars themselves rather than corrupting a styled
		// segment's escape codes.
		return uitext.Truncate(m.theme.Base.Render(prefix)+cols.Styled(m.theme, node.Issue, remaining), width)
	}
}

// columns fills the space after the prefix, sacrificing columns in the order
// the tree can best afford: the age first, then the status, then the id, and
// only then does the title truncate. The prefix grows with depth, so the
// title — the one column that identifies the row — is what must survive.
func (m *Model) columns(issue *beads.Issue, remaining int) rowfmt.Columns {
	full := rowfmt.Columns{
		Glyph:    m.theme.TypeGlyph(issue.IssueType) + " ",
		ID:       uitext.Sanitize(issue.ID) + " ",
		Priority: issue.Priority.Label() + " ",
		Status:   fmt.Sprintf("%-*s ", rowfmt.StatusWidth, issue.Status.Display()),
		Age:      rowfmt.FormatAge(issue),
	}

	for _, drop := range []func(rowfmt.Columns) rowfmt.Columns{
		func(c rowfmt.Columns) rowfmt.Columns { return c },
		func(c rowfmt.Columns) rowfmt.Columns { c.Age = ""; return c },
		func(c rowfmt.Columns) rowfmt.Columns { c.Age, c.Status = "", ""; return c },
		func(c rowfmt.Columns) rowfmt.Columns { c.Age, c.Status, c.ID = "", "", ""; return c },
	} {
		candidate := drop(full)
		if title, ok := fitTitle(candidate, issue, remaining); ok {
			candidate.Title = title

			return candidate
		}
	}

	// No layout clears minTitleWidth: keep the narrowest one and append
	// whatever of the title still fits below that floor, rather than dropping
	// it outright. minTitleWidth gates which layout is preferred, not whether
	// a title renders at all — a row identifying nothing while free columns
	// sit unused is worse. renderRow's own truncate (via Plain/Styled) is the
	// backstop.
	narrowest := rowfmt.Columns{Glyph: full.Glyph, Priority: full.Priority}
	narrowest.Title = uitext.Truncate(
		uitext.Sanitize(issue.Title), max(remaining-fixedWidth(narrowest), 0))

	return narrowest
}

// fitTitle appends as much of issue's title as fits after columns' fixed
// fields, reporting false when even a floor-width title would not fit — the
// signal columns uses to try the next, narrower layout.
func fitTitle(columns rowfmt.Columns, issue *beads.Issue, remaining int) (string, bool) {
	titleWidth := remaining - fixedWidth(columns)
	if titleWidth < minTitleWidth {
		return "", false
	}

	return uitext.Truncate(uitext.Sanitize(issue.Title), titleWidth), true
}

// fixedWidth sums every column but Title — Title is what fitTitle solves for
// once the rest of the row's width is fixed.
func fixedWidth(c rowfmt.Columns) int {
	return ansi.StringWidth(c.Glyph + c.ID + c.Priority + c.Status + c.Age)
}

// setExpanded sets every node's Expanded flag, recursively.
func setExpanded(nodes []*Node, expanded bool) {
	for _, n := range nodes {
		n.Expanded = expanded
		setExpanded(n.Children, expanded)
	}
}

// flattenRows walks nodes in display order — mirroring Flatten's own descent
// in tree.go, which only recurses into an Expanded node's children — while
// threading through the per-ancestor continuation bits Prefix needs and the
// isLast bit for this row's own connector.
//
// A node's own contribution to its children's ancestors — !isLast, "does
// this node have a later sibling" — lands at the new slice's last index.
// Prefix draws a bar for every entry in ancestors, including that last one,
// so a node's own continuation is visible immediately in its direct
// children's rows (not merely promoted into visibility two levels down):
// that is what makes the guide beside a node's children stop exactly when
// the node itself has no later sibling to keep it open for.
func flattenRows(nodes []*Node, ancestors []bool) []visibleRow {
	var rows []visibleRow
	for i, n := range nodes {
		isLast := i == len(nodes)-1
		rows = append(rows, visibleRow{node: n, ancestors: ancestors, isLast: isLast})
		if !n.Expanded {
			continue
		}

		childAncestors := make([]bool, len(ancestors)+1)
		copy(childAncestors, ancestors)
		childAncestors[len(ancestors)] = !isLast
		rows = append(rows, flattenRows(n.Children, childAncestors)...)
	}

	return rows
}
