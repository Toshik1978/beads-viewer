package beads_test

import (
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

// inheritTestSuite pins the inherited-blockedness rule on a hand-built
// snapshot. TestMatchesBrBlocked exercises the same rule against a live br,
// but it reads this repository's own .beads/, which is edited constantly and
// may stop containing the shape under test at any moment — including the
// parent cycle, which br would never write at all. This fixture cannot drift.
type inheritTestSuite struct {
	suite.Suite

	snap *beads.Snapshot
}

func withBlocks(targetID string) func(*beads.Issue) {
	return func(i *beads.Issue) {
		i.Dependencies = append(i.Dependencies, beads.Dependency{
			IssueID: i.ID, DependsOnID: targetID, Type: beads.DepBlocks,
		})
	}
}

func withStatus(status beads.Status) func(*beads.Issue) {
	return func(i *beads.Issue) { i.Status = status }
}

func withType(issueType beads.IssueType) func(*beads.Issue) {
	return func(i *beads.Issue) { i.IssueType = issueType }
}

func (s *inheritTestSuite) SetupSuite() {
	s.snap = beads.NewSnapshot([]beads.Issue{
		// A three-deep chain under a feature that waits on live work outside it.
		mkIssue("h-gate"),
		mkIssue("h-root", withBlocks("h-gate"), withType(beads.TypeFeature)),
		mkIssue("h-mid", withParent("h-root")),
		mkIssue("h-leaf", withParent("h-mid")),
		// The same shape with the gate closed: nothing is waiting on anything.
		mkIssue("h-done-gate", withStatus(beads.StatusClosed)),
		mkIssue("h-clear-root", withBlocks("h-done-gate")),
		mkIssue("h-clear-kid", withParent("h-clear-root")),
		// An epic blocked only because a child is open. That marker must not
		// travel downward, or the child would be blocked by its own existence.
		mkIssue("h-epic", withType(beads.TypeEpic)),
		mkIssue("h-epic-kid", withParent("h-epic")),
		// A parent cycle, which only a hand-edited issues.jsonl produces. Walking
		// it must terminate; h-cyc-dep drags h-cyc-free in through the cycle.
		mkIssue("h-cyc-a", withParent("h-cyc-b")),
		mkIssue("h-cyc-b", withParent("h-cyc-a")),
		mkIssue("h-cyc-free", withParent("h-cyc-dep")),
		mkIssue("h-cyc-dep", withParent("h-cyc-free"), withBlocks("h-gate")),
		// An issue that declares itself its own parent.
		mkIssue("h-self", withParent("h-self")),
	})
}

func (s *inheritTestSuite) TestIsBlocked() {
	cases := []struct {
		id   string
		want bool
		why  string
	}{
		{"h-gate", false, "the gate itself waits on nothing"},
		{"h-root", true, "its own blocks edge is unsatisfied"},
		{"h-mid", true, "its parent is blocked, and the wait reaches down"},
		{"h-leaf", true, "inheritance travels the whole chain, not one hop"},
		{"h-clear-root", false, "a closed blocker is satisfied"},
		{"h-clear-kid", false, "an unblocked parent passes nothing down"},
		{"h-epic", true, "an epic with an open child is blocked"},
		{"h-epic-kid", false, "the open-child rule does not travel downward"},
		{"h-cyc-a", false, "a parent cycle with no blocking edge blocks nobody"},
		{"h-cyc-b", false, "and the walk terminates from either end"},
		{"h-cyc-dep", true, "blocked by its own edge, cycle or not"},
		{"h-cyc-free", true, "inherits from the blocked issue in its cycle"},
		{"h-self", false, "an issue is not its own blocked ancestor"},
	}
	for _, tc := range cases {
		s.Run(tc.id+": "+tc.why, func() {
			s.Equal(tc.want, s.snap.IsBlocked(tc.id))
		})
	}
}

// TestInheritedBlockNamesNoBlocker fixes the boundary between IsBlocked and
// Blockers: an inherited wait is a fact about the subtree, not an edge on this
// issue, so naming the parent here would report a "blocked by" relationship
// that no dependency row in the workspace actually states.
func (s *inheritTestSuite) TestInheritedBlockNamesNoBlocker() {
	s.True(s.snap.IsBlocked("h-leaf"))
	s.Empty(s.snap.Blockers("h-leaf"))

	s.Require().Len(s.snap.Blockers("h-root"), 1)
	s.Equal("h-gate", s.snap.Blockers("h-root")[0].ID)
}

// TestBlockedAncestorNamesTheHolderOfTheEdge pins which ancestor is reported.
// h-leaf's nearest *blocked* ancestor is h-mid, but h-mid is only blocked by
// inheritance itself; the one worth naming is h-root, which holds the edge.
func (s *inheritTestSuite) TestBlockedAncestorNamesTheHolderOfTheEdge() {
	ancestor, ok := s.snap.BlockedAncestor("h-leaf")
	s.Require().True(ok)
	s.Equal("h-root", ancestor.ID)

	ancestor, ok = s.snap.BlockedAncestor("h-mid")
	s.Require().True(ok)
	s.Equal("h-root", ancestor.ID)

	_, ok = s.snap.BlockedAncestor("h-root")
	s.False(ok, "the holder of the edge does not inherit from itself")
	_, ok = s.snap.BlockedAncestor("h-clear-kid")
	s.False(ok)
	_, ok = s.snap.BlockedAncestor("h-epic-kid")
	s.False(ok, "the open-child rule is not inherited, so it names no ancestor")
	_, ok = s.snap.BlockedAncestor("does-not-exist")
	s.False(ok)
}

func (s *inheritTestSuite) TestBlockedByOpenChildIsEpicOnly() {
	s.True(s.snap.BlockedByOpenChild("h-epic"))
	s.False(s.snap.BlockedByOpenChild("h-root"), "a blocked feature parent is not blocked by its child")
	s.False(s.snap.BlockedByOpenChild("h-epic-kid"))
	s.False(s.snap.BlockedByOpenChild("does-not-exist"))
}

// TestDanglingBlockersNameTheMissingIds covers the third reason IsBlocked can
// be true with nothing in Blockers, and the property that keeps it honest:
// every entry it reports is an edge the blocker rule accepts, so a missing id
// only counts when it is missing from a row that would otherwise block.
// h-external is the case that separates "absent" from "not a blocker at all" —
// br stores external refs and never blocks on them — and the non-blocking edge
// types are the ones that would start leaking in if this filter were ever
// restated instead of shared with the rule itself.
func (s *inheritTestSuite) TestDanglingBlockersNameTheMissingIds() {
	otherEdge := func(id, target string, depType beads.DepType) func(*beads.Issue) {
		return func(i *beads.Issue) {
			i.Dependencies = append(i.Dependencies, beads.Dependency{
				IssueID: id, DependsOnID: target, Type: depType,
			})
		}
	}
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("h-here"),
		mkIssue("h-missing", withBlocks("h-nowhere"), withBlocks("h-here")),
		mkIssue("h-external", withBlocks("external:JIRA-1")),
		mkIssue("h-kin", withParent("h-nobody")),
		mkIssue("h-related", otherEdge("h-related", "h-nobody", beads.DepRelated)),
		mkIssue("h-found", otherEdge("h-found", "h-nobody", beads.DepDiscoveredFrom)),
	})

	s.Equal([]string{"h-nowhere"}, snap.DanglingBlockers("h-missing"))
	s.Empty(snap.DanglingBlockers("h-here"))
	s.Empty(snap.DanglingBlockers("h-external"), "external refs are absent by design, not danglers")
	s.Empty(snap.DanglingBlockers("h-kin"), "a missing parent is not a blocking edge")
	s.Empty(snap.DanglingBlockers("h-related"), "a missing related target is not a blocking edge")
	s.Empty(snap.DanglingBlockers("h-found"), "nor is a missing discovered-from target")
	s.Empty(snap.DanglingBlockers("does-not-exist"))

	// The invariant that ties the list back to the rule it filters with: an id
	// can only appear here if it is one of the reasons IsBlocked says yes.
	for _, issue := range snap.Issues() {
		if len(snap.DanglingBlockers(issue.ID)) > 0 {
			s.True(snap.IsBlocked(issue.ID), issue.ID)
		}
	}
}

func (s *inheritTestSuite) TestInheritedBlockWithholdsReadiness() {
	s.False(s.snap.IsReady("h-leaf"))
	s.True(s.snap.IsReady("h-clear-kid"))
	s.True(s.snap.IsReady("h-epic-kid"))
}

// TestCountsDoNotDoubleCount guards the other consumer of blockedness. Ready
// and Blocked are disjoint by construction — IsReady ends in !IsBlocked — and
// both live inside the non-terminal branch, so widening blockedness moves
// issues between the two rather than inflating either total.
func (s *inheritTestSuite) TestCountsDoNotDoubleCount() {
	counts := s.snap.Counts()

	s.Equal(s.snap.Len(), counts.Total)
	s.Equal(counts.Total, counts.Open+counts.Closed)
	s.LessOrEqual(counts.Ready+counts.Blocked, counts.Open)

	blocked, ready := 0, 0
	for _, issue := range s.snap.Issues() {
		if issue.Status.IsTerminal() {
			continue
		}
		if s.snap.IsBlocked(issue.ID) {
			blocked++
		}
		if s.snap.IsReady(issue.ID) {
			ready++
		}
	}
	s.Equal(blocked, counts.Blocked)
	s.Equal(ready, counts.Ready)
}
