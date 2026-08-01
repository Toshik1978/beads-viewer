package tui

// This file renders the status bar: one line, its segments trimmed from the
// least important end until it fits — see renderStatus.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// statusHint reminds the user how to reach help; it is the first segment
// renderStatus drops once the bar cannot fit everything.
const statusHint = "? help"

// statusSeparator joins the bar's segments.
const statusSeparator = " │ "

// statusState is the status bar's content for one frame. Model composes a
// fresh value every render (statusLine, app.go) from live counts/filter/view
// plus Message/IsError/token, the part that persists across frames — set by
// Model.setStatus, called on a reload (success or failure) and on yank.
type statusState struct {
	Counts beads.Counts
	Filter beads.Filter
	View   config.ViewKind
	Lane   string
	// Hidden is how many issues the tree's hide-closed toggle (I2) is
	// hiding; 0 when off or the tree is not active. See treeHiddenCount.
	Hidden  int
	Message string
	IsError bool
	// token is yank's own generation counter (see setStatus below and
	// clearStatusMsg in keys.go). It plays no part in rendering — renderStatus
	// never reads it — so it does not affect status_test.go's suite, which
	// exercises this struct purely as render input.
	token int
}

// setStatus replaces the status line's persistent message and bumps its
// generation — see clearStatusMsg (keys.go) for why the generation exists:
// without it, a yank confirmation's own timer could clear a genuinely newer
// message (a reload error, say) that landed before the timer fired.
func (m *Model) setStatus(message string, isError bool) {
	m.status.token++
	m.status.Message, m.status.IsError = message, isError
}

// treeHiddenCount is how many issues the tree's own 'c' toggle is currently
// hiding, or 0 off-tree (I2). Mirrors boardLaneName's shape (app.go).
func (m *Model) treeHiddenCount() int {
	tree, ok := m.views[1].(*treeview.Model)
	if !ok || m.active != 1 {
		return 0
	}

	return tree.HiddenCount()
}

// renderStatus composes the one-line status bar, dropping segments from the
// tail of optional — statusHint, filter, view, then the count breakdown —
// until it fits; message is never dropped. optional's own order (counts,
// view, filter, hint) does not match the design example's left-to-right one
// on purpose: it makes dropping the slice's tail equal the documented
// priority. Every segment is sanitized before it is measured, not only
// rendered, so the fit decision below matches what actually reaches the
// screen.
//
// counts (optional[0]) is by far the widest segment, so the first loop stops
// at n == 1: dropping straight from "counts alone" to nothing threw the
// other three away too (I1, blank at widths 30-51). The second loop retries
// the same search over what is left, allowing the full drop to n == 0.
func renderStatus(st statusState, th theme.Theme, width int) string {
	view := string(st.View)
	if st.Lane != "" {
		view += " (" + st.Lane + ")"
	}
	if st.Hidden > 0 {
		view += fmt.Sprintf(" (%d hidden)", st.Hidden)
	}
	counts := fmt.Sprintf("%d issues · %d open · %d ready · %d blocked · %d closed",
		st.Counts.Total, st.Counts.Open, st.Counts.Ready, st.Counts.Blocked, st.Counts.Closed)
	optional := []string{counts, view, uitext.Sanitize(st.Filter.Describe()), statusHint}
	message := uitext.Sanitize(st.Message)

	for n := len(optional); n >= 1; n-- {
		prefix, full := assemble(optional[:n], message)
		if lipgloss.Width(full) <= width {
			return renderLine(th, prefix, full, st.IsError && message != "", width)
		}
	}

	rest := optional[1:]
	for n := len(rest); n >= 0; n-- {
		prefix, full := assemble(rest[:n], message)
		if lipgloss.Width(full) <= width || n == 0 {
			return renderLine(th, prefix, full, st.IsError && message != "", width)
		}
	}

	return ""
}

// assemble joins the optional segments the caller kept (skipping any that
// are themselves empty) into prefix, then appends message with the same
// separator, omitting it on either side when empty. Both are returned since
// renderLine needs prefix's length to know where message starts in full.
func assemble(optional []string, message string) (prefix, full string) {
	parts := make([]string, 0, len(optional))
	for _, seg := range optional {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	prefix = strings.Join(parts, statusSeparator)

	switch {
	case prefix == "":
		return prefix, message
	case message == "":
		return prefix, prefix
	default:
		return prefix, prefix + statusSeparator + message
	}
}

// renderLine truncates the fitted full line to width and styles it: the base
// bar style throughout, with the trailing message re-rendered in theme.Error
// when styleMessage, so a bad reload does not paint counts or the view name
// red too. Truncation only removes from the tail and message is always last
// in full, so boundary (from the untruncated prefix) still splits correctly.
//
// Both branches pad to width, as card.go's columnHeaderLines does; the error
// branch pads with a plain space run instead of Style.Width, since it
// concatenates two styled spans rather than rendering one.
func renderLine(th theme.Theme, prefix, full string, styleMessage bool, width int) string {
	truncated := uitext.Truncate(full, width)
	if !styleMessage {
		return th.StatusBar.Width(width).Render(truncated)
	}

	boundary := 0
	if prefix != "" { // assemble only inserts the separator when prefix is non-empty.
		boundary = len(prefix) + len(statusSeparator)
	}
	boundary = min(boundary, len(truncated))
	pad := strings.Repeat(" ", max(width-lipgloss.Width(truncated), 0))

	return th.StatusBar.Render(truncated[:boundary]) + th.Error.Render(truncated[boundary:]) + th.StatusBar.Render(pad)
}
