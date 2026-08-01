package treeview_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
)

func TestTreeview(t *testing.T) {
	suite.Run(t, new(treeTestSuite))
	suite.Run(t, new(prefixTestSuite))
	suite.Run(t, new(navTestSuite))
	suite.Run(t, new(stateTestSuite))
	suite.Run(t, new(treeview.WhiteBoxSuite))
}

type treeTestSuite struct {
	suite.Suite
}

// flattenVisible is a test-local stand-in for the exported Flatten function
// I9 removed (zero non-test callers, superseded by the unexported
// flattenRows in render.go): it walks the tree in display order the same
// way, descending into a node's children only while it is Expanded.
func flattenVisible(nodes []*treeview.Node) []*treeview.Node {
	var rows []*treeview.Node
	for _, n := range nodes {
		rows = append(rows, n)
		if n.Expanded {
			rows = append(rows, flattenVisible(n.Children)...)
		}
	}

	return rows
}

func child(id, parent string) beads.Issue {
	return beads.Issue{
		ID: id, Title: id, Status: beads.StatusOpen,
		Dependencies: []beads.Dependency{
			{IssueID: id, DependsOnID: parent, Type: beads.DepParentChild},
		},
	}
}

func (s *treeTestSuite) TestBuildsDepth() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("feature", "epic"),
		child("task", "feature"),
	})

	roots := treeview.Build(snap)
	s.Require().Len(roots, 1)
	s.Equal("epic", roots[0].Issue.ID)
	s.Equal(0, roots[0].Depth)

	s.Require().Len(roots[0].Children, 1)
	s.Equal(1, roots[0].Children[0].Depth)
	s.Equal(2, roots[0].Children[0].Children[0].Depth)

	// Build's Expanded default is depth == 0: a root starts open, everything
	// below it starts closed. Both mutations of that default (always false,
	// always true) pass every other test in this suite, so it needs a direct
	// pin here.
	s.True(roots[0].Expanded, "a root starts expanded")
	s.False(roots[0].Children[0].Expanded, "a non-root starts collapsed")
}

func (s *treeTestSuite) TestDanglingParentBecomesARoot() {
	// br permits a dependency on an id that no longer exists. Dropping the
	// issue would hide live work; it must surface at the top level instead.
	//
	// This is an integration passthrough, not coverage of anything in this
	// package: the behaviour lives entirely in beads.Snapshot.indexHierarchy
	// (snapshot.go) and is already exercised directly by
	// TestRootsIncludeIssuesWithDanglingParents in internal/beads/snapshot_test.go.
	// Nothing in tree.go could break this test. It stays because it is named
	// as an acceptance criterion for this task.
	snap := beads.NewSnapshot([]beads.Issue{
		child("orphan", "deleted-epic"),
		{ID: "normal", Title: "normal", Status: beads.StatusOpen},
	})

	roots := treeview.Build(snap)
	ids := make([]string, len(roots))
	for i, r := range roots {
		ids[i] = r.Issue.ID
	}
	s.ElementsMatch([]string{"orphan", "normal"}, ids)
}

