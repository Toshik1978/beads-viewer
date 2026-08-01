package listview

// This file composes one list row; rowfmt.Columns renders it two ways.
// Composition is split from styling because a selected row and an
// unselected one must lay out identical text at identical widths but cannot
// share a styling strategy — see rowfmt.Columns.Styled.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/rowfmt"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// compose lays out issue's columns inside width cells.
func (d delegate) compose(issue *beads.Issue, width int) rowfmt.Columns {
	c := rowfmt.Columns{
		Glyph:    d.theme.TypeGlyph(issue.IssueType) + " ",
		ID:       fmt.Sprintf("%-*s ", d.idWidth, uitext.Sanitize(issue.ID)),
		Priority: issue.Priority.Label() + " ",
		Status:   fmt.Sprintf("%-*s ", statusColumnWidth, issue.Status.Display()),
	}

	remaining := width - ansi.StringWidth(c.Glyph+c.ID+c.Priority+c.Status)
	if remaining <= 0 {
		return c
	}

	c.Age, remaining = d.fitAge(issue, remaining)
	c.Title, c.Labels = d.fitTitleAndLabels(issue, remaining)

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
