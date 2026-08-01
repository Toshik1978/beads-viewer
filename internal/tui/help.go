package tui

// This file renders the help overlay: every bound key, grouped by area
// (see helpGroups) and laid out in two columns balanced by rendered height
// rather than by binding count, so one large group cannot push a column past
// the bottom of an 80x24 terminal while the other still has room.

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// helpTitle and helpFootnote are the overlay's static framing. The footnote
// is what makes BV_THEME discoverable from inside a running bv.
const (
	helpTitle     = "bv — key bindings"
	helpFootnote  = "BV_THEME overrides the detected background: auto (default), light or dark."
	helpColumnGap = "   "
)

// helpGroup is one named section of the overlay.
type helpGroup struct {
	title    string
	bindings []key.Binding
}

// renderHelp lays out every bound key, grouped by area, in two columns
// balanced by height, centred over width x height. It is a pure function of
// its four arguments — an exported test seam — so it cannot know whether a
// tea.BackgroundColorMsg ever arrived; helpOverlay (app.go) calls
// renderHelpBody directly with that note instead, so the note is centred
// together with the rest rather than appended after this returns.
func renderHelp(keys KeyMap, th theme.Theme, width, height int) string {
	return renderHelpBody(keys, th, width, height, "")
}

// renderHelpBody composes the overlay — title, the two balanced columns, the
// static footnote and, when note is non-empty, one more line — then clips it
// to height and centres it over width x height. lipgloss.Place pads a block
// shorter than height but does not shorten one already taller, so an
// overlong body is clipped first; this must be the overlay's only Place
// call, since appending anything to its result afterward (as helpOverlay
// used to, for the background note) lands outside the centred block.
func renderHelpBody(keys KeyMap, th theme.Theme, width, height int, note string) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	colWidth := max((width-len(helpColumnGap))/2, 1)
	left, right := balanceColumns(th, helpGroups(keys), colWidth)
	columns := lipgloss.JoinHorizontal(lipgloss.Top,
		strings.Join(left, "\n"), helpColumnGap, strings.Join(right, "\n"))

	body := strings.Join([]string{
		th.Title.Render(uitext.Truncate(helpTitle, width)),
		"",
		columns,
		"",
		th.Muted.Render(uitext.Truncate(helpFootnote, width)),
	}, "\n")
	if note != "" {
		body += "\n" + note
	}

	if lines := strings.Split(body, "\n"); len(lines) > height {
		body = strings.Join(lines[:height], "\n")
	}

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}

// helpGroups is the overlay's single source of truth for which keys exist.
// The tree- and board-only bindings below are not KeyMap fields (that would
// push its 11 past the 20-field struct cap); they are declared here instead,
// mirroring the literal key strings treeview/nav.go's and boardview.go's own
// keyActions maps use — a deliberate duplication, since keyActions maps a
// key to a bound method value, not a label. treeview.HelpKeys and
// boardview.HelpKeys are what guard it: help_test.go asserts every key they
// report is named here, so drift now fails a test instead of shipping
// silently.
func helpGroups(keys KeyMap) []helpGroup {
	return []helpGroup{
		{"Global", []key.Binding{keys.Quit, keys.Help, keys.Yank, keys.Focus}},
		{"Views", []key.Binding{keys.ViewList, keys.ViewTree, keys.ViewBoard, keys.Open}},
		{"Filtering", []key.Binding{keys.Filter, keys.HideClosed}},
		{"Navigation", []key.Binding{
			keys.Up, keys.Down,
			key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("left/h", "collapse / move left")),
			key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("right/l", "expand / move right")),
			key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("home/g", "jump to top")),
			key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("end/G", "jump to bottom")),
			key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle expand")),
			key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "jump to parent (tree)")),
			key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "expand all (tree)")),
			key.NewBinding(key.WithKeys("O"), key.WithHelp("O", "collapse all (tree)")),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "cycle swimlane (board)")),
			keys.ScrollUp, keys.ScrollDown,
			key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "page up (tree)")),
			key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "page down (tree)")),
		}},
	}
}

// balanceColumns places each whole group into whichever column is currently
// shorter, sorted largest-first so the split is balanced rather than
// order-dependent.
func balanceColumns(th theme.Theme, groups []helpGroup, width int) (left, right []string) {
	slices.SortFunc(groups, func(a, b helpGroup) int { return len(b.bindings) - len(a.bindings) })

	for _, g := range groups {
		lines := renderGroup(th, g, width)
		if len(left) <= len(right) {
			left = append(sep(left), lines...)
		} else {
			right = append(sep(right), lines...)
		}
	}

	return left, right
}

// sep appends one blank separator line to column, unless it is still empty.
func sep(column []string) []string {
	if len(column) > 0 {
		return append(column, "")
	}

	return column
}

// renderGroup renders a title and one row per binding, joining each row's
// raw Binding.Keys() rather than its curated Help().Key label ("↑/k" does not
// contain the literal "ctrl+c" that Keys() does for Quit, and
// TestDocumentsEveryBoundKey checks for the raw spellings).
func renderGroup(th theme.Theme, g helpGroup, width int) []string {
	lines := make([]string, 0, len(g.bindings)+1)
	lines = append(lines, th.Title.Render(uitext.Truncate(g.title, width)))
	for _, b := range g.bindings {
		row := strings.Join(b.Keys(), "/") + "  " + b.Help().Desc
		lines = append(lines, th.Base.Render(uitext.Truncate(row, width)))
	}

	return lines
}
