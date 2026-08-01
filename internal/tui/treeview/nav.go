package treeview

// This file turns the static tree tree.go and render.go build and draw into
// a navigable, windowed one: cursor movement, expand/collapse at the cursor,
// and the viewport arithmetic that keeps the rendered rows inside the pane's
// height. tree.go stays pure tree construction; everything here is what a
// keypress does to that tree's cursor and window.

import (
	"maps"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

// Update handles the keys that move the cursor and expand or collapse the
// node under it. Every other message is ignored — there is no other state
// here to touch.
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

// MoveUp moves the cursor to the previous row, stopping at the top.
func (m *Model) MoveUp() {
	m.setCursor(m.cursor - 1)
}

// MoveDown moves the cursor to the next row, stopping at the bottom.
func (m *Model) MoveDown() {
	m.setCursor(m.cursor + 1)
}

// PageUp moves the cursor up by one pane height, stopping at the top.
func (m *Model) PageUp() {
	m.setCursor(m.cursor - max(m.height, 1))
}

// PageDown moves the cursor down by one pane height, stopping at the bottom.
func (m *Model) PageDown() {
	m.setCursor(m.cursor + max(m.height, 1))
}

// JumpToTop moves the cursor to the first row.
func (m *Model) JumpToTop() {
	m.setCursor(0)
}

// JumpToBottom moves the cursor to the last row.
func (m *Model) JumpToBottom() {
	m.setCursor(len(m.rows) - 1)
}

// JumpToParent moves the cursor to the current node's parent. A root has no
// parent to jump to, so the cursor is left exactly where it was.
func (m *Model) JumpToParent() {
	node, ok := m.currentNode()
	if !ok || m.snapshot == nil {
		return
	}

	parent, ok := m.snapshot.Parent(node.Issue.ID)
	if !ok {
		return
	}

	m.SelectByID(parent.ID)
}

// ToggleExpand flips the current node's own Expanded flag. A leaf has
// nothing to expand and is left alone.
func (m *Model) ToggleExpand() {
	node, ok := m.currentNode()
	if !ok || len(node.Children) == 0 {
		return
	}

	node.Expanded = !node.Expanded
	m.rememberExpanded(node.Issue.ID, node.Expanded)
	m.refreshRows()
}

// ExpandOrDescend opens the current node when it is collapsed, or moves the
// cursor to its first child when it is already open — one key that either
// reveals a subtree or drills into it, depending on what is already visible.
func (m *Model) ExpandOrDescend() {
	node, ok := m.currentNode()
	if !ok || len(node.Children) == 0 {
		return
	}

	if !node.Expanded {
		node.Expanded = true
		m.rememberExpanded(node.Issue.ID, true)
		m.refreshRows()

		return
	}

	m.MoveDown()
}

// CollapseOrAscend closes the current node when it is open, or moves the
// cursor to its parent when it is already closed — the mirror of
// ExpandOrDescend.
func (m *Model) CollapseOrAscend() {
	node, ok := m.currentNode()
	if ok && len(node.Children) > 0 && node.Expanded {
		node.Expanded = false
		m.rememberExpanded(node.Issue.ID, false)
		m.refreshRows()

		return
	}

	m.JumpToParent()
}

// SelectByID moves the cursor to id and reports success. A failed lookup
// leaves the cursor exactly where it was.
func (m *Model) SelectByID(id string) bool {
	idx := m.rowIndex(id)
	if idx < 0 {
		return false
	}

	m.setCursor(idx)

	return true
}

// SelectedID returns the id of the row under the cursor, or "" when the tree
// is empty.
func (m *Model) SelectedID() string {
	return m.selected
}

// ExportState captures the tree's current UI state for persistence: every
// expanded node's id and the current selection. It reads m.expandedIDs
// directly rather than walking m.roots, which would miss any node the app's
// shared filter currently excludes — that node never reaches Build at all, so
// a save taken while filtered would silently forget its expansion.
func (m *Model) ExportState() State {
	expanded := make([]string, 0, len(m.expandedIDs))
	for id, isExpanded := range m.expandedIDs {
		if isExpanded {
			expanded = append(expanded, id)
		}
	}

	return State{Expanded: expanded, Selected: m.selected}
}

// ApplyState restores a previously persisted UI state. It is meant to run
// once, right after the first SetSnapshot, so the ids named in s.Expanded and
// s.Selected can already be resolved against the built tree.
//
// Replacing m.expandedIDs wholesale, rather than layering s.Expanded onto
// whatever rebuild would otherwise have preserved, is what makes this an
// exact restore: idSet never returns nil, so rebuild treats a genuinely
// all-collapsed saved state as already initialized rather than as a first
// build — see rebuild's own comment on why that distinction matters.
func (m *Model) ApplyState(s State) {
	m.expandedIDs = idSet(s.Expanded)
	m.rebuild()

	if s.Selected != "" {
		m.SelectByID(s.Selected)
	}
}

// visibleRange returns the row window View renders.
//
// Both bounds are clamped. A negative offset slices backwards and panics, and
// it arises whenever the row count shrinks below the current offset — exactly
// what CollapseAll, a narrowed filter and a shrinking reload all do.
func (m *Model) visibleRange() (start, end int) {
	total := len(m.rows)
	height := max(m.height, 0)

	start = min(max(m.offset, 0), max(total-1, 0))
	end = min(start+height, total)

	return start, end
}

// ensureCursorVisible scrolls the window so the cursor sits inside it.
func (m *Model) ensureCursorVisible() {
	if m.height <= 0 || len(m.rows) == 0 {
		m.offset = 0

		return
	}

	switch {
	case m.cursor < m.offset:
		m.offset = m.cursor
	case m.cursor >= m.offset+m.height:
		m.offset = m.cursor - m.height + 1
	}

	m.offset = min(max(m.offset, 0), max(len(m.rows)-m.height, 0))
}

// setCursor moves the cursor to row, clamped to the current row range, and
// keeps the selected id and the viewport in sync with it.
func (m *Model) setCursor(row int) {
	if len(m.rows) == 0 {
		m.cursor, m.selected = 0, ""
		m.ensureCursorVisible()

		return
	}

	m.cursor = min(max(row, 0), len(m.rows)-1)
	m.selected = m.rows[m.cursor].node.Issue.ID
	m.ensureCursorVisible()
}

// currentNode returns the node under the cursor, or false when the tree is
// empty.
func (m *Model) currentNode() (*Node, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil, false
	}

	return m.rows[m.cursor].node, true
}

