package detail

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// dateFormat is used for every timestamp this pane shows, comments included:
// renderComments (below) already orders them oldest first, so a bare date is
// enough resolution to tell same-day comments apart in sequence.
const dateFormat = "2006-01-02"

// wrapIndent is the hanging indent wrapLine applies to every continuation
// line, so a wrapped metadata row, dependency entry or comment still reads
// as one entry continuing rather than as several unrelated lines stacked on
// top of each other.
const wrapIndent = "  "

// proseSection pairs a heading with the markdown body it introduces.
type proseSection struct {
	heading string
	body    string
}

// composeContent builds the pane's full content for the current issue.
func (m *Model) composeContent() string {
	if m.issue == nil {
		return m.theme.Muted.Render(noIssuePlaceholder)
	}

	sections := []string{m.renderHeader(), m.renderMeta()}
	for _, section := range []string{m.renderLabels(), m.renderDeps(), m.renderProse(), m.renderComments()} {
		if section != "" {
			sections = append(sections, section)
		}
	}

	return strings.Join(sections, "\n\n")
}

// renderHeader renders the title, wrapped to the pane width and styled by
// priority so the most urgent issues read as such at a glance.
func (m *Model) renderHeader() string {
	wrapped := ansi.Wordwrap(uitext.Sanitize(m.issue.Title), max(m.width, 1), "")

	return m.theme.Priority(m.issue.Priority).Render(wrapped)
}

// renderMeta renders the fixed metadata line: id, status, priority, type,
// assignee, created, updated and closed, in that order, each field present
// only when it has something to say.
//
// Wrapped rather than truncated: at ordinary pane widths this line does not
// fit on one row once every optional field is present, and truncateLines
// used to delete whatever ran past the edge — silently dropping "updated",
// "closed", or both, which is metadata the brief requires. wrapLine keeps
// every field; it just spills onto more than one line.
func (m *Model) renderMeta() string {
	issue := m.issue
	fields := []string{
		uitext.Sanitize(issue.ID), issue.Status.Display(), issue.Priority.Label(), issue.IssueType.Display(),
	}

	if issue.Assignee != "" {
		fields = append(fields, uitext.Sanitize(issue.Assignee))
	}
	if !issue.CreatedAt.IsZero() {
		fields = append(fields, "created "+issue.CreatedAt.Format(dateFormat))
	}
	if !issue.UpdatedAt.IsZero() {
		fields = append(fields, "updated "+issue.UpdatedAt.Format(dateFormat))
	}
	if issue.ClosedAt != nil {
		fields = append(fields, "closed "+issue.ClosedAt.Format(dateFormat))
	}

	return m.theme.Muted.Render(wrapLine(strings.Join(fields, " · "), m.width))
}

// renderLabels renders the issue's labels, or "" when it has none.
func (m *Model) renderLabels() string {
	if len(m.issue.Labels) == 0 {
		return ""
	}

	labels := make([]string, len(m.issue.Labels))
	for i, label := range m.issue.Labels {
		labels[i] = uitext.Sanitize(label)
	}

	return m.theme.Accent.Render(wrapLine(strings.Join(labels, ", "), m.width))
}

// renderDeps composes the blocked-by, blocks, parent and children sections,
// omitting whichever do not apply to this issue.
func (m *Model) renderDeps() string {
	var parts []string
	for _, section := range []string{m.renderBlockedBy(), m.renderBlocks(), m.renderParent(), m.renderChildren()} {
		if section != "" {
			parts = append(parts, section)
		}
	}

	return strings.Join(parts, "\n")
}

// renderProse renders the four prose fields as markdown, each under its own
// heading and each shown only when non-empty — an empty section would be a
// heading over nothing.
func (m *Model) renderProse() string {
	sections := []proseSection{
		{"Description", m.issue.Description},
		{"Design", m.issue.Design},
		{"Acceptance Criteria", m.issue.AcceptanceCriteria},
		{"Notes", m.issue.Notes},
	}

	var parts []string
	for _, section := range sections {
		if strings.TrimSpace(section.body) == "" {
			continue
		}
		parts = append(parts, m.theme.Title.Render(section.heading)+"\n"+m.markdown(section.body))
	}

	return strings.Join(parts, "\n\n")
}