func (s *treeTestSuite) TestCycleTerminatesWithoutDuplicating() {
	snap := beads.NewSnapshot([]beads.Issue{child("a", "b"), child("b", "a")})

	// This fixture's two records each parent the other, so neither is a root:
	// Snapshot.indexHierarchy assigns each record at most one parent (first
	// edge wins), and a plain mutual cycle like this leaves both without a
	// root ancestor. Roots() therefore comes back empty and Build never even
	// enters buildNode's recursion — this test passes whether or not the
	// path-local guard exists. Pin that so the test cannot silently regress
	// into a no-op: TestCycleReachableThroughADuplicateIDTerminates below is
	// the one that actually reaches the guard from a real root.
	s.Empty(snap.Roots(), "a mutual two-id cycle has no root; Build never recurses here")

	done := make(chan []*treeview.Node, 1)
	go func() { done <- treeview.Build(snap) }()

	// The timeout below converts a hang into a clean test failure, but it
	// cannot catch every way Build could fail to terminate: unguarded
	// recursion on a real cycle is a stack overflow, which is fatal and kills
	// the test process before this select (or its timer) ever runs. The
	// timeout only guards against a Build that spins without recursing
	// (e.g. an infinite loop with no stack growth); TestCycleReachableThrough-
	// ADuplicateIDTerminates is what actually exercises the recursive case,
	// and it passes only because the path-local guard in buildNode prevents
	// the overflow in the first place.
	select {
	case roots := <-done:
		seen := map[string]int{}
		var walk func([]*treeview.Node)
		walk = func(nodes []*treeview.Node) {
			for _, n := range nodes {
				seen[n.Issue.ID]++
				walk(n.Children)
			}
		}
		walk(roots)
		// seen[id] == 1 for every id is a stronger invariant than Build
		// actually provides: two records that legitimately share an id under
		// two different roots would produce two distinct nodes with that id,
		// and FindNode returns whichever it meets first. No fixture in this
		// suite constructs that case, so it is untested rather than
		// guaranteed. Task 5.3's cursor-restore-by-id needs to know this
		// before relying on id uniqueness across the tree.
		for id, count := range seen {
			s.Equal(1, count, "%s appears more than once", id)
		}
	case <-time.After(2 * time.Second):
		s.FailNow("Build did not terminate on a cycle")
	}
}

// TestCycleReachableThroughADuplicateIDTerminates covers a cycle the sibling
// test above cannot reach. Snapshot.indexHierarchy assigns each *record* at
// most one parent (first edge wins), so a plain two-id mutual cycle such as
// child("a","b")/child("b","a") leaves both records without a root ancestor —
// Roots() comes back empty and Build never even enters the recursion, which
// makes that test pass whether or not the path guard exists.
//
// Snapshot.Children, though, is keyed by the id *string*, not by record
// identity, and NewSnapshot documents that a duplicate id is not rejected.
// Chaining a duplicate id back into an already-visited id — root -> a -> mid
// -> a(second record, different pointer, same id) -> mid again through the
// shared "a" children-bucket — reaches an actual cycle from a real root, and
// only the path-local guard stops it from recursing forever.
func (s *treeTestSuite) TestCycleReachableThroughADuplicateIDTerminates() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		child("a", "root"),
		child("mid", "a"),
		child("a", "mid"), // second "a" record: closes the loop back through mid
	})

	done := make(chan []*treeview.Node, 1)
	go func() { done <- treeview.Build(snap) }()

	// The timeout below only catches a Build that hangs without recursing; an
	// unguarded recursive cycle is a fatal stack overflow that kills the test
	// process before this select or its timer is ever consulted. What
	// actually prevents that here is buildNode's path-local visited set — the
	// timeout is a backstop for a different failure mode, not a substitute
	// for it.
	select {
	case roots := <-done:
		seen := map[string]int{}
		var walk func([]*treeview.Node)
		walk = func(nodes []*treeview.Node) {
			for _, n := range nodes {
				seen[n.Issue.ID]++
				walk(n.Children)
			}
		}
		walk(roots)
		// As in the sibling test above, seen[id] == 1 is stronger than
		// anything Build guarantees in general — it holds here because this
		// fixture never lets two same-id records land under different roots.
		for id, count := range seen {
			s.Equal(1, count, "%s appears more than once", id)
		}
	case <-time.After(2 * time.Second):
		s.FailNow("Build did not terminate on a cycle reachable through a duplicate id")
	}
}

func (s *treeTestSuite) TestFlattenRespectsExpansion() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("a", "epic"),
		child("b", "epic"),
	})
	roots := treeview.Build(snap)

	roots[0].Expanded = false
	s.Len(flattenVisible(roots), 1, "a collapsed parent hides its subtree")

	roots[0].Expanded = true
	s.Len(flattenVisible(roots), 3)
}

