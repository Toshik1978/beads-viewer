package listview

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// labelsMinWidth is the fewest cells that must remain after the fixed
// columns and the truncated title before labels are worth rendering; below
// it a label would itself need truncating past legibility.
const labelsMinWidth = 12

// statusColumnWidth is the widest label beads.Status.Display returns for a
// status br defines: "In Progress", 11 cells. A custom status longer than
// this is never truncated or misaligned — row() measures the prefix's actual
// rendered width with ansi.StringWidth after padding, so an overlong status
// only pushes that row's title column start to the right, it never shifts
// another row's columns out of line.
const statusColumnWidth = 11

// item adapts a *beads.Issue to bubbles/v2/list's Item interface.
type item struct {
	issue *beads.Issue
}

// FilterValue is what bubbles/v2/list matches an in-progress filter against.
// bv's own filtering lives in the root model (Filter.Apply), not here — this
// only satisfies the interface bubbles requires of every item.
func (i item) FilterValue() string {
	return i.issue.ID + " " + i.issue.Title
}

// delegate renders one row per issue: a type glyph, the padded id, priority,
// status, the truncated title and, space permitting, labels.
type delegate struct {
	theme   theme.Theme
	idWidth int
}

// newDelegate builds a delegate sized to the widest id in snap, so every row
// in the same snapshot lines its title column up at the same column.
func newDelegate(th theme.Theme, snap *beads.Snapshot) delegate {
	idWidth := 0
	if snap != nil {
		for _, issue := range snap.Issues() {
			// ansi.StringWidth, not len: len counts bytes, but the %-*s that
			// pads the id column counts runes, so a multi-byte id would
			// measure wider here than it actually pads to.
			idWidth = max(idWidth, ansi.StringWidth(issue.ID))
		}
	}

	return delegate{theme: th, idWidth: idWidth}
}

// Render writes exactly one line for the issue at index — never more, since
// bubbles/v2/list positions rows by a fixed Height and an embedded newline
// would shift every row below it.
func (d delegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(item)
	if !ok {
		return
	}
	issue := it.issue

	style := d.theme.Base
	if index == m.Index() {
		style = d.theme.Selected
	} else if issue.Status.IsTerminal() {
		style = d.theme.Muted
	}

	row := d.row(issue, m.Width())
	_, _ = fmt.Fprint(w, style.Render(row))
}

// Height reports one line per row; the flat list never wraps a row.
func (d delegate) Height() int { return 1 }

// Spacing reports no gap between rows, matching the previous tree's density.
func (d delegate) Spacing() int { return 0 }

// Update does nothing: this delegate has no per-item interaction, unlike a
// delegate that opens an editor or toggles a checkbox in place.
func (d delegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// row composes the fixed-width columns, then truncates the title to whatever
// width is left, and appends labels only if enough room remains. The
// finished row is padded back out to width: lipgloss.JoinHorizontal (in
// internal/tui's joinPanes) positions the neighbouring detail pane by this
// row's own natural width, so a short title that leaves the row shorter than
// its assigned column would otherwise waste that much of the terminal and
// let the detail pane creep left into what should have been the list's own
// space.
func (d delegate) row(issue *beads.Issue, width int) string {
	glyph := d.theme.TypeGlyph(issue.IssueType)
	id := fmt.Sprintf("%-*s", d.idWidth, uitext.Sanitize(issue.ID))
	priority := issue.Priority.Label()
	status := issue.Status.Display()

	prefix := fmt.Sprintf("%s %s %s %-*s ", glyph, id, priority, statusColumnWidth, status)
	remaining := width - ansi.StringWidth(prefix)
	if remaining <= 0 {
		return uitext.Truncate(prefix, width)
	}

	title, labels, labelsWidth := d.titleAndLabels(issue, remaining)
	line := prefix + title
	if labelsWidth > 0 {
		// Left as plain text rather than styled with theme.Muted: the whole
		// row is wrapped in one style by Render below (Base, Muted or
		// Selected). A styled span in the middle of a row would emit its own
		// reset code partway through the line, which drops the outer style
		// for everything after it — so the row is styled once, as a unit,
		// rather than column by column.
		line += "  " + labels
	}

	return padToWidth(line, width)
}

// padToWidth right-pads s with spaces up to width cells. s is already
// truncated to at most width by the callers above, so this only ever adds
// space, never cuts — ansi.StringWidth, not len, since the row can carry
// double-width CJK glyphs that a byte count would overstate.
func padToWidth(s string, width int) string {
	if pad := width - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}

	return s
}

// titleAndLabels sanitises the title and joined labels, then truncates the
// title to fit remaining, reserving labelsMinWidth cells for labels when the
// issue has any and there is room. Sanitising happens here, before Truncate:
// ansi.StringWidth treats a newline as zero-width and ansi.Truncate never
// removes one, so an un-sanitised title containing "\n" would slip through
// both untouched and render as a second physical row, shifting every row
// below it — control characters must never reach Truncate.
func (d delegate) titleAndLabels(issue *beads.Issue, remaining int) (title, labels string, labelsWidth int) {
	sanitizedTitle := uitext.Sanitize(issue.Title)
	if len(issue.Labels) == 0 || remaining < labelsMinWidth {
		return uitext.Truncate(sanitizedTitle, remaining), "", 0
	}

	joined := uitext.Sanitize(strings.Join(issue.Labels, ","))
	labelsWidth = ansi.StringWidth(joined) + 2 // separating spaces
	titleWidth := remaining - labelsWidth
	if titleWidth < labelsMinWidth {
		return uitext.Truncate(sanitizedTitle, remaining), "", 0
	}

	return uitext.Truncate(sanitizedTitle, titleWidth), joined, labelsWidth
}