// renderComments renders every comment oldest first, each with its author
// and timestamp.
func (m *Model) renderComments() string {
	if len(m.issue.Comments) == 0 {
		return ""
	}

	comments := slices.Clone(m.issue.Comments)
	slices.SortStableFunc(comments, func(a, b beads.Comment) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	lines := []string{m.theme.Title.Render("Comments") + ":"}
	for _, comment := range comments {
		lines = append(
			lines,
			wrapLine(fmt.Sprintf("%s — %s", comment.Author, comment.CreatedAt.Format(dateFormat)), m.width),
			wrapLine(uitext.Sanitize(comment.Text), m.width),
		)
	}

	return strings.Join(lines, "\n")
}

// renderBlockedBy states every reason this issue is blocked, not only the ones
// Snapshot.Blockers can name.
//
// The section used to be "the live blockers, or else a dangling reference",
// which read the empty Blockers list as proof of a missing issue. It is not:
// Blockers reports this issue's own dependency rows, so it is also empty for
// an issue blocked through its parent chain and for an epic held by its own
// open children — both of which would then be reported as pointing at an issue
// "not present in this workspace" while the real cause sat two rows further
// down the same pane. Each cause is asked for separately and stated on its own
// line, so no cause has to stand in for another and several can be true at
// once. When none is, the section is omitted entirely.
func (m *Model) renderBlockedBy() string {
	entries := m.blockedByEntries()
	if len(entries) == 0 {
		return ""
	}

	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, m.theme.Title.Render("Blocked by")+":")
	for _, entry := range entries {
		lines = append(lines, wrapLine("  "+entry, m.width))
	}

	return strings.Join(lines, "\n")
}

// blockedByEntries collects one line per reason, live blockers first: those
// are the edges the issue itself declares, and the rest qualify them.
func (m *Model) blockedByEntries() []string {
	var entries []string

	for _, blocker := range m.snapshot.Blockers(m.issue.ID) {
		entries = append(entries, describeIssue(blocker))
	}
	if ancestor, ok := m.snapshot.BlockedAncestor(m.issue.ID); ok {
		entries = append(entries, describeIssue(ancestor)+" — "+inheritedBlockerNote)
	}
	for _, missing := range m.snapshot.DanglingBlockers(m.issue.ID) {
		entries = append(entries, uitext.Sanitize(missing)+" — "+danglingBlockerNote)
	}
	if m.snapshot.BlockedByOpenChild(m.issue.ID) {
		entries = append(entries, openChildBlockerNote)
	}

	return entries
}

// describeIssue renders the "id title (Status)" form every named blocker and
// ancestor shares, so the two read as the same kind of reference.
func describeIssue(issue *beads.Issue) string {
	return fmt.Sprintf(
		"%s %s (%s)", uitext.Sanitize(issue.ID), uitext.Sanitize(issue.Title), issue.Status.Display(),
	)
}

// renderBlocks names every issue this one blocks. Snapshot has no direct
// index for "what does id block" — only for the reverse, Blockers — so this
// scans the dependency edges other issues declare against this one.
func (m *Model) renderBlocks() string {
	var blocked []*beads.Issue
	for _, other := range m.snapshot.Issues() {
		if other.ID != m.issue.ID && dependsOn(other, m.issue.ID) {
			blocked = append(blocked, other)
		}
	}
	if len(blocked) == 0 {
		return ""
	}

	lines := []string{m.theme.Title.Render("Blocks") + ":"}
	for _, other := range blocked {
		lines = append(
			lines, wrapLine(fmt.Sprintf("  %s %s", uitext.Sanitize(other.ID), uitext.Sanitize(other.Title)), m.width),
		)
	}

	return strings.Join(lines, "\n")
}