func (s *treeTestSuite) TestRetainHidesClosed() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("open-child", "epic"),
		{
			ID: "closed-child", Title: "closed", Status: beads.StatusClosed,
			Dependencies: []beads.Dependency{
				{IssueID: "closed-child", DependsOnID: "epic", Type: beads.DepParentChild},
			},
		},
	})
	roots := treeview.Retain(treeview.Build(snap), beads.Filter{HideClosed: true})
	s.Require().Len(roots, 1)
	roots[0].Expanded = true

	rows := flattenVisible(roots)
	s.Len(rows, 2)
	for _, r := range rows {
		s.NotEqual("closed-child", r.Issue.ID)
	}
}

// TestRetainKeepsAClosedAncestorOfAnOpenMatch is the case that made folding
// hideClosed into Retain necessary rather than merely tidier. Under the old
// Flatten(roots, hideClosed) design the closed epic was dropped and its open
// child went with it — a live issue vanishing because its parent was done.
func (s *treeTestSuite) TestRetainKeepsAClosedAncestorOfAnOpenMatch() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "finished epic", Status: beads.StatusClosed},
		child("open-child", "epic"),
	})

	roots := treeview.Retain(treeview.Build(snap), beads.Filter{HideClosed: true})

	s.Require().Len(roots, 1)
	s.Equal("epic", roots[0].Issue.ID)
	s.False(roots[0].MatchesFilter, "the closed epic is kept only for reachability")
	s.Require().Len(roots[0].Children, 1)
	s.Equal("open-child", roots[0].Children[0].Issue.ID)
	s.True(roots[0].Children[0].MatchesFilter)
}

func (s *treeTestSuite) TestRetainKeepsMatchesReachable() {
	// The bug this exists to prevent: filtering for a leaf keeps the leaf and
	// drops its parent, leaving the match with nowhere to hang in a tree.
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "unrelated epic", Status: beads.StatusOpen},
		child("mid", "epic"),
		{
			ID: "leaf", Title: "decoder work", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "leaf", DependsOnID: "mid", Type: beads.DepParentChild},
			},
		},
	})

	roots := treeview.Retain(treeview.Build(snap), beads.Filter{Text: "decoder"})

	s.Require().Len(roots, 1)
	s.Equal("epic", roots[0].Issue.ID)
	for _, r := range roots {
		r.Expanded = true
		for _, c := range r.Children {
			c.Expanded = true
		}
	}
	s.Len(flattenVisible(roots), 3,
		"the whole chain must survive so the match is reachable")
}

func (s *treeTestSuite) TestRetainMarksNonMatches() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "unrelated", Status: beads.StatusOpen},
		{
			ID: "leaf", Title: "decoder", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "leaf", DependsOnID: "epic", Type: beads.DepParentChild},
			},
		},
	})

	roots := treeview.Retain(treeview.Build(snap), beads.Filter{Text: "decoder"})
	s.Require().Len(roots, 1)
	s.False(roots[0].MatchesFilter, "an ancestor kept for reachability is dimmed")
	s.Require().Len(roots[0].Children, 1)
	s.True(roots[0].Children[0].MatchesFilter)
}

// TestRetainDropsAWhollyUnmatchedSubtree is the negative case. Without it,
// a Retain that returned its input unchanged — never pruning anything — would
// satisfy every other assertion in this suite.
func (s *treeTestSuite) TestRetainDropsAWhollyUnmatchedSubtree() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "keep", Title: "decoder", Status: beads.StatusOpen},
		{ID: "drop", Title: "unrelated", Status: beads.StatusOpen},
		child("drop-child", "drop"),
	})

	roots := treeview.Retain(treeview.Build(snap), beads.Filter{Text: "decoder"})

	s.Require().Len(roots, 1)
	s.Equal("keep", roots[0].Issue.ID)
}

// TestRetainWithAZeroFilterKeepsEverything pins the identity case: an empty
// filter must not prune, and must not mark every node as a non-match.
func (s *treeTestSuite) TestRetainWithAZeroFilterKeepsEverything() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("a", "epic"),
		child("b", "epic"),
	})

	roots := treeview.Retain(treeview.Build(snap), beads.Filter{})

	s.Require().Len(roots, 1)
	s.True(roots[0].MatchesFilter)
	s.Len(roots[0].Children, 2)
}

