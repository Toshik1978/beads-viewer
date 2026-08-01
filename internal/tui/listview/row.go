package listview

// This file composes one list row and renders it two ways. Composition is
// split from styling because a selected row and an unselected one must lay
// out identical text at identical widths but cannot share a styling strategy
// — see rowColumns.styled.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// rowColumns is one row's fields, already sanitised and truncated to their
// final widths. Each field carries its own trailing (or leading, for age)
// separator, so plain and styled concatenate rather than re-deriving spacing
// and drifting apart.
type rowColumns struct {
	glyph    string
	id       string
	priority string
	status   string
	title    string
	labels   string
	age      string
}

// compose lays out issue's columns inside width cells.
func (d delegate) compose(issue *beads.Issue, width int) rowColumns {
	c := rowColumns{
		glyph:    d.theme.TypeGlyph(issue.IssueType) + " ",
		id:       fmt.Sprintf("%-*s ", d.idWidth, uitext.Sanitize(issue.ID)),
		priority: issue.Priority.Label() + " ",
		status:   fmt.Sprintf("%-*s ", statusColumnWidth, issue.Status.Display()),
	}

	remaining := width - ansi.StringWidth(c.glyph+c.id+c.priority+c.status)
	if remaining <= 0 {
		return c
	}

	c.age, remaining = d.fitAge(issue, remaining)
	c.title, c.labels = d.fitTitleAndLabels(issue, remaining)

	return c
}

// fitAge reserves the age column, or drops it when the row cannot afford one
// — the age goes first, before labels and before the title truncates.
//
// time.Now() is read here rather than captured on the delegate: the row is
// re-rendered every frame anyway, so ages stay current without a reload, and
// RelativeAge itself is pure, so this call is the whole untested surface.
func (d delegate) fitAge(issue *beads.Issue, remaining int) (age string, left int) {
	age = uitext.RelativeAge(time.Now(), issue.UpdatedAt)
	if age == "" {
		return "", remaining
	}

	cost := uitext.AgeWidth + 1 // one separating space
	if remaining-cost < labelsMinWidth {
		return "", remaining
	}

	// Right-aligned inside a fixed-width column so every row's age ends at
	// the same cell however many digits it has.
	return fmt.Sprintf(" %*s", uitext.AgeWidth, age), remaining - cost
}

// fitTitleAndLabels sanitises the title and joined labels, then truncates the
// title to fit remaining, reserving labelsMinWidth cells for labels when the
// issue has any and there is room. Sanitising happens before Truncate:
// ansi.StringWidth treats a newline as zero-width and ansi.Truncate never
// removes one, so an un-sanitised title containing "\n" would slip through
// both and render as a second physical row, shifting every row below it.
func (d delegate) fitTitleAndLabels(issue *beads.Issue, remaining int) (title, labels string) {
	sanitized := uitext.Sanitize(issue.Title)
	if len(issue.Labels) == 0 || remaining < labelsMinWidth {
		return uitext.Truncate(sanitized, remaining), ""
	}

	joined := uitext.Sanitize(strings.Join(issue.Labels, ","))
	labelsWidth := ansi.StringWidth(joined) + 2
	titleWidth := remaining - labelsWidth
	if titleWidth < labelsMinWidth {
		return uitext.Truncate(sanitized, remaining), ""
	}

	return uitext.Truncate(sanitized, titleWidth), "  " + joined
}

// plain concatenates the columns unstyled and pads the row out to width, with
// the age flush right. This is what a selected row renders, under one style.
func (c rowColumns) plain(width int) string {
	body := c.glyph + c.id + c.priority + c.status + c.title + c.labels

	return uitext.Truncate(pad(body, width-ansi.StringWidth(c.age))+c.age, width)
}

// styled renders the same columns each in its own style. Only unselected rows
// take this path: theme.Selected sets a background, and a per-column
// foreground inside it emits a reset that drops that background for the rest
// of the line — a visible gap running to the row's right edge. No background
// is involved here, so each segment's own reset is harmless.
func (c rowColumns) styled(th theme.Theme, issue *beads.Issue, width int) string {
	body := th.Base
	if issue.Status.IsTerminal() {
		body = th.Muted
	}

	line := th.Type(issue.IssueType).Render(c.glyph) +
		body.Render(c.id) +
		th.Priority(issue.Priority).Render(c.priority) +
		th.Status(issue.Status).Render(c.status) +
		body.Render(c.title) +
		th.Muted.Render(c.labels)

	plainWidth := ansi.StringWidth(c.glyph + c.id + c.priority + c.status + c.title + c.labels + c.age)
	if gap := width - plainWidth; gap > 0 {
		line += strings.Repeat(" ", gap)
	}

	line += th.Muted.Render(c.age)

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
