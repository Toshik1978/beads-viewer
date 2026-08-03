package depsview

// This file turns the static columns depsview.go renders into a navigable,
// re-rootable view: cursor movement, promoting a card to the focus, and the
// history that makes that walk reversible. deps.go stays pure column
// construction; depsview.go stays geometry, rendering and Reveal — which
// changes what the model is about rather than where its cursor is.

import (
	"maps"
	"slices"

	tea "charm.land/bubbletea/v2"
)

// Update handles the keys that move the cursor and change the focus. Every
// other message is ignored — there is no other state here to touch.
//
// This replaces the stub Update depsview.go carried so the type satisfied
// tui.View before there were any keys to dispatch.
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

// Descend promotes the highlighted card to the focus, pushing the outgoing
// one onto the history stack so Back can return to it.
//
// A dangling entry is a no-op: it names an id this workspace has no issue
// for, so re-rooting on it would render four empty columns describing
// nothing.
func (m *Model) Descend() {
	entry, ok := m.currentEntry()
	if !ok || entry.Issue == nil || entry.ID == m.focusID {
		return
	}

	previous := m.focusID
	if m.Reveal(entry.ID) && previous != "" {
		m.history = append(m.history, previous)
	}
}

// Back returns to the previously focused issue. An exhausted history is a
// no-op rather than a reset to anything: the user is already at the start of
// their own walk, and moving them somewhere else would be worse than doing
// nothing.
//
// An id that has since left the snapshot — a reload removed it, or the
// shared filter now excludes it — is popped and skipped rather than
// stopping the walk, so backspace keeps going back to somewhere that still
// exists.
func (m *Model) Back() {
	for len(m.history) > 0 {
		last := m.history[len(m.history)-1]
		m.history = m.history[:len(m.history)-1]
		if m.Reveal(last) {
			return
		}
	}
}

// MoveUp moves the cursor to the previous card in its column, stopping at
// the top.
func (m *Model) MoveUp() {
	m.row--
	m.clamp()
	m.syncSelected()
}

// MoveDown moves the cursor to the next card in its column, stopping at the
// bottom.
func (m *Model) MoveDown() {
	m.row++
	m.clamp()
	m.syncSelected()
}

// MoveLeft moves the cursor to the nearest populated column to the left, and
// stays put when there is none. Three of the four columns are routinely
// empty, so parking on one would leave the cursor with nothing to act on.
func (m *Model) MoveLeft() {
	m.col = m.nextPopulated(m.col, -1)
	m.row = 0
	m.clamp()
	m.syncSelected()
}

// MoveRight is MoveLeft's mirror.
func (m *Model) MoveRight() {
	m.col = m.nextPopulated(m.col, 1)
	m.row = 0
	m.clamp()
	m.syncSelected()
}

// JumpToTop moves the cursor to the first card of its column.
func (m *Model) JumpToTop() {
	m.row = 0
	m.clamp()
	m.syncSelected()
}

// JumpToBottom moves the cursor to the last card of its column.
func (m *Model) JumpToBottom() {
	if m.col >= 0 && m.col < len(m.columns) {
		m.row = len(m.columns[m.col].Entries)
	}
	m.clamp()
	m.syncSelected()
}

// HelpKeys returns every key keyActions binds, checked against helpGroups by
// internal/tui/help_test.go.
func HelpKeys() []string {
	return slices.Collect(maps.Keys(keyActions(&Model{})))
}

// nextPopulated returns the index of the nearest column in direction step
// holding at least one entry, or from when there is none — including when
// every column is empty, which is why the caller can treat the result as
// valid.
func (m *Model) nextPopulated(from, step int) int {
	for i := from + step; i >= 0 && i < len(m.columns); i += step {
		if len(m.columns[i].Entries) > 0 {
			return i
		}
	}

	return from
}

// keyActions maps a key's textual representation to the method it triggers.
// Built fresh per keypress rather than cached on Model, matching treeview's
// nav.go and boardview.go: gochecknoglobals rules out a package-level table,
// and a per-instance field would be a dozen bound-method values sitting idle
// for the life of the view to save one small map literal per human
// keystroke.
//
// enter and backspace appear here even though tui.Model's handleActionKey
// consumes both before a view sees them (keys.go). They are bound anyway so
// a caller driving this package directly behaves identically, and so
// HelpKeys reports them to help_test.go's drift guard.
func keyActions(m *Model) map[string]func() {
	return map[string]func(){
		"up": m.MoveUp, "k": m.MoveUp,
		"down": m.MoveDown, "j": m.MoveDown,
		"left": m.MoveLeft, "h": m.MoveLeft,
		"right": m.MoveRight, "l": m.MoveRight,
		"home": m.JumpToTop, "g": m.JumpToTop,
		"end": m.JumpToBottom, "G": m.JumpToBottom,
		"enter":     m.Descend,
		"backspace": m.Back,
	}
}
