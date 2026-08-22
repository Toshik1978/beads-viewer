package tui

// This file is the filter overlay's own editing loop: the debounced
// edit-apply cycle behind the / overlay. It is the one piece of keys.go that
// is a self-contained state machine rather than a dispatch, which is what
// keeps keys.go about routing.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// filterDebounce is how long after the last keystroke the in-progress filter
// is applied. Filter.Apply rebuilds and re-indexes a whole second snapshot —
// benchmarked at 149us/818KB/1,655 allocs on a 758-record fixture — so a
// burst of typing must cost one re-filter, not one per character. 150ms is
// below the threshold at which a pause reads as lag and above a fast typist's
// inter-key interval.
const filterDebounce = 150 * time.Millisecond

// filterPrompt labels the overlay line. It is pure ASCII, so its byte length
// is also its width in terminal cells — which is what overlayLine subtracts
// from the frame to size the buffer beside it.
const filterPrompt = "filter: "

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

// overlayLine renders the single-line filter-edit overlay. The help overlay
// is multi-line and handled separately by Model.helpOverlay, which replaces
// the body outright instead of sharing this one-row budget.
//
// A buffer wider than the frame is clipped from its head, not its tail: the
// end is where the next character lands, so that is the half worth showing.
// Clipping at all is what keeps the "single-line" in this comment true —
// an over-wide line wraps, and the extra physical row pushes the status bar
// off the bottom of the frame. Reaching that by typing takes a while;
// pasting reaches it in one keystroke, which is what made the guard worth
// having rather than merely arguable.
func (m *Model) overlayLine() string {
	if m.overlay.kind != overlayFilter {
		return ""
	}

	buffer := m.overlay.buffer
	if room := m.layout.Width - len(filterPrompt); room > 0 {
		buffer = uitext.TruncateLeft(buffer, room)
	}

	return m.theme.Accent.Render(filterPrompt + buffer)
}

// handlePaste enters bracketed-paste content into the filter box, which is
// the only thing in bv that accepts text.
//
// bubbletea enables bracketed paste by default, so a terminal paste arrives
// as one tea.PasteMsg rather than as the burst of tea.KeyPressMsg
// handleFilterKey reads — which is why the filter box could only ever be
// typed into. The paste was not merely ignored: Update's default branch
// forwarded it to the active view sitting behind the open overlay, the one
// thing handleFilterKey's "every key is consumed" rule exists to prevent.
//
// Anywhere but the filter box a paste is dropped rather than forwarded, for
// the same reason: nothing else here reads text, and passing it through
// would leave that hole open.
//
// uitext.Sanitize, rather than folding control characters into spaces: the
// commonest paste of all is an id copied out of a terminal, which brings its
// trailing newline with it, and beads.Filter matches a substring — a space
// left in the middle of one would match nothing.
func (m *Model) handlePaste(content string) tea.Cmd {
	if m.overlay.kind != overlayFilter {
		return nil
	}

	m.overlay.buffer += uitext.Sanitize(content)

	return m.scheduleFilterApply()
}
