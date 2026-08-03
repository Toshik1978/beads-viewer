// Package cardfmt renders one issue as a bordered card: the id-and-priority
// line, the title, and — when expanded — assignee, labels and a blocked-by or
// readiness line.
//
// It exists for the same reason rowfmt does. The board and the dependency view
// both draw issues as cards, and views are peers that must not import each
// other; without a shared package the second one to want a card would either
// duplicate the first's rendering or import it directly, and the peer rule
// would be broken by the very package that follows it in the architecture
// diagram.
//
// Like rowfmt, it deliberately owns no width ladder — which column a view
// sacrifices first as its pane narrows differs per view, so only the card
// itself lives here.
package cardfmt

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

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

// Render draws one card: a bordered box holding the id/priority line, the
// title, and — when expanded — the assignee/labels and blocked-by lines. width
// is the card's total width including its border.
func Render(
	th theme.Theme, snap *beads.Snapshot, issue *beads.Issue, width int, selected, expanded bool,
) string {
	if width < minCardContentWidth+2 {
		if width <= 0 {
			return ""
		}

		return uitext.Truncate(uitext.Sanitize(issue.ID), width)
	}

	contentWidth := width - 2
	lines := contentLines(th, snap, issue, contentWidth, expanded)
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

// Height returns a card's total rendered row count, border included.
func Height(expanded bool) int {
	lines := collapsedLines
	if expanded {
		lines += expandedExtraLines
	}

	return lines + cardBorderRows
}

// contentLines builds a card's inner text, one already-truncated line
// per row, before the border style pads and wraps it.
func contentLines(th theme.Theme, snap *beads.Snapshot, issue *beads.Issue, width int, expanded bool) []string {
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