// rowIndex returns id's position among the current rows, or -1.
func (m *Model) rowIndex(id string) int {
	for i, r := range m.rows {
		if r.node.Issue.ID == id {
			return i
		}
	}

	return -1
}

// refreshRows re-flattens m.roots after a change that only touched Expanded
// flags — the tree's shape did not change, so this skips rebuild's
// Build/Retain pass and just re-derives the visible rows, reconciling the
// cursor against them. It uses flattenRows, not Flatten, so m.rows carries
// the same prefix metadata View renders with — see the Model comment on rows.
func (m *Model) refreshRows() {
	m.rows = flattenRows(m.roots, nil)
	m.reconcileSelection()
	m.ensureCursorVisible()
}

// rebuild reruns the full Build/Retain pipeline from the current snapshot.
// Retain mutates the nodes it is given, so every data change starts over from
// a fresh Build rather than re-filtering an already-pruned tree. The Retain
// call stays even though its filter is now always zero: a zero filter is not
// the identity — Retain's own doc records that it still drops tombstone-only
// subtrees — so removing it would put deletion markers on screen for a caller
// driving this package directly, as its own tests do; not inside the composed
// app, where applyFilter (tui/app.go) has dropped them already. Every other
// narrowing is the app's, applied upstream and delivered as a narrower snapshot.
//
// A fresh Build resets every node back to its depth==0 default, correct
// exactly once: the very first build, before the user has expanded or
// collapsed anything. Every later call must instead preserve whatever the
// user already chose, or a live reload silently collapses the whole tree and
// strands the selection outside it (this is what reaching row depth 2 or
// deeper exposed). m.expandedIDs, not a value captured from m.roots on the
// spot, is what this restores from — see the Model comment on that field
// (render.go) for why a fresh capture loses a subtree the filter excluded. A
// nil map (only ever true before the first build) is what lets Build's own
// default stand: an empty-but-non-nil map would reopen a tree the user
// genuinely left collapsed, which is why ApplyState seeds idSet's result
// even for an empty s.Expanded.
func (m *Model) rebuild() {
	if m.snapshot == nil {
		m.roots, m.rows = nil, nil
		m.reconcileSelection()
		m.ensureCursorVisible()

		return
	}

	m.roots = Retain(Build(m.snapshot), beads.Filter{})

	if m.expandedIDs == nil {
		m.expandedIDs = make(map[string]bool)
		recordExpandedIDs(m.roots, m.expandedIDs)
	} else {
		applyExpandedIDs(m.roots, m.expandedIDs)
	}

	m.rows = flattenRows(m.roots, nil)
	m.reconcileSelection()
	m.ensureCursorVisible()
}

