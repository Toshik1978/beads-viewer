package tui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// clipboardMessageTTL is how long yank's "copied bv-124" confirmation stays
// on the status line. OSC52 gives no acknowledgement of its own, so this is
// the only signal the user gets that anything happened at all.
const clipboardMessageTTL = 3 * time.Second

// KeyMap is the single source of truth for bv's key bindings. Views that need
// to know a binding — the active view's own navigation, and the help overlay
// Task 7.1 adds — read it from here rather than hard-coding a keystroke a
// second time.
type KeyMap struct {
	Quit       key.Binding
	Up         key.Binding
	Down       key.Binding
	ViewList   key.Binding
	ViewTree   key.Binding
	ViewBoard  key.Binding
	Help       key.Binding
	Filter     key.Binding
	Yank       key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Focus      key.Binding
}

// DefaultKeyMap returns bv's built-in bindings. There is no user-configurable
// alternative yet, so this is the only constructor.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		ViewList:  key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "list")),
		ViewTree:  key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "tree")),
		ViewBoard: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "board")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Yank:      key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yank")),
		// ScrollUp and ScrollDown are pgup/pgdown rather than the list's own
		// up/k and down/j: bubbles/v2/list's default keymap already binds
		// pgup and pgdown to PrevPage/NextPage (list/keys.go), which would
		// otherwise fire even with pagination hidden. Routing these two keys
		// to the detail pane instead — see handleKey — is what makes a long
		// description actually scrollable without also paging the list.
		ScrollUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "scroll detail up")),
		ScrollDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "scroll detail down")),
		Focus:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus list / detail")),
	}
}

// overlayKind names which modal, if any, is capturing input ahead of the
// active view.
type overlayKind int

// The overlay states app.go's Update routes around before an active view
// ever sees a key.
const (
	overlayNone overlayKind = iota
	// overlayHelp is opened by Task 7.1's help binding; its content is that
	// task's, but the routing that keeps it from leaking a quit-key press
	// through to the active view belongs here.
	overlayHelp
	// overlayFilter edits Model.filter.Text in place; Enter applies it,
	// Escape discards the edit.
	overlayFilter
)

// focusTarget names which pane receives navigation keys. The detail pane can
// only be focused while it is actually on screen — see Model.detailOnScreen —
// so the zero value focusView is always valid, including before the first
// WindowSizeMsg arrives.
type focusTarget int

// The two panes that can hold focus.
const (
	focusView focusTarget = iota
	focusDetail
)

// overlayState is the modal editing state that must swallow keys before the
// active view or the global bindings see them. background and themePref
// round it out rather than adding fields to the capped Model (its own doc
// comment): themePref is what applyBackground re-resolves the theme with,
// background is what the help footer reads to warn that BV_THEME might need
// setting. Neither is "overlay" state, but this is one of the two structs
// sanctioned to grow instead.
type overlayState struct {
	kind       overlayKind
	buffer     string
	background theme.DetectedBackground
	themePref  config.ThemePreference
	// focus is which pane navigation keys reach. It rides here rather than on
	// Model for the reason overlayState's doc comment already gives: Model is
	// at its 12-field cap and this struct is one of the two sanctioned to grow.
	focus focusTarget
}

// handleKey implements the routing order app.go's Update documents: an open
// overlay swallows every key, an in-progress filter edit swallows every key
// next, then the global bindings, and only then the active view.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch m.overlay.kind {
	case overlayNone:
		// Nothing open; fall through to the global bindings and the active
		// view below.
	case overlayHelp:
		// Any key closes the overlay, including the one that would otherwise
		// quit — so "?" then "q" closes help rather than exiting bv. Only
		// kind and buffer clear: background and themePref are Model-lifetime
		// bookkeeping riding along on overlayState, not overlay-session state.
		m.overlay.kind, m.overlay.buffer = overlayNone, ""

		return nil
	case overlayFilter:
		return m.handleFilterKey(msg)
	}

	if cmd, handled := m.handleGlobalKey(msg); handled {
		return cmd
	}

	// Focus decides who gets what is left. A focused detail pane takes every
	// key the global bindings did not consume, not a hand-picked subset: a
	// model that forwarded only up and down would leave g, home and the rest
	// silently moving a cursor the user cannot see. pgup/pgdown reach the
	// detail pane either way — see ScrollUp/ScrollDown's comment in
	// DefaultKeyMap for why those two are bound to it specifically, and
	// detailOnScreen for why both a nil pane and a zero width count as absent.
	if m.detailOnScreen() {
		if m.overlay.focus == focusDetail {
			return m.detail.Update(msg)
		}
		if key.Matches(msg, m.keys.ScrollUp) || key.Matches(msg, m.keys.ScrollDown) {
			return m.detail.Update(msg)
		}
	}

	return m.views[m.active].Update(msg)
}

// overlayLine renders the single-line filter-edit overlay. The help overlay
// is multi-line and handled separately by Model.helpOverlay, which replaces
// the body outright instead of sharing this one-row budget.
func (m *Model) overlayLine() string {
	if m.overlay.kind != overlayFilter {
		return ""
	}

	return m.theme.Accent.Render("filter: " + m.overlay.buffer)
}

// handleGlobalKey applies the keys that are meaningful regardless of which
// view is active and reports whether it consumed the key press. app.go only
// falls through to the active view when this returns false. It is split in
// two purely to stay inside the complexity limit; the routing order is
// unchanged.
func (m *Model) handleGlobalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.handleViewSwitchKey(msg) {
		return nil, true
	}

	return m.handleActionKey(msg)
}

