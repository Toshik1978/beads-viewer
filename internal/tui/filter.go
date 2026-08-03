package tui

// This file is the filter overlay's own editing loop: the debounced
// edit-apply cycle behind the / overlay. It is the one piece of keys.go that
// is a self-contained state machine rather than a dispatch, which is what
// keeps keys.go about routing.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// filterDebounce is how long after the last keystroke the in-progress filter
// is applied. Filter.Apply rebuilds and re-indexes a whole second snapshot —
// benchmarked at 149us/818KB/1,655 allocs on a 758-record fixture — so a
// burst of typing must cost one re-filter, not one per character. 150ms is
// below the threshold at which a pause reads as lag and above a fast typist's
// inter-key interval.
const filterDebounce = 150 * time.Millisecond

// handleFilterKey edits the in-progress filter text. Every key is consumed —
// that is what stops typing "q" into the filter box from quitting the app,
// the bug fixed upstream as #176 — except Enter, which commits the edit
// synchronously, Escape, which restores the filter as it was when the box
// opened, and ctrl+c, which quits (I4): it carries no msg.Text for the
// default branch to swallow it into, so it used to be silently dropped.
// Backspace and a printed character both schedule a debounced apply rather
// than applying immediately, so a burst of typing costs one re-filter.
func (m *Model) handleFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}

	switch msg.Code {
	case tea.KeyEnter:
		filter := m.filter
		filter.Text = m.overlay.buffer
		m.SetFilter(filter)
		m.closeFilterOverlay()
	case tea.KeyEscape:
		// Restore, not clear. Typing applies as it goes now, so m.filter
		// already holds the abandoned edit; filterBefore is the only record
		// of what the user had before opening the box.
		m.SetFilter(m.overlay.filterBefore)
		m.closeFilterOverlay()
	case tea.KeyBackspace:
		if n := len(m.overlay.buffer); n > 0 {
			m.overlay.buffer = m.overlay.buffer[:n-1]
		}

		return m.scheduleFilterApply()
	default:
		if msg.Text != "" {
			m.overlay.buffer += msg.Text

			return m.scheduleFilterApply()
		}
	}

	return nil
}

// scheduleFilterApply debounces the live filter: every keystroke bumps the
// generation and schedules exactly one tick.
//
// token is captured by value here, deliberately. A closure that read
// m.overlay.filterToken when the tick fired would always see the latest
// generation, so every stale tick would report itself as current and the
// guard in applyBufferedFilter would never reject anything.
func (m *Model) scheduleFilterApply() tea.Cmd {
	m.overlay.filterToken++
	token := m.overlay.filterToken

	return tea.Tick(filterDebounce, func(time.Time) tea.Msg {
		return filterTickMsg{token: token}
	})
}

// applyBufferedFilter applies the in-progress text, but only for the tick the
// most recent keystroke scheduled, and only while the overlay is still open.
// The second guard is what makes a tick that outlived Escape harmless: by
// then the previous filter has already been restored, and re-applying the
// abandoned buffer over it would undo that silently, a frame later.
func (m *Model) applyBufferedFilter(token int) {
	if token != m.overlay.filterToken || m.overlay.kind != overlayFilter {
		return
	}

	filter := m.filter
	filter.Text = m.overlay.buffer
	m.SetFilter(filter)
}

// closeFilterOverlay clears the overlay and gives the body back the row the
// filter line was using. Enter and Escape differ only in which filter they
// leave behind, so the teardown is shared.
func (m *Model) closeFilterOverlay() {
	m.overlay.kind, m.overlay.buffer = overlayNone, ""
	m.applyLayout(m.layout.Width, m.layout.Height)
}

// filterTickMsg asks Update (app.go) to apply the in-progress filter text.
// token pins it to the keystroke that scheduled it, so a tick still in flight
// when another character arrives applies nothing.
type filterTickMsg struct{ token int }
