package boardview

import (
	"fmt"

	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// CatchallMarker prefixes a catch-all column's header title.
//
// Column.Catchall — not Title — is the only reliable signal that a column is
// the catch-all rather than a real column that happens to share its title
// (a project can have an assignee literally named "Unassigned"), so this is
// the one place that bool actually changes what gets rendered. Exported so
// a caller (and this package's own tests) can assert on the exact marker
// rather than guessing at the header's shape.
const CatchallMarker = "» "

// columnHeaderLines renders a column's title-and-count line and a stats
// summary line, both clipped to width and then padded back out to it.
//
// focused picks a bolder, accent-coloured title style so the cursor's
// column is identifiable even before any card in it is examined — th.Title
// is already Bold (theme.go), so an unfocused header without that padding
// used to be the only one left out of the emphasis it was meant to carry
// (F13); Bold(true) here restores it while accent's colour still tells the
// two apart.
//
// The Width() call is what keeps an empty column exactly as wide as a
// populated one: JoinHorizontal pads each joined block only to its own
// widest *line*, and a truncated-but-unpadded header line on a column with
// no card rows below it was previously the widest line in that block,
// leaving it visibly narrower than any neighbour with cards (F8).
func columnHeaderLines(th theme.Theme, col Column, width int, focused bool) []string {
	title := uitext.Sanitize(col.Title)
	if col.Catchall {
		title = CatchallMarker + title
	}
	titleLine := uitext.Truncate(fmt.Sprintf("%s (%d)", title, col.Stats.Count), width)

	titleStyle := th.Title
	if focused {
		titleStyle = th.Accent.Bold(true)
	}

	statsText := fmt.Sprintf("%d ready · %d blocked", col.Stats.Ready, col.Stats.Blocked)
	if col.Stats.OldestDays > 0 {
		statsText += fmt.Sprintf(" · %dd", col.Stats.OldestDays)
	}
	statsLine := uitext.Truncate(statsText, width)

	return []string{
		titleStyle.Width(width).Render(titleLine),
		th.Muted.Width(width).Render(statsLine),
	}
}

// moreLabel renders the "+N more" text for a column's own hidden-cards
// indicator, shown when renderColumn cannot fit every card in its height
// budget.
func moreLabel(hidden int) string {
	return fmt.Sprintf("+%d more", hidden)
}

// renderMarker renders one directional hidden-columns indicator for the
// board-level scroll window (boardview.go's followCursor and
// visibleLayout): "<N" when left is true, for columns hidden to the
// cursor's left, or "N>" for columns hidden to its right.
//
// Rendered as width blank cells rather than omitted when count is 0, so the
// reserved slot's width never depends on which side currently has
// something hidden — see visibleLayout's fixed reserve, which this must
// match to hold the "no line exceeds the width" invariant. A marker
// pointing the wrong way is worse than none at all (a scrolled-right board
// that still says "more" only to the right lies about where the hidden
// columns are), which is why the direction is a parameter here rather than
// inferred by the caller from whichever count happens to be nonzero.
func renderMarker(th theme.Theme, count, width int, left bool) string {
	if width <= 0 {
		return ""
	}

	var text string
	if count > 0 {
		text = fmt.Sprintf("%d>", count)
		if left {
			text = fmt.Sprintf("<%d", count)
		}
		text = uitext.Truncate(text, width)
	}

	// Width() pads count == 0's empty text out to width the same way it
	// pads a short digit count: the rendered slot is always exactly width
	// cells, matching visibleLayout's fixed reserve regardless of how many
	// digits count has or whether it is 0 at all.
	return th.Muted.Width(width).Render(text)
}
