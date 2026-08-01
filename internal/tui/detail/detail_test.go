package detail_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/detail"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// TestDetail is this package's single entry point; every real assertion
// lives on a suite method below, or — for the fallback path glamour itself
// never takes — on detail.InternalSuite in detail_internal_test.go.
func TestDetail(t *testing.T) {
	suite.Run(t, new(detailTestSuite))
	suite.Run(t, new(detail.InternalSuite))
}

type detailTestSuite struct {
	suite.Suite
}

func (s *detailTestSuite) newModel(w, h int) *detail.Model {
	m, err := detail.New(slog.New(slog.DiscardHandler), theme.New(config.ThemeDark, theme.BackgroundDark))
	s.Require().NoError(err)
	m.SetSize(w, h)

	return m
}

// findIssue is a test-local stand-in for the exported Snapshot.ByID method
// I9 removed (zero non-test callers): every call site below only needs the
// snapshot's own copy of a fixture issue by id, not any ByID-specific
// behaviour.
func findIssue(snap *beads.Snapshot, id string) *beads.Issue {
	for _, issue := range snap.Issues() {
		if issue.ID == id {
			return issue
		}
	}

	return nil
}

func (s *detailTestSuite) TestRendersMetadata() {
	issue := &beads.Issue{
		ID: "bv-1", Title: "Add tree view", Status: beads.StatusInProgress,
		Priority: beads.PriorityHigh, IssueType: beads.TypeFeature,
		Assignee: "anton", Labels: []string{"ui", "tree"},
		CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
	}
	m := s.newModel(60, 30)
	m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))

	out := m.View()
	for _, want := range []string{"bv-1", "Add tree view", "In Progress", "P1", "anton", "ui", "tree"} {
		s.Contains(out, want)
	}
}

// AMENDMENT — the raw View() output cannot satisfy multi-word Contains
// checks; strip ANSI first.
//
// Measured against glamour v2.0.1: WithStandardStyle("dark") wraps every
// word of rendered markdown in its own SGR run, independent of word wrap —
// rendering "a note" through glamour.NewTermRenderer(WithStandardStyle(
// "dark"), WithWordWrap(0)) yields "\x1b[38;5;252ma\x1b[m\x1b[38;5;252m
// note\x1b[m", so strings.Contains(out, "a note") is false no matter how the
// surrounding pane is implemented — the escape codes between words are
// bytes the plain substring can never match through. This is the same
// reason golden view tests elsewhere in this codebase compare the
// ANSI-stripped frame rather than the raw one. Single-word checks
// ("description" alone) would have passed either way, which is exactly why
// they cannot stand in for the multi-word ones.
func (s *detailTestSuite) TestRendersProseSections() {
	issue := &beads.Issue{
		ID: "bv-1", Title: "T",
		Description:        "The **description** body.",
		Design:             "## Design heading",
		AcceptanceCriteria: "- criterion one",
		Notes:              "a note",
	}
	m := s.newModel(70, 40)
	m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))

	out := ansi.Strip(m.View())
	s.Contains(out, "description")
	s.Contains(out, "Design heading")
	s.Contains(out, "criterion one")
	s.Contains(out, "a note")
}

// AMENDMENT — a positive assertion, not only the two NotContains checks.
//
// Without it, a View() mutated to always return "" would still pass: an
// empty string contains neither "Acceptance Criteria" nor "Notes". The
// s.Contains below is what makes the omission mean something.
func (s *detailTestSuite) TestOmitsEmptySections() {
	issue := &beads.Issue{ID: "bv-1", Title: "T", Description: "only this"}
	m := s.newModel(70, 40)
	m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))

	// ansi.Strip: glamour wraps every word of rendered markdown in its own
	// SGR run (see TestRendersProseSections' amendment above), so
	// strings.Contains(out, "only this") is false against the raw, styled
	// output no matter how the surrounding pane is implemented.
	out := ansi.Strip(m.View())
	s.Contains(out, "only this", "the one section that IS present must actually render")
	s.NotContains(out, "Acceptance Criteria", "an empty section is a wasted heading")
	s.NotContains(out, "Notes")
}

// AMENDMENT — the status was never actually asserted.
//
// s.Contains(out, "bv-2") and s.Contains(out, "the blocker") both hold for
// any rendering that names the blocker at all; deleting the "(%s)" status
// suffix in renderBlockedBy (render.go) left this test green even though the
// brief requires the blocker's status specifically. The third assertion
// below is what actually tests the name this test carries.
func (s *detailTestSuite) TestNamesBlockersWithTheirStatus() {
	issues := []beads.Issue{
		{
			ID: "bv-1", Title: "blocked", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "bv-1", DependsOnID: "bv-2", Type: beads.DepBlocks},
			},
		},
		{ID: "bv-2", Title: "the blocker", Status: beads.StatusOpen},
	}
	snap := beads.NewSnapshot(issues)
	target := findIssue(snap, "bv-1")

	m := s.newModel(70, 40)
	m.SetIssue(target, snap)

	out := m.View()
	s.Contains(out, "bv-2")
	s.Contains(out, "the blocker")
	s.Contains(out, "(Open)", "the blocker's own status must be named, not just its id and title")
}

