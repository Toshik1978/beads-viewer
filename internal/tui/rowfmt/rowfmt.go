// Package rowfmt composes and styles one issue row for the panes that render
// issues as fixed columns — the list and the tree.
//
// It exists because a row cannot be styled uniformly and per-column at the
// same time: theme.Selected sets a background, and a per-column foreground
// inside it emits its own reset, which drops that background for everything
// after it. A row is therefore laid out once and rendered two ways, and that
// rule lives here rather than once per view.
//
// This package owns composition and styling only. It does not own the width
// ladder: which column a view sacrifices first differs between the list
// (age, then labels, then the title truncates) and the tree (age, then
// status, then the id), so each view fills Columns itself.
package rowfmt

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// StatusWidth is the widest label beads.Status.Display returns for a status
// br defines: "In Progress", 11 cells. It lives here, not in listview or
// treeview, because nothing pinned the two views' own copies equal to each
// other — a row must read the same in both panes, and a single constant is
// the only way that stays true when one of them changes. A custom status
// longer than this is never truncated or misaligned: each caller measures the
// status column's actual rendered width after padding, so an overlong status
// only pushes that row's later columns to the right, never out of line with
// the row above or below it.
const StatusWidth = 11

// FormatAge renders issue's age flush right in a fixed-width column, or ""
// when the issue carries no UpdatedAt. Both panes call this rather than
// formatting the age themselves, so the column reads identically in each —
// the tree uses the result outright, and the list additionally decides
// whether it can afford to keep it, which is why the budget decision itself
// stays with each caller instead of moving here.
func FormatAge(issue *beads.Issue) string {
	relative := uitext.RelativeAge(time.Now(), issue.UpdatedAt)
	if relative == "" {
		return ""
	}

	return fmt.Sprintf(" %*s", uitext.AgeWidth, relative)
}

// Columns is one row's fields, already sanitised and truncated to their final
// widths by the caller. Each field carries its own trailing separator — or
// leading, for Age — so Plain and Styled concatenate rather than re-deriving
// the spacing and drifting apart.
type Columns struct {
	Glyph    string
	ID       string
	Priority string
	Status   string
	Title    string
	Labels   string
	Age      string
}

// Plain concatenates the columns unstyled and pads the row out to width, with
// Age flush right. This is what a row rendered under a single style — a
// selected row, or the tree's retained non-matching ancestor — passes to that
// style.
func (c Columns) Plain(width int) string {
	body := c.Glyph + c.ID + c.Priority + c.Status + c.Title + c.Labels

	return uitext.Truncate(pad(body, width-ansi.StringWidth(c.Age))+c.Age, width)
}

// Styled renders the same columns each in its own style. Only rows that are
// not under a single style take this path: theme.Selected sets a background,
// and a per-column foreground inside it emits a reset that drops that
// background for the rest of the line — a visible gap running to the row's
// right edge. No background is involved here, so each segment's own reset is
// harmless.
func (c Columns) Styled(th theme.Theme, issue *beads.Issue, width int) string {
	body := th.Base
	if issue.Status.IsTerminal() {
		body = th.Muted
	}

	line := th.Type(issue.IssueType).Render(c.Glyph) +
		body.Render(c.ID) +
		th.Priority(issue.Priority).Render(c.Priority) +
		th.Status(issue.Status).Render(c.Status) +
		body.Render(c.Title) +
		th.Muted.Render(c.Labels)

	plainWidth := ansi.StringWidth(c.Glyph + c.ID + c.Priority + c.Status + c.Title + c.Labels + c.Age)
	if gap := width - plainWidth; gap > 0 {
		line += strings.Repeat(" ", gap)
	}

	line += th.Muted.Render(c.Age)

	// Safety net for an extreme narrow pane where the fixed columns alone
	// exceed width: a no-op when line already fits, ansi-aware otherwise, so
	// it never corrupts an already-styled segment's escape codes.
	return uitext.Truncate(line, width)
}

// pad right-pads s with spaces up to width cells. Callers have already
// truncated s to at most width, so this only ever adds space — ansi.
// StringWidth, not len, since a row can carry double-width CJK glyphs a byte
// count would overstate.
func pad(s string, width int) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}

	return s
}