// renderParent names this issue's parent, if it declares one that exists.
func (m *Model) renderParent() string {
	parent, ok := m.snapshot.Parent(m.issue.ID)
	if !ok {
		return ""
	}

	line := fmt.Sprintf(
		"%s: %s %s", m.theme.Title.Render("Parent"), uitext.Sanitize(parent.ID), uitext.Sanitize(parent.Title),
	)

	return wrapLine(line, m.width)
}

// renderChildren names this issue's children, if it has any.
func (m *Model) renderChildren() string {
	children := m.snapshot.Children(m.issue.ID)
	if len(children) == 0 {
		return ""
	}

	lines := []string{m.theme.Title.Render("Children") + ":"}
	for _, child := range children {
		lines = append(
			lines, wrapLine(fmt.Sprintf("  %s %s", uitext.Sanitize(child.ID), uitext.Sanitize(child.Title)), m.width),
		)
	}

	return strings.Join(lines, "\n")
}

// dependsOn reports whether issue declares a blocking dependency on id.
func dependsOn(issue *beads.Issue, id string) bool {
	for _, dep := range issue.Dependencies {
		if dep.Type.Blocks() && dep.DependsOnID == id {
			return true
		}
	}

	return false
}

// wrapLine wraps s to width using ansi.Wordwrap — which breaks cleanly on
// the " · " separator renderMeta joins with, as well as on plain spaces —
// and hangs every continuation line by wrapIndent, so a metadata row,
// dependency entry or comment that needs more than one line still reads as a
// single continued entry rather than several unrelated lines.
//
// This is the fix for the metadata, dependency and comment lines that used
// to go through truncateLines alone: at ordinary pane widths (measured: the
// 70-cell DetailWidth a 120-column terminal actually yields, and 60, 50, 40
// below it) the unwrapped metadata line ran past the edge before "updated"
// or "closed" ever appeared, and truncateLines simply deleted the overhang —
// silently dropping metadata and blocker status the brief requires. Wrapping
// before truncateLines runs keeps every field; truncateLines still runs
// afterward as the documented backstop, but in normal operation it now has
// nothing left to cut.
//
// Continuation lines are wrapped narrower, by len(wrapIndent), specifically
// so that adding the indent back cannot push them past width — the same
// class of overflow minWrap's floor exists to avoid on the glamour side of
// this pane.
func wrapLine(s string, width int) string {
	if width <= len(wrapIndent) {
		return ansi.Wordwrap(s, max(width, 0), "")
	}

	lines := strings.Split(ansi.Wordwrap(s, width-len(wrapIndent), ""), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = wrapIndent + lines[i]
	}

	return strings.Join(lines, "\n")
}

// truncateLines backstops every line of s to at most width cells.
//
// glamour's WithWordWrap silently stops wrapping below a small width (see
// minWrap in detail.go): measured against glamour v2.0.1, WithWordWrap(4),
// (3), (1), (0) and (-1) all emit their paragraph on one unwrapped line, with
// no error and no signal. Compute legitimately returns a small or zero
// DetailWidth during startup and mid-resize, so the floor in SetSize is
// reachable in normal use, and an unclamped line wraps in the terminal and
// shifts every row below it — the same corruption listview's truncation
// exists to prevent, through a different door. Clamping the wrap width alone
// is not sufficient: when the floor exceeds the pane width, the clamp is
// what causes the overflow, and only this per-line truncation holds the
// invariant — which is why it runs over the whole composed section, not only
// the markdown-rendered spans within it.
//
// wrapLine above is the primary defence for the metadata, dependency and
// comment lines this package builds itself, so in normal operation this
// backstop is a no-op for them; it still matters for glamour's own output,
// which this package does not control the wrapping of line by line. Its call
// site in refreshContent is pinned directly by
// TestRefreshContentTruncatesTheStoredViewportContent in
// detail_internal_test.go — bubbles/v2/viewport clips every rendered line to
// its own width regardless, but that clipping is reversible (the content
// stays in the model, restored the moment the pane widens); this function's
// truncation is not, which is exactly why the call site needs its own test
// rather than relying on the viewport's render-time behaviour to stand in
// for it.
func truncateLines(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}

	return strings.Join(lines, "\n")
}