// TestRendersComments pins the brief's three comment requirements together:
// every comment present, oldest first, and each with its author and
// timestamp — none of which TestRendersMetadata or TestRendersProseSections
// touches, since neither fixture carries a comment at all.
func (s *detailTestSuite) TestRendersComments() {
	issue := &beads.Issue{
		ID: "bv-1", Title: "T",
		Comments: []beads.Comment{
			{
				Author:    "later-author",
				Text:      "the second comment",
				CreatedAt: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
			},
			{Author: "first-author", Text: "the first comment", CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)},
		},
	}
	m := s.newModel(70, 40)
	m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))

	out := ansi.Strip(m.View())
	s.Contains(out, "first-author")
	s.Contains(out, "the first comment")
	s.Contains(out, "later-author")
	s.Contains(out, "the second comment")
	s.Contains(out, "2026-07-01", "each comment's own timestamp must render")
	s.Contains(out, "2026-07-02")

	firstIdx := strings.Index(out, "the first comment")
	secondIdx := strings.Index(out, "the second comment")
	s.Require().NotEqual(-1, firstIdx)
	s.Require().NotEqual(-1, secondIdx)
	s.Less(firstIdx, secondIdx, "comments must render oldest first regardless of input order")
}

// TestRendersBlocks pins the "Blocks" section — what this issue blocks,
// the reverse direction from TestNamesBlockersWithTheirStatus' "blocked by".
func (s *detailTestSuite) TestRendersBlocks() {
	issues := []beads.Issue{
		{ID: "bv-1", Title: "the blocker", Status: beads.StatusOpen},
		{
			ID: "bv-2", Title: "downstream issue", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "bv-2", DependsOnID: "bv-1", Type: beads.DepBlocks},
			},
		},
	}
	snap := beads.NewSnapshot(issues)
	target := findIssue(snap, "bv-1")

	m := s.newModel(70, 40)
	m.SetIssue(target, snap)

	out := m.View()
	s.Contains(out, "Blocks")
	s.Contains(out, "bv-2")
	s.Contains(out, "downstream issue")
}

// TestRendersParent pins the "Parent" section.
func (s *detailTestSuite) TestRendersParent() {
	issues := []beads.Issue{
		{ID: "bv-1", Title: "the parent", Status: beads.StatusOpen},
		{
			ID: "bv-2", Title: "the child", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "bv-2", DependsOnID: "bv-1", Type: beads.DepParentChild},
			},
		},
	}
	snap := beads.NewSnapshot(issues)
	target := findIssue(snap, "bv-2")

	m := s.newModel(70, 40)
	m.SetIssue(target, snap)

	out := m.View()
	s.Contains(out, "Parent")
	s.Contains(out, "bv-1")
	s.Contains(out, "the parent")
}

// TestRendersChildren pins the "Children" section.
func (s *detailTestSuite) TestRendersChildren() {
	issues := []beads.Issue{
		{ID: "bv-1", Title: "the parent", Status: beads.StatusOpen},
		{
			ID: "bv-2", Title: "the child", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "bv-2", DependsOnID: "bv-1", Type: beads.DepParentChild},
			},
		},
	}
	snap := beads.NewSnapshot(issues)
	target := findIssue(snap, "bv-1")

	m := s.newModel(70, 40)
	m.SetIssue(target, snap)

	out := m.View()
	s.Contains(out, "Children")
	s.Contains(out, "bv-2")
	s.Contains(out, "the child")
}

func (s *detailTestSuite) TestExplainsADanglingBlocker() {
	// IsBlocked is true but Blockers is empty. Rendering nothing here reads as
	// a bug — the issue is plainly blocked and the pane says nothing.
	issues := []beads.Issue{
		{
			ID: "bv-1", Title: "blocked", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "bv-1", DependsOnID: "gone", Type: beads.DepBlocks},
			},
		},
	}
	snap := beads.NewSnapshot(issues)
	target := findIssue(snap, "bv-1")

	m := s.newModel(70, 40)
	m.SetIssue(target, snap)

	s.Contains(m.View(), "not present in this workspace")
}

func (s *detailTestSuite) TestNilIssueRendersAPlaceholder() {
	m := s.newModel(60, 30)
	m.SetIssue(nil, beads.NewSnapshot(nil))

	out := m.View()
	s.NotEmpty(strings.TrimSpace(out))
	s.NotPanics(func() { _ = m.View() })
}

