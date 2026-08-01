package listview

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// labelsMinWidth is the fewest cells that must remain after the fixed
// columns and the truncated title before labels are worth rendering; below
// it a label would itself need truncating past legibility.
const labelsMinWidth = 12

// statusColumnWidth is the widest label beads.Status.Display returns for a
// status br defines: "In Progress", 11 cells. A custom status longer than
// this is never truncated or misaligned — compose measures the status
// column's actual rendered width with ansi.StringWidth after padding, so an
// overlong status only pushes that row's title column start to the right, it
// never shifts another row's columns out of line.
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
//
// The two branches are not interchangeable: see rowfmt.Columns.Styled for why
// a selected row must be styled as a single unit and every other row must
// not.
func (d delegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(item)
	if !ok {
		return
	}

	columns := d.compose(it.issue, m.Width())
	if index == m.Index() {
		_, _ = fmt.Fprint(w, d.theme.Selected.Render(columns.Plain(m.Width())))

		return
	}

	_, _ = fmt.Fprint(w, columns.Styled(d.theme, it.issue, m.Width()))
}

// Height reports one line per row; the flat list never wraps a row.
func (d delegate) Height() int { return 1 }

// Spacing reports no gap between rows, matching the previous tree's density.
func (d delegate) Spacing() int { return 0 }

// Update does nothing: this delegate has no per-item interaction, unlike a
// delegate that opens an editor or toggles a checkbox in place.
func (d delegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
