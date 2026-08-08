package tui

// This file is the fan-out layer: every method that takes a change in the
// world — a fresh snapshot, a detected terminal background, a new filter —
// and propagates it to the panes that have to hear about it. They sit
// together because they share a hazard rather than a topic: each one that
// forgets a pane leaves that pane rendering from something the rest of the
// app has already moved on from, and the symptom is always the same, a single
// stale pane beside correct ones.
//
// applyLayout is the fourth member of the family and lives in layout.go
// instead, next to the geometry it recomputes.

import (
	"log/slog"

	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/boardview"
	"github.com/Toshik1978/beads-viewer/internal/tui/detail"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// applyDetectedBackground turns a raw tea.BackgroundColorMsg into the
// theme.DetectedBackground applyBackground wants. Pulled out of Update so its
// conditional does not count against Update's own complexity limit.
func (m *Model) applyDetectedBackground(msg tea.BackgroundColorMsg) {
	detected := theme.BackgroundLight
	if msg.IsDark() {
		detected = theme.BackgroundDark
	}
	m.applyBackground(detected)
}

// applySnapshot handles the result of a reload. An error leaves the previous
// snapshot on screen and surfaces on the status line — unadorned, since I3
// moved the fix for a decode failure burying its own reason behind a
// workspace path to the source (jsonl.go). Runtime errors must never tear
// down the UI. A success re-applies the current filter and hands the result
// to every view, which is what lets each view restore its selection by id
// even though a new issue may have shifted every row.
func (m *Model) applySnapshot(msg SnapshotMsg) {
	if msg.Err != nil {
		m.setStatus(msg.Err.Error(), true)

		return
	}

	m.snapshot = msg.Snapshot
	m.setStatus("", false)
	m.applyFilter()
}

// applyBackground records the terminal's reported background and, if the
// resolved scheme changes, rebuilds the theme, restyles every view
// (View.SetTheme) and rebuilds the detail pane's glamour renderer — glamour
// v2 bakes its stylesheet name in at construction, so a stale detail pane
// would keep the old scheme after every other pane had moved on.
//
// An explicit --theme/BV_THEME preference always wins in theme.Resolve, so
// this is a scheme no-op whenever one was set; it still records background,
// which lets the help footer state that detection never happened even
// though it made no visible difference this time.
func (m *Model) applyBackground(detected theme.DetectedBackground) {
	m.overlay.background = detected

	next := theme.New(m.overlay.themePref, detected)
	if next.Scheme == m.theme.Scheme {
		return
	}

	// The same unreachable-in-practice case NewModel documents: detail.New
	// only fails on a glamour style theme.New already produced, so err just
	// keeps the old theme rather than tearing down the UI — logged (I7).
	detailPane, err := detail.New(m.log, next)
	if err != nil {
		if m.log != nil {
			m.log.Warn("rebuild detail pane for theme change", slog.Any("error", err))
		}
		return
	}

	m.theme = next
	for _, v := range m.views {
		v.SetTheme(next)
	}
	m.detail = detailPane
	_, detailHeight := m.layout.paneHeights()
	m.detail.SetSize(m.layout.DetailWidth, detailHeight)
	m.syncDetail()
}

// applyFilter is filtering's single call site: it narrows the current
// snapshot once and hands the result to every view.
//
// The board is the one exception, and it is a narrow one: it gets the same
// narrowing with hide-closed left out of it, plus the preference itself, so
// that it can honour or ignore that single criterion per swimlane (see
// boardview.Model.laneSnapshot). Every other criterion still reaches it the
// same way it reaches the rest — a text query narrows the board too.
func (m *Model) applyFilter() {
	filtered := m.filter.Apply(m.snapshot)
	for i, v := range m.views {
		if i == boardSlot {
			continue
		}
		v.SetSnapshot(filtered)
	}
	m.applyBoardFilter(filtered)
	m.syncDetail()
}

// applyBoardFilter hands the board its own narrowing, falling back to the
// shared one when the slot somehow does not hold a board — no view may be
// left holding a stale snapshot, whatever the slot turns out to contain.
func (m *Model) applyBoardFilter(filtered *beads.Snapshot) {
	board, ok := m.views[boardSlot].(*boardview.Model)
	if !ok {
		m.views[boardSlot].SetSnapshot(filtered)

		return
	}

	snap := filtered
	if m.filter.HideClosed {
		unhidden := m.filter
		unhidden.HideClosed = false
		snap = unhidden.Apply(m.snapshot)
	}
	board.SetHideClosed(m.filter.HideClosed)
	board.SetSnapshot(snap)
}

// syncDetail keeps the detail pane showing the active view's current
// selection. It runs after anything that might have moved the cursor —
// filtering, a reload, a keypress — cheaply: detail.Model.SetIssue is a
// no-op unless the selected id or the snapshot has actually changed, so
// calling this after every keypress does not re-render markdown for the
// keys (help, an unmatched key) that never touch selection. The full
// (unfiltered) snapshot is passed, not the filtered one views render from,
// so a blocker hidden by the active filter still resolves to a name and
// status here rather than reading as a dangling reference.
func (m *Model) syncDetail() {
	if m.detail == nil {
		return
	}
	m.detail.SetIssue(m.views[m.active].Selected(), m.snapshot)
}
