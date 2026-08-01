package boardview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
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

// minCardContentWidth is the fewest content cells (border excluded) a card
// is worth drawing bordered. Below it, renderCard falls back to a single
// unbordered, truncated line rather than asking lipgloss for a negative
// content width.
const minCardContentWidth = 1

// cardBorderRows is how many rows NormalBorder adds top and bottom.
const cardBorderRows = 2

// collapsedLines is the card content shown at all times: the id/priority
// line and the title.
const collapsedLines = 2

// expandedExtraLines is what ToggleExpand adds: assignee/labels and a
// blocked-by or readiness line.
const expandedExtraLines = 2

// cardHeight returns a card's total rendered row count, border included.
func cardHeight(expanded bool) int {
	lines := collapsedLines
	if expanded {
		lines += expandedExtraLines
	}

	return lines + cardBorderRows
}

// renderCard draws one card: a bordered box holding the id/priority line,
// the title, and — when expanded — the assignee/labels and blocked-by
// lines. width is the card's total width including its border.
func renderCard(
	th theme.Theme, snap *beads.Snapshot, issue *beads.Issue, width int, selected, expanded bool,
) string {
	if width < minCardContentWidth+2 {
		if width <= 0 {
			return ""
		}

		return uitext.Truncate(uitext.Sanitize(issue.ID), width)
	}

	contentWidth := width - 2
	lines := cardContentLines(th, snap, issue, contentWidth, expanded)
	text := strings.Join(lines, "\n")

	style := th.Base
	if selected {
		style = th.Selected
	}
	style = style.Width(width).Border(lipgloss.NormalBorder()).BorderForeground(th.Border.GetForeground())
	if selected {
		style = style.BorderForeground(th.Selected.GetForeground())
	}

	return style.Render(text)
}

// cardContentLines builds a card's inner text, one already-truncated line
// per row, before the border style pads and wraps it.
func cardContentLines(th theme.Theme, snap *beads.Snapshot, issue *beads.Issue, width int, expanded bool) []string {
	lines := make([]string, 0, collapsedLines+expandedExtraLines)
	lines = append(lines, headerLine(th, issue, width), uitext.Truncate(uitext.Sanitize(issue.Title), width))
	if !expanded {
		return lines
	}

	return append(lines, assigneeLine(issue, width), blockedByLine(snap, issue, width))
}

// headerLine renders the glyph, sanitised id and priority label, truncating
// the id first when the column is too narrow for all three — the priority
// label is what tells a reader whether a card is worth opening, so it is the
// last thing to give up room.
func headerLine(th theme.Theme, issue *beads.Issue, width int) string {
	glyph := th.TypeGlyph(issue.IssueType)
	priority := issue.Priority.Label()
	id := uitext.Sanitize(issue.ID)

	left := glyph + " " + id
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(priority)
	if gap < 1 {
		idWidth := max(width-ansi.StringWidth(priority)-ansi.StringWidth(glyph+" ")-1, 0)
		id = uitext.Truncate(id, idWidth)
		left = glyph + " " + id
		gap = max(width-ansi.StringWidth(left)-ansi.StringWidth(priority), 0)
	}

	return uitext.Truncate(left+strings.Repeat(" ", gap)+priority, width)
}

// assigneeLine renders the sanitised assignee and labels, or an em dash
// placeholder when neither is set — a blank row would otherwise read as a
// rendering gap rather than "nothing to show here".
func assigneeLine(issue *beads.Issue, width int) string {
	var parts []string
	if assignee := uitext.Sanitize(issue.Assignee); strings.TrimSpace(assignee) != "" {
		parts = append(parts, "@"+assignee)
	}
	if len(issue.Labels) > 0 {
		labels := make([]string, len(issue.Labels))
		for i, l := range issue.Labels {
			labels[i] = uitext.Sanitize(l)
		}
		parts = append(parts, strings.Join(labels, ","))
	}

	text := strings.Join(parts, "  ")
	if text == "" {
		text = "—"
	}

	return uitext.Truncate(text, width)
}

// blockedByLine names the issue's blockers when it is blocked, reports
// readiness otherwise, and falls back to the raw status for everything else
// — an issue that is neither ready nor blocked (closed, deferred, draft).
func blockedByLine(snap *beads.Snapshot, issue *beads.Issue, width int) string {
	if snap == nil {
		return uitext.Truncate(issue.Status.Display(), width)
	}

	text := issue.Status.Display()
	switch {
	case snap.IsBlocked(issue.ID):
		text = "blocked"
		if blockers := snap.Blockers(issue.ID); len(blockers) > 0 {
			ids := make([]string, len(blockers))
			for i, b := range blockers {
				ids[i] = uitext.Sanitize(b.ID)
			}
			text = "blocked by " + strings.Join(ids, ",")
		}
	case snap.IsReady(issue.ID):
		text = "ready"
	}

	return uitext.Truncate(text, width)
}

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