// rememberExpanded records a single node's Expanded flag into m.expandedIDs,
// initializing the map on first use. Every direct mutation of a node's own
// Expanded flag outside rebuild — ToggleExpand, ExpandOrDescend,
// CollapseOrAscend — calls this immediately afterward, so the record never
// drifts out of step with what is on screen.
func (m *Model) rememberExpanded(id string, expanded bool) {
	if m.expandedIDs == nil {
		m.expandedIDs = make(map[string]bool)
	}
	m.expandedIDs[id] = expanded
}

// rememberAllVisible mirrors every node currently in m.roots into
// m.expandedIDs. ExpandAll and CollapseAll use this rather than
// rememberExpanded, since they mutate every present node at once — but, like
// rebuild, it only ever adds or overwrites, never deleting an entry for a
// node the app's filter excludes: a CollapseAll while filtered must not erase
// the memory of a subtree it cannot even see.
func (m *Model) rememberAllVisible() {
	if m.expandedIDs == nil {
		m.expandedIDs = make(map[string]bool)
	}
	recordExpandedIDs(m.roots, m.expandedIDs)
}

// reconcileSelection keeps the cursor on the previously selected id when it
// is still present among the rows. When it is not — a reload dropped the
// issue, or a collapse hid its row — the cursor is clamped into the current
// range and whichever row ends up there becomes the new selection, rather
// than leaving a dangling id or resetting to the top unconditionally.
func (m *Model) reconcileSelection() {
	if idx := m.rowIndex(m.selected); idx >= 0 {
		m.cursor = idx

		return
	}

	if len(m.rows) == 0 {
		m.cursor, m.selected = 0, ""

		return
	}

	m.cursor = min(max(m.cursor, 0), len(m.rows)-1)
	m.selected = m.rows[m.cursor].node.Issue.ID
}

// HelpKeys returns every key keyActions binds, checked against helpGroups by internal/tui/help_test.go.
func HelpKeys() []string {
	return slices.Collect(maps.Keys(keyActions(&Model{})))
}

// keyActions maps a key's textual representation to the method it triggers.
// Built fresh per keypress rather than cached on Model: gochecknoglobals
// rules out a package-level table, and a per-instance field would be a
// dozen bound-method values sitting idle for the life of the view merely to
// save one small map literal per human keystroke.
//
// "pgup"/"pgdown" are deliberately absent: app.go's handleKey routes those
// two spellings to the detail pane whenever it is on screen — the normal case
// — so a tree-side binding on them was dead code, confirmed live (two
// PageDown presses left the tree's cursor untouched while the detail pane
// scrolled). ctrl+b/ctrl+f page the tree instead, colliding with nothing.
//
// None of these bindings live in KeyMap (keys.go), unlike Quit/Up/Down/etc,
// and "up"/"k"/"down"/"j" below already duplicate KeyMap.Up/KeyMap.Down. The
// help overlay renders from KeyMap, so every binding here is invisible to it
// until it is folded in there the way "c" was; this table is not the source
// of truth it should eventually be.
func keyActions(m *Model) map[string]func() {
	return map[string]func(){
		"up": m.MoveUp, "k": m.MoveUp,
		"down": m.MoveDown, "j": m.MoveDown,
		"left": m.CollapseOrAscend, "h": m.CollapseOrAscend,
		"right": m.ExpandOrDescend, "l": m.ExpandOrDescend,
		"space":  m.ToggleExpand,
		"home":   m.JumpToTop,
		"g":      m.JumpToTop,
		"end":    m.JumpToBottom,
		"G":      m.JumpToBottom,
		"p":      m.JumpToParent,
		"o":      m.ExpandAll,
		"O":      m.CollapseAll,
		"ctrl+b": m.PageUp,
		"ctrl+f": m.PageDown,
	}
}

// recordExpandedIDs mirrors nodes' current Expanded flags into ids: adding or
// overwriting an entry for every node visited, but never deleting one for an
// id not present in nodes. That asymmetry is what lets a node the app's filter
// excludes keep the expansion it had when it was last in the tree.
func recordExpandedIDs(nodes []*Node, ids map[string]bool) {
	for _, n := range nodes {
		ids[n.Issue.ID] = n.Expanded
		recordExpandedIDs(n.Children, ids)
	}
}

// applyExpandedIDs sets every node's Expanded flag from whether its id is in
// ids, overriding Build's depth-based default so a persisted or
// previously-chosen expansion survives a rebuild exactly as the user left it.
func applyExpandedIDs(nodes []*Node, ids map[string]bool) {
	for _, n := range nodes {
		n.Expanded = ids[n.Issue.ID]
		applyExpandedIDs(n.Children, ids)
	}
}

// idSet converts ids into a set, always non-nil even for a nil or empty ids:
// rebuild reads nil as "nothing has ever been built" and an empty map as "a
// state was restored with nothing expanded", and ApplyState must be able to
// force the latter.
func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}

	return set
}
