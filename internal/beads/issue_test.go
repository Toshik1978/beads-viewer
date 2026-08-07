package beads_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

func TestBeads(t *testing.T) {
	suite.Run(t, new(issueTestSuite))
	suite.Run(t, new(jsonlTestSuite))
	suite.Run(t, new(workspaceTestSuite))
	suite.Run(t, new(snapshotTestSuite))
	suite.Run(t, new(deriveTestSuite))
	suite.Run(t, new(inheritTestSuite))
	suite.Run(t, new(filterTestSuite))
}

type issueTestSuite struct {
	suite.Suite
}

func (s *issueTestSuite) TestStatusIsTerminal() {
	cases := []struct {
		name   string
		status beads.Status
		want   bool
	}{
		{"open is not terminal", beads.StatusOpen, false},
		{"in_progress is not terminal", beads.StatusInProgress, false},
		{"blocked is not terminal", beads.StatusBlocked, false},
		{"closed is terminal", beads.StatusClosed, true},
		{"tombstone is terminal", beads.StatusTombstone, true},
		// An unknown status is not terminal. Treating it as terminal would
		// silently hide issues a future br version introduces.
		{"unknown is not terminal", beads.Status("triaged"), false},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, tc.status.IsTerminal())
		})
	}
}

func (s *issueTestSuite) TestStatusIsKnown() {
	s.True(beads.StatusOpen.IsKnown())
	s.True(beads.StatusPinned.IsKnown())
	s.False(beads.Status("triaged").IsKnown())
	s.False(beads.Status("").IsKnown())
}

func (s *issueTestSuite) TestStatusDisplayHumanisesUnderscores() {
	s.Equal("In Progress", beads.StatusInProgress.Display())
	s.Equal("Open", beads.StatusOpen.Display())
	// Unknown values are shown as-is rather than dropped or renamed.
	s.Equal("triaged", beads.Status("triaged").Display())
}

// TestStatusDisplaySanitizesUnknownValues is I6's fix pinned. Display's
// open-enum fallback for an unrecognised value hands the raw stored string
// straight to the renderer — that is the documented behaviour, so a control
// byte reaching it is the expected path, not an edge case. Left unsanitised,
// a "\n" measures zero-width to ansi.StringWidth and survives ansi.Truncate
// untouched, so it would shift every row below it in a fixed-height view —
// the identical bug class ids, titles and labels are already sanitised
// against, paid for a third time here.
func (s *issueTestSuite) TestStatusDisplaySanitizesUnknownValues() {
	s.Equal("triagedbad", beads.Status("triaged\nbad").Display())
	s.NotContains(beads.Status("red\x1b[41malert").Display(), "\x1b")
}

func (s *issueTestSuite) TestIssueTypeIsKnown() {
	s.True(beads.TypeEpic.IsKnown())
	s.False(beads.IssueType("spike").IsKnown())
}

func (s *issueTestSuite) TestIssueTypeDisplay() {
	s.Equal("Task", beads.TypeTask.Display())
	// Unknown ASCII values round-trip verbatim.
	s.Equal("spike", beads.IssueType("spike").Display())
	// Unknown non-ASCII values must round-trip byte-identical. Byte-slicing
	// the leading rune (the bug this test exists to catch) would corrupt
	// these rather than leave them unchanged. The Han-script fixture is
	// deliberate test data proving UTF-8 safety, not translatable UI text.
	for _, custom := range []string{"任务", "задача", "épic"} { //nolint:gosmopolitan // deliberate non-ASCII fixture, not UI copy
		s.Equal(custom, beads.IssueType(custom).Display())
	}
	s.Empty(beads.IssueType("").Display())
}

// TestIssueTypeDisplaySanitizesUnknownValues is IssueType's half of I6 — see
// TestStatusDisplaySanitizesUnknownValues above for why an unrecognised
// value reaching Display with a control byte still intact is the expected
// path, not an edge case.
func (s *issueTestSuite) TestIssueTypeDisplaySanitizesUnknownValues() {
	s.Equal("spikebad", beads.IssueType("spike\nbad").Display())
}

func (s *issueTestSuite) TestPriorityLabel() {
	cases := []struct {
		priority beads.Priority
		want     string
	}{
		{beads.PriorityCritical, "P0"},
		{beads.PriorityHigh, "P1"},
		{beads.PriorityMedium, "P2"},
		{beads.PriorityLow, "P3"},
		{beads.PriorityBacklog, "P4"},
	}
	for _, tc := range cases {
		s.Run(tc.want, func() {
			s.Equal(tc.want, tc.priority.Label())
		})
	}
}

func (s *issueTestSuite) TestPriorityClampsOutOfRange() {
	// br writes 0..4. A value outside that range would otherwise index past
	// the end of any colour or label table keyed on priority.
	s.Equal(beads.PriorityCritical, beads.Priority(-3).Clamp())
	s.Equal(beads.PriorityBacklog, beads.Priority(99).Clamp())
	s.Equal(beads.PriorityMedium, beads.PriorityMedium.Clamp())
}

func (s *issueTestSuite) TestDepTypeBlocks() {
	cases := []struct {
		name    string
		depType beads.DepType
		want    bool
	}{
		{"blocks blocks", beads.DepBlocks, true},
		{"conditional-blocks blocks", beads.DepConditionalBlocks, true},
		{"waits-for blocks", beads.DepWaitsFor, true},
		// This is the rule most easily got wrong. br's blocker query
		// (src/storage/sqlite.rs:7175) excludes parent-child, even though the
		// idx_dependencies_blocking index lists it. A parent does not block
		// its child.
		{"parent-child does not block", beads.DepParentChild, false},
		{"related does not block", beads.DepRelated, false},
		{"discovered-from does not block", beads.DepDiscoveredFrom, false},
		{"unknown does not block", beads.DepType("mentions"), false},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, tc.depType.Blocks())
		})
	}
}

func (s *issueTestSuite) TestDepTypeClassification() {
	cases := []struct {
		depType    beads.DepType
		blocks     bool
		isRelation bool
	}{
		// The blocking three. Verified against br 1.4.0 by giving ten issues
		// one edge each of a distinct type and reading back `br blocked`:
		// only these three sources were reported.
		{beads.DepBlocks, true, false},
		{beads.DepConditionalBlocks, true, false},
		{beads.DepWaitsFor, true, false},
		// Hierarchy: neither blocking nor a relation.
		{beads.DepParentChild, false, false},
		// The relations, old and new.
		{beads.DepRelated, false, true},
		{beads.DepDiscoveredFrom, false, true},
		{beads.DepRepliesTo, false, true},
		{beads.DepRelatesTo, false, true},
		{beads.DepDuplicates, false, true},
		{beads.DepSupersedes, false, true},
		{beads.DepCausedBy, false, true},
		// Unknown types are neither, so they stay unclassified rather than
		// being guessed at. br validates this field strictly, so a value it
		// does not define can only reach bv through hand-edited JSONL.
		{beads.DepType("no-such-type"), false, false},
	}

	for _, tc := range cases {
		s.Run(string(tc.depType), func() {
			s.Equal(tc.blocks, tc.depType.Blocks(), "Blocks")
			s.Equal(tc.isRelation, tc.depType.IsRelation(), "IsRelation")
		})
	}
}

func (s *issueTestSuite) TestIssueIsTombstone() {
	s.True(beads.Issue{Status: beads.StatusTombstone}.IsTombstone())

	deleted := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	s.True(beads.Issue{Status: beads.StatusOpen, DeletedAt: &deleted}.IsTombstone())

	s.False(beads.Issue{Status: beads.StatusOpen}.IsTombstone())
}
