package treeview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// minTitleWidth is the fewest cells a truncated title is worth keeping.
// Below it, columns drops the id column first rather than truncating the
// title down to an unreadable sliver — that is what makes a deeply nested
// row degrade gracefully instead of collapsing.
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
// hideClosed replaces the placeholder filter field Task 5.2 left here — see
// the AMENDMENT in this task's brief — so Retain only ever narrows on this
// one axis inside the tree; every other Filter dimension is already applied
// upstream, once, by the app. expandedIDs is the persistent record of every
// node's own expansion choice, keyed by issue id rather than by *Node: a
// rebuild replaces every *Node in m.roots, and a value captured fresh from
// the immediately-prior m.roots on each rebuild would lose a subtree's
// expansion the moment hide-closed pruned it out entirely — expandedIDs
// survives that because nothing but an explicit mutation (ToggleExpand,
// ExpandAll, CollapseAll, ApplyState) ever changes it. nil distinguishes the
// very first Build/Retain pass, whose depth==0 Expanded default must stand
// untouched, from every later rebuild, which instead restores exactly what
// expandedIDs already records — see rebuild's own comment.
//
// A pin in this package's external test package (var _ tui.View =
// (*Model)(nil), in view_test.go) guards the tui.View claim above. That pin
// creates treeview_test -> tui -> treeview, not a cycle: treeview_test sits
// outside every package's real import graph.
type Model struct {
	theme       theme.Theme
	width       int
	height      int
	snapshot    *beads.Snapshot
	roots       []*Node
	hideClosed  bool
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
// allotted space: without it, a 41-row tree at height 5 would render all 41
// lines regardless, and internal/tui's joinPanes has no way to know that
// happened — it assumes every pane already fits the height it was given.
//
// It no longer carries its own generic "no issues" placeholder for an empty
// workspace, an empty filter result or a failed initial load — see
// listview's identical note for the app-level replacement (Model.body,
// empty.go), which intercepts all three before joinPanes ever calls this
// View. hideClosed (the 'c' key) is different: it is a tree-local narrowing
// the app-level filter cannot see, so a tree with only closed issues,
// filtered to hide them, is the one empty case View itself is still the
// only place that can explain — an empty m.rows here can only mean that,
// which is what the branch below names. Without it this rendered "" (a
// regression: pre-consolidation this pane said "No issues"), leaving a
// blank tree pane beside a populated status bar and detail pane with no
// hint that 'c' is the way back.
func (m *Model) View() string {
	if len(m.rows) == 0 {
		return m.theme.Muted.Render(uitext.Truncate("No open issues here. Press c to show closed.", m.width))
	}

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

// renderRow composes one row: the prefix, a type glyph, the id, the priority
// and the truncated title, styled for selection or a filter mismatch.
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
	remaining := width - ansi.StringWidth(prefix)
	line := prefix + m.columns(node.Issue, remaining)

	style := m.theme.Base
	switch {
	case selected:
		style = m.theme.Selected
	case !node.MatchesFilter:
		// Kept only so a matching descendant stays reachable, not a match
		// itself — muted marks that distinction rather than hiding it.
		style = m.theme.Muted
	}

	// A defensive final truncate: the layout below floors every intermediate
	// width at zero, but this is what actually guarantees no rendered line
	// exceeds width even at extreme depth, where the prefix alone can
	// approach or exceed the pane.
	return style.Render(uitext.Truncate(line, width))
}

// columns fills the space after the prefix: a type glyph, id, priority and
// the truncated title, dropping the id column first when depth has left too
// little room for both it and a legible title.
func (m *Model) columns(issue *beads.Issue, remaining int) string {
	glyph := m.theme.TypeGlyph(issue.IssueType)
	priority := issue.Priority.Label()
	title := uitext.Sanitize(issue.Title)
	id := uitext.Sanitize(issue.ID)

	withID := fmt.Sprintf("%s %s %s ", glyph, id, priority)
	if line, ok := fitTitle(withID, title, remaining); ok {
		return line
	}

	withoutID := fmt.Sprintf("%s %s ", glyph, priority)
	if line, ok := fitTitle(withoutID, title, remaining); ok {
		return line
	}

	// Neither layout clears minTitleWidth: append whatever of the title still
	// fits below that floor rather than dropping it outright. minTitleWidth
	// gates which layout is preferred, not whether a title renders at all —
	// a bare withoutID would leave free columns unused while the row
	// identifies nothing. The caller's final safety pass still truncates the
	// whole line, so even the priority label may not fully survive at this
	// width, which is the row degrading rather than the renderer panicking.
	return withoutID + uitext.Truncate(title, max(remaining-ansi.StringWidth(withoutID), 0))
}

// fitTitle appends as much of title as fits after columns, reporting false
// when even a floor-width title would not fit — the signal columns uses to
// try the next, narrower layout.
func fitTitle(columns, title string, remaining int) (string, bool) {
	titleWidth := remaining - ansi.StringWidth(columns)
	if titleWidth < minTitleWidth {
		return "", false
	}

	return columns + uitext.Truncate(title, titleWidth), true
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