// TestBelowMinPaneWidthSkipsMarkdownEntirely pins minPaneWidth's actual
// purpose, which TestNoLineExceedsTheWidth cannot: bubbles/v2/viewport
// clips every line to its own width regardless of what it is given, so the
// width invariant alone holds even if the placeholder gate were deleted —
// confirmed by temporarily removing both minPaneWidth checks in detail.go
// and rerunning the suite, which stayed green. What the gate actually buys
// is not attempting markdown at an illegible width at all, so this asserts
// the issue's own content is absent below the threshold, not merely that
// nothing overflows.
func (s *detailTestSuite) TestBelowMinPaneWidthSkipsMarkdownEntirely() {
	issue := &beads.Issue{
		ID: "bv-1", Title: "should not appear", Description: "nor should this",
	}
	m := s.newModel(4, 40)
	m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))

	out := m.View()
	s.NotContains(out, "bv-1", "too narrow for markdown must not attempt to render real content")
	s.NotContains(out, "should not appear")
}

// AMENDMENT — this test's original name was a lie, and the lie mattered.
//
// It was called TestMalformedMarkdownFallsBackToRawText and was the only
// coverage of the fallback path. Measured against glamour v2.0.1: rendering
// "| broken | table\n|---\n| a | b | c |\n\n```\nunclosed fence\n" returns a
// nil error and 119 bytes of output. goldmark does not reject malformed
// markdown, it degrades. So the fallback never fired, the test passed through
// the success path, and it could not have failed if the fallback were deleted
// outright.
//
// The content-survival assertion is still worth keeping — it just tests
// something else, so it is named for what it does. The fallback itself is
// covered separately, below, through a seam.
func (s *detailTestSuite) TestMalformedMarkdownStillShowsItsContent() {
	issue := &beads.Issue{
		ID: "bv-1", Title: "T",
		Description: "| broken | table\n|---\n| unterminated",
	}
	m := s.newModel(70, 40)
	m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))

	s.Contains(m.View(), "unterminated",
		"a formatting failure must cost formatting, not content")
}

// AMENDMENT — the widths tested must include the ones that break glamour.
// 50 alone passes trivially. The narrow cases are where WithWordWrap stops
// wrapping and where the pane width falls under the placeholder threshold.
//
// AMENDMENT 2 — no lower bound. Every case here asserted only
// s.LessOrEqual(lipgloss.Width(line), width), which a View() mutated to
// always return "" satisfies vacuously at every one of the six widths. The
// NotEmpty below closes that for every width; the Contains below it goes
// further at widths >= detail's minPaneWidth (12, mirrored here as a literal
// since it is unexported), where real content — not just the narrow-pane
// placeholder — must actually appear.
func (s *detailTestSuite) TestNoLineExceedsTheWidth() {
	const minPaneWidth = 12 // mirrors detail.minPaneWidth, unexported to this package

	issue := &beads.Issue{
		ID: "bv-1", Title: strings.Repeat("very long title ", 20),
		Description: strings.Repeat("word ", 200) +
			"\n\n| a very wide table header | and another column |\n" +
			"|---|---|\n| with content that will not fit | more content |\n",
	}
	for _, width := range []int{80, 50, 30, 12, 4, 1} {
		s.Run("", func() {
			m := s.newModel(width, 40)
			m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))

			out := m.View()
			s.NotEmpty(strings.TrimSpace(out), "a mutated View returning \"\" must not pass this test vacuously")
			if width >= minPaneWidth {
				s.Contains(out, "bv-1", "the issue's own id must actually render at a usable width")
			}
			for line := range strings.SplitSeq(out, "\n") {
				s.LessOrEqual(lipgloss.Width(line), width, "line: %q", line)
			}
		})
	}
}

func (s *detailTestSuite) TestDegenerateSizesDoNotPanic() {
	issue := &beads.Issue{ID: "bv-1", Title: "T", Description: "body"}
	for _, size := range [][2]int{{0, 0}, {0, 10}, {10, 0}, {1, 1}} {
		s.Run("", func() {
			s.NotPanics(func() {
				m := s.newModel(size[0], size[1])
				m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))
				_ = m.View()
			})
		})
	}
}

// TestRendererIsBuiltOnce was deleted — it could not fail.
//
// Measured: 200 glamour.NewTermRenderer constructions cost about 4.89ms
// (24.4us each) against this test's 2-second budget. With a real per-SetIssue
// rebuild added to prove the point, this test still passed in 0.02s while
// TestSetIssueNeverReplacesTheRenderer (detail_internal_test.go) went red, as
// it should have. No timing threshold is discriminating on hardware this
// fast, so the structural test is the one kept — it asserts the renderer's
// identity is untouched across repeated SetIssue calls, which holds
// regardless of how fast reconstruction happens to be.