// toggleFocus moves focus between the active view and the detail pane. It is
// a no-op when there is no detail pane on screen: focusing something the user
// cannot see would silently swallow every navigation key.
func (m *Model) toggleFocus() {
	if !m.detailOnScreen() {
		return
	}

	if m.overlay.focus == focusView {
		m.overlay.focus = focusDetail

		return
	}

	m.overlay.focus = focusView
}

// detailOnScreen reports whether the detail pane is actually rendered. The
// nil check is the same one joinPanes, applyLayout and syncDetail all make: a
// Model built directly rather than through NewModel (as WhiteBoxSuite's
// spy-based tests do) has no detail pane, and DetailWidth alone does not rule
// that out. After the board goes full screen, DetailWidth is what makes this
// false on the board.
func (m *Model) detailOnScreen() bool {
	return m.detail != nil && m.layout.DetailWidth > 0
}

// handleViewSwitchKey applies the three view-selection keys and reports
// whether it consumed the press. Switching views returns focus to the view:
// leaving it on the detail pane would strand the new view's cursor until the
// user pressed Tab again. It never produces a tea.Cmd, unlike handleActionKey
// beside it, so it reports only the bool handleGlobalKey needs.
func (m *Model) handleViewSwitchKey(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, m.keys.ViewList):
		m.active, m.overlay.focus = 0, focusView
	case key.Matches(msg, m.keys.ViewTree):
		m.active, m.overlay.focus = 1, focusView
	case key.Matches(msg, m.keys.ViewBoard):
		m.active, m.overlay.focus = 2, focusView
	default:
		return false
	}

	return true
}

// handleActionKey applies the global keys that are not view selection.
func (m *Model) handleActionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return tea.Quit, true
	case key.Matches(msg, m.keys.Help):
		// See handleKey's overlayHelp case: only kind changes here, so
		// background, themePref and focus survive opening the overlay.
		m.overlay.kind = overlayHelp

		return nil, true
	case key.Matches(msg, m.keys.Filter):
		m.overlay.kind, m.overlay.buffer = overlayFilter, m.filter.Text
		m.applyLayout(m.layout.Width, m.layout.Height)

		return nil, true
	case key.Matches(msg, m.keys.Focus):
		m.toggleFocus()

		return nil, true
	case key.Matches(msg, m.keys.Yank):
		return m.yank(), true
	case msg.String() == "esc":
		// C1: empty.go's emptyFilter message says "press esc to clear the
		// filter", but that screen shows only once the box is already
		// closed, and handleFilterKey's Escape fires only while open.
		m.SetFilter(beads.Filter{})

		return nil, true
	default:
		return nil, false
	}
}

// handleFilterKey edits the in-progress filter text. Every key is consumed —
// that is what stops typing "q" into the filter box from quitting the app,
// the bug fixed upstream as #176 — except Enter, which applies the edit,
// Escape, which discards it, and ctrl+c, which quits (I4): it carries no
// msg.Text for the default branch to swallow it into, so it used to be
// silently dropped.
func (m *Model) handleFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return tea.Quit
	}

	switch msg.Code {
	case tea.KeyEnter:
		filter := m.filter
		filter.Text = m.overlay.buffer
		m.SetFilter(filter)
		m.overlay.kind, m.overlay.buffer = overlayNone, ""
		m.applyLayout(m.layout.Width, m.layout.Height)
	case tea.KeyEscape:
		m.overlay.kind, m.overlay.buffer = overlayNone, ""
		m.applyLayout(m.layout.Width, m.layout.Height)
	case tea.KeyBackspace:
		if n := len(m.overlay.buffer); n > 0 {
			m.overlay.buffer = m.overlay.buffer[:n-1]
		}
	default:
		if msg.Text != "" {
			m.overlay.buffer += msg.Text
		}
	}

	return nil
}

// clearStatusMsg asks Update (app.go) to clear the status line's transient
// message — yank's "copied bv-124" confirmation, today — once
// clearMessageAfter's delay has elapsed. token pins it to the specific
// message it was scheduled for: see Model.setStatus (status.go).
type clearStatusMsg struct{ token int }

// clearMessageAfter schedules a clearStatusMsg carrying token after d, so a
// stale timer from an earlier message cannot clear a newer, unrelated one
// that happened to land before it fired — a reload error arriving while a
// yank confirmation is still showing, for instance.
func clearMessageAfter(d time.Duration, token int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearStatusMsg{token: token} })
}

// yank copies the active view's selected issue id to the system clipboard
// via OSC52 (tea.SetClipboard) rather than a local-pasteboard library: the
// escape sequence travels through the terminal itself, so it works over SSH
// and inside tmux (given tmux's own set-clipboard on) without bv ever
// spawning a subprocess to get there — the reason atotto/clipboard was
// dropped rather than added. OSC52 gives no acknowledgement of its own, so
// the transient status message is what tells the user it actually happened.
// Nothing selected is a no-op: copying "" or panicking on a nil issue would
// both be worse than doing nothing.
func (m *Model) yank() tea.Cmd {
	issue := m.views[m.active].Selected()
	if issue == nil {
		return nil
	}

	m.setStatus("copied "+issue.ID, false)

	return tea.Batch(tea.SetClipboard(issue.ID), clearMessageAfter(clipboardMessageTTL, m.status.token))
}