// TestRetainDropsATombstoneLeafUnderAZeroFilter pins the other half of the
// zero-filter behaviour: beads.Filter.Matches hides tombstones unless
// ShowTombstones is set, so an empty Filter is not the identity. A tombstone
// leaf must still be dropped even when no filter narrows anything else.
func (s *treeTestSuite) TestRetainDropsATombstoneLeafUnderAZeroFilter() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		{
			ID: "tomb-leaf", Title: "tomb-leaf", Status: beads.StatusTombstone,
			Dependencies: []beads.Dependency{
				{IssueID: "tomb-leaf", DependsOnID: "root", Type: beads.DepParentChild},
			},
		},
	})

	roots := treeview.Retain(treeview.Build(snap), beads.Filter{})

	s.Require().Len(roots, 1)
	s.Equal("root", roots[0].Issue.ID)
	s.Empty(roots[0].Children, "a tombstone leaf is dropped even under a zero filter")
}

// TestRetainKeepsATombstoneAncestorOfALiveChildUnderAZeroFilter is the
// reachability counterpart: a tombstone with a live descendant is kept, the
// same way a closed ancestor is (TestRetainKeepsAClosedAncestorOfAnOpenMatch),
// but it is never marked matched — Filter.Matches never returns true for a
// tombstone unless ShowTombstones is set.
func (s *treeTestSuite) TestRetainKeepsATombstoneAncestorOfALiveChildUnderAZeroFilter() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "tomb-root", Title: "tomb-root", Status: beads.StatusTombstone},
		child("live-child", "tomb-root"),
	})

	roots := treeview.Retain(treeview.Build(snap), beads.Filter{})

	s.Require().Len(roots, 1)
	s.Equal("tomb-root", roots[0].Issue.ID)
	s.False(roots[0].MatchesFilter, "a tombstone is kept only for reachability, never marked matched")
	s.Require().Len(roots[0].Children, 1)
	s.Equal("live-child", roots[0].Children[0].Issue.ID)
	s.True(roots[0].Children[0].MatchesFilter)
}

// TestRetainMutatesInPlaceAndDoesNotRecoverPrunedNodes pins the destructive
// hazard documented on Retain: it prunes n.Children in place, so a tree that
// has already been Retained once cannot be widened back out by calling Retain
// again with a looser filter on the same tree. A caller that wants to go back
// to a wider view — such as a future cache in Task 5.3 — must rebuild from
// Build, not re-filter the already-pruned tree.
func (s *treeTestSuite) TestRetainMutatesInPlaceAndDoesNotRecoverPrunedNodes() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("keep", "epic"),
		child("drop", "epic"),
	})
	tree := treeview.Build(snap)

	tight := treeview.Retain(tree, beads.Filter{Text: "keep"})
	s.Require().Len(tight, 1)
	s.Require().Len(tight[0].Children, 1)
	s.Equal("keep", tight[0].Children[0].Issue.ID)

	// Same tree, looser filter: "drop" does not come back, because Retain
	// already overwrote tight[0].Children (== tree[0].Children) in place.
	widened := treeview.Retain(tree, beads.Filter{})
	s.Require().Len(widened, 1)
	s.Len(widened[0].Children, 1, "the child pruned by the earlier Retain call never returns")
	s.Equal("keep", widened[0].Children[0].Issue.ID)
}

func (s *treeTestSuite) TestEmptySnapshot() {
	s.Empty(treeview.Build(beads.NewSnapshot(nil)))
	s.Empty(flattenVisible(nil))
	s.Empty(treeview.Retain(nil, beads.Filter{}))
}

func (s *treeTestSuite) TestFindNode() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("deep", "epic"),
	})
	roots := treeview.Build(snap)

	got, ok := treeview.FindNode(roots, "deep")
	s.Require().True(ok)
	s.Equal("deep", got.Issue.ID)

	_, ok = treeview.FindNode(roots, "nope")
	s.False(ok)
}
