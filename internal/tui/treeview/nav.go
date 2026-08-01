package treeview

// This file turns the static tree tree.go and render.go build and draw into
// a navigable, windowed one: cursor movement, expand/collapse at the cursor,
// the hide-closed toggle, and the viewport arithmetic that keeps the
// rendered rows inside the pane's height. tree.go's Build/Retain/Flatten stay
// pure tree construction; everything here is what a keypress does to that
// tree's cursor and window.

import (
	"maps"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

// Update handles the keys that move the cursor, expand or collapse the node
// under it, and toggle hide-closed. Every other message is ignored — there
// is no other state here for a message to touch.
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

// ToggleHideClosed flips the hide-closed setting and rebuilds the tree from
// a fresh Build(snapshot). Retain mutates the nodes it is given, so
// re-filtering the tree a previous Retain call already pruned would not
// bring back a node a looser filter should restore — see tree.go's Retain.
func (m *Model) ToggleHideClosed() {
	m.hideClosed = !m.hideClosed
	m.rebuild()
}

// HiddenCount reports how many issues hide-closed is currently hiding.
//
// It counts nodes in the retained tree, not rows Flatten currently shows: a
// node hidden inside a collapsed parent is not "hidden" in the hide-closed
// sense, so collapsing a subtree must never change this number. When
// hide-closed is off, nothing is hidden by it — snapshot.Len() minus
// countNodes(m.roots) is not zero even then, because Retain's zero filter
// still drops tombstone-only subtrees on its own (see tree.go's Retain), and
// attributing that drop to hide-closed would report hidden issues with the
// feature disabled.
func (m *Model) HiddenCount() int {
	if !m.hideClosed || m.snapshot == nil {
		return 0
	}

	return m.snapshot.Len() - countNodes(m.roots)
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
// expanded node's id, the current selection, and hide-closed.
//
// This reads m.expandedIDs directly rather than walking m.roots the way
// ApplyState's own predecessor once did. Walking m.roots would miss any node
// currently hidden by an active hide-closed filter — Retain has pruned it out
// of the tree entirely, not merely hidden its row — so a save taken while
// filtered would silently forget that node's expansion instead of persisting
// the full picture m.expandedIDs actually holds.
func (m *Model) ExportState() State {
	expanded := make([]string, 0, len(m.expandedIDs))
	for id, isExpanded := range m.expandedIDs {
		if isExpanded {
			expanded = append(expanded, id)
		}
	}

	return State{Expanded: expanded, Selected: m.selected, HideClosed: m.hideClosed}
}

// ApplyState restores a previously persisted UI state. It is meant to run
// once, right after the first SetSnapshot, so the ids named in s.Expanded
// and s.Selected can already be resolved against the built tree.
//
// Replacing m.expandedIDs wholesale, rather than layering s.Expanded onto
// whatever rebuild would otherwise have preserved, is what makes this an
// exact restore: idSet never returns nil, even for a nil or empty
// s.Expanded, so rebuild treats a genuinely all-collapsed saved state as
// already initialized rather than as the very first build — see rebuild's
// own comment on why that distinction matters.
func (m *Model) ApplyState(s State) {
	m.hideClosed = s.HideClosed
	m.expandedIDs = idSet(s.Expanded)
	m.rebuild()

	if s.Selected != "" {
		m.SelectByID(s.Selected)
	}
}

// visibleRange returns the row window View renders.
//
// Both bounds are clamped. A negative offset slices backwards and panics,
// and it arises whenever the row count shrinks below the current offset —
// exactly what CollapseAll, a hide-closed toggle, and a reload that removes
// issues all do.
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

// rebuild reruns the full Build/Retain pipeline from the current snapshot and
// hideClosed setting. Retain mutates the nodes it is given, so every filter
// or data change starts over from a fresh Build rather than re-filtering the
// tree a previous Retain call already pruned.
//
// A fresh Build resets every node back to its depth==0 default, which is
// correct exactly once: the very first build, before the user has expanded or
// collapsed anything. Every later call — a watcher-driven SetSnapshot, a
// ToggleHideClosed — must instead preserve whatever the user already chose,
// or a live reload silently collapses the whole tree and strands the
// selection outside it (this is what reaching row depth 2 or deeper exposed).
//
// m.expandedIDs, not a value captured from m.roots on the spot, is what this
// restores from. A value captured from the immediately-prior m.roots only
// remembers whichever nodes that particular tree happened to contain — which
// is already a lossy, pruned subset whenever hideClosed is on, since Retain
// removes a wholly non-matching subtree rather than merely hiding its row.
// Capturing fresh on every call would lose that pruned subtree's expansion
// permanently the moment hideClosed turned it back on; m.expandedIDs instead
// persists across rebuilds independently of whichever nodes the tree happens
// to contain right now, so a toggle-off-then-on round trip restores exactly
// what the user chose even for a subtree hide-closed hid in between. A nil
// map (only ever true before the first build) is what lets Build's own
// depth==0 default stand — applying an empty-but-non-nil map here instead
// would collapse a tree the user genuinely left fully collapsed right back
// open, which is why ApplyState seeds idSet's result even for an empty
// s.Expanded rather than leaving m.expandedIDs nil.
func (m *Model) rebuild() {
	if m.snapshot == nil {
		m.roots, m.rows = nil, nil
		m.reconcileSelection()
		m.ensureCursorVisible()

		return
	}

	m.roots = Retain(Build(m.snapshot), beads.Filter{HideClosed: m.hideClosed})

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
// Expanded flag outside of rebuild — ToggleExpand, ExpandOrDescend,
// CollapseOrAscend — calls this immediately afterward, so the persistent
// record never drifts out of step with what is actually on screen.
func (m *Model) rememberExpanded(id string, expanded bool) {
	if m.expandedIDs == nil {
		m.expandedIDs = make(map[string]bool)
	}
	m.expandedIDs[id] = expanded
}

// rememberAllVisible mirrors every node currently in m.roots into
// m.expandedIDs. ExpandAll and CollapseAll use this rather than
// rememberExpanded, since they mutate every currently-present node at once —
// but, like rebuild, this only ever adds or overwrites entries for nodes
// m.roots actually contains right now; it never deletes an entry for a node
// hide-closed currently has pruned out; a CollapseAll while filtered must not
// erase the memory of a subtree it cannot even see.
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
// "pgup"/"pgdown" are deliberately absent: keys.go's KeyMap binds
// ScrollUp/ScrollDown to pgup/ctrl+u and pgdown/ctrl+d, and app.go's
// handleKey routes those two spellings to the detail pane whenever it is on
// screen — the normal case — so a tree-side binding on the same keys was
// dead code, confirmed live (two PageDown presses left the tree's cursor
// untouched while the detail pane scrolled). ctrl+b/ctrl+f page the tree
// instead, since neither collides with the detail pane's routing.
//
// None of these bindings live in KeyMap (keys.go), unlike Quit/Up/Down/etc.
// KeyMap's own doc says a view should read a binding from there rather than
// hard-coding it a second time, and "up"/"k"/"down"/"j" below already
// duplicate KeyMap.Up/KeyMap.Down. Task 7.1 renders its help overlay from
// KeyMap, so every binding here — h/l/o/O/c/p and the rest, not just the
// duplicated ones — is invisible to that overlay until it is folded in
// there; this table is not the source of truth it should eventually be.
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
		"c":      m.ToggleHideClosed,
		"ctrl+b": m.PageUp,
		"ctrl+f": m.PageDown,
	}
}

// countNodes counts every node in the tree, visible or not — HiddenCount
// needs the whole retained tree's size, not just what Flatten currently
// shows.
func countNodes(nodes []*Node) int {
	total := len(nodes)
	for _, n := range nodes {
		total += countNodes(n.Children)
	}

	return total
}

// recordExpandedIDs mirrors nodes' current Expanded flags into ids: adding or
// overwriting an entry for every node visited, but never deleting an entry
// for an id not present in nodes. That asymmetry is what lets a node hidden
// by the current hide-closed filter keep whatever expansion state it had the
// last time it was actually part of the tree — see rebuild's comment on why
// a value captured fresh from a filtered, already-pruned tree would lose it.
func recordExpandedIDs(nodes []*Node, ids map[string]bool) {
	for _, n := range nodes {
		ids[n.Issue.ID] = n.Expanded
		recordExpandedIDs(n.Children, ids)
	}
}

// applyExpandedIDs sets every node's Expanded flag from whether its id is in
// ids, overriding Build's depth-based default so a persisted or
// previously-chosen expansion survives a rebuild exactly as the user left it
// — a root collapsed, or a node several levels down left open.
func applyExpandedIDs(nodes []*Node, ids map[string]bool) {
	for _, n := range nodes {
		n.Expanded = ids[n.Issue.ID]
		applyExpandedIDs(n.Children, ids)
	}
}

// idSet converts ids into a set. It always returns a non-nil map, even for a
// nil or empty ids — the distinction rebuild relies on to tell "nothing has
// ever been built" (nil) apart from "a state was explicitly restored with
// nothing expanded" (empty, non-nil) matters here: ApplyState must be able to
// force the latter.
func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}

	return set
}
