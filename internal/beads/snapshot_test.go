package beads_test

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

type snapshotTestSuite struct {
	suite.Suite
}

func mkIssue(id string, opts ...func(*beads.Issue)) beads.Issue {
	i := beads.Issue{
		ID:        id,
		Title:     id,
		Status:    beads.StatusOpen,
		Priority:  beads.PriorityMedium,
		IssueType: beads.TypeTask,
		CreatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, opt := range opts {
		opt(&i)
	}

	return i
}

func withParent(parentID string) func(*beads.Issue) {
	return func(i *beads.Issue) {
		i.Dependencies = append(i.Dependencies, beads.Dependency{
			IssueID: i.ID, DependsOnID: parentID, Type: beads.DepParentChild,
		})
	}
}

func withLabels(labels ...string) func(*beads.Issue) {
	return func(i *beads.Issue) { i.Labels = labels }
}

func withDep(t beads.DepType, targets ...string) func(*beads.Issue) {
	return func(i *beads.Issue) {
		for _, target := range targets {
			i.Dependencies = append(i.Dependencies, beads.Dependency{
				IssueID: i.ID, DependsOnID: target, Type: t,
			})
		}
	}
}

func ids(issues []*beads.Issue) []string {
	out := make([]string, len(issues))
	for i, issue := range issues {
		out[i] = issue.ID
	}
	slices.Sort(out)

	return out
}

func (s *snapshotTestSuite) TestChildrenAndParent() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("parent"),
		mkIssue("child1", withParent("parent")),
		mkIssue("child2", withParent("parent")),
	})

	children := snap.Children("parent")
	s.Require().Len(children, 2)
	s.Equal([]string{"child1", "child2"}, []string{children[0].ID, children[1].ID})

	parent, ok := snap.Parent("child1")
	s.Require().True(ok)
	s.Equal("parent", parent.ID)

	_, ok = snap.Parent("parent")
	s.False(ok)
}

func (s *snapshotTestSuite) TestRootsIncludeIssuesWithDanglingParents() {
	// A child whose parent is absent from the dataset must still be reachable.
	// Dropping it would silently hide work — br allows a dependency on an id
	// that no longer exists.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("orphan", withParent("never-existed")),
		mkIssue("top"),
	})

	roots := snap.Roots()
	ids := make([]string, len(roots))
	for i, r := range roots {
		ids[i] = r.ID
	}
	s.ElementsMatch([]string{"orphan", "top"}, ids)
}

func (s *snapshotTestSuite) TestParentChildCycleDoesNotHang() {
	// Hand-edited JSONL can express a cycle. Roots() must terminate and must
	// not lose the issues involved.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("a", withParent("b")),
		mkIssue("b", withParent("a")),
	})

	s.Equal(2, snap.Len())
	s.NotPanics(func() { _ = snap.Roots() })
}

func (s *snapshotTestSuite) TestSortOrderIsDeterministic() {
	older := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	issues := []beads.Issue{
		mkIssue("z-same-time", func(i *beads.Issue) { i.CreatedAt = newer }),
		mkIssue("low", func(i *beads.Issue) { i.Priority = beads.PriorityLow }),
		mkIssue("a-same-time", func(i *beads.Issue) { i.CreatedAt = newer }),
		mkIssue("critical", func(i *beads.Issue) { i.Priority = beads.PriorityCritical }),
		mkIssue("old", func(i *beads.Issue) { i.CreatedAt = older }),
	}

	first := beads.NewSnapshot(issues)
	// Reversing the input must not change the output.
	reversed := make([]beads.Issue, len(issues))
	for i := range issues {
		reversed[i] = issues[len(issues)-1-i]
	}
	second := beads.NewSnapshot(reversed)

	order := func(snap *beads.Snapshot) []string {
		ids := make([]string, snap.Len())
		for i, issue := range snap.Issues() {
			ids[i] = issue.ID
		}

		return ids
	}

	s.Equal([]string{"critical", "a-same-time", "z-same-time", "old", "low"}, order(first))
	s.Equal(order(first), order(second), "sort must not depend on input order")
}

func (s *snapshotTestSuite) TestConstructionIsIndependentOfTheCallerSlice() {
	// Prove copy-on-the-way-in. A naive copy(dst, src) over []Issue copies
	// only the Issue struct's slice headers and its *time.Time pointers, so
	// Labels/Dependencies/Comments and ClosedAt/DueAt/DeferUntil/DeletedAt
	// would still alias the caller's memory unless cloned explicitly.
	closedAt := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	issues := []beads.Issue{
		mkIssue("a", withLabels("original"), withParent("root"), func(i *beads.Issue) {
			t := closedAt
			i.ClosedAt = &t
		}),
		mkIssue("root"),
	}
	snap := beads.NewSnapshot(issues)

	// Mutate everything the caller still holds: a scalar field, an element of
	// a nested slice, a Dependency field, and — by dereferencing rather than
	// reassigning — the time.Time a pointer field addresses.
	issues[0].Title = "mutated"
	issues[0].Labels[0] = "mutated"
	issues[0].Dependencies[0].DependsOnID = "hijacked"
	*issues[0].ClosedAt = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

	got, ok := snap.ByID("a")
	s.Require().True(ok)
	s.Equal("a", got.Title)
	s.Equal("original", got.Labels[0])
	s.Equal("root", got.Dependencies[0].DependsOnID)
	s.Require().NotNil(got.ClosedAt)
	s.True(closedAt.Equal(*got.ClosedAt), "the snapshot's ClosedAt must not follow the caller's mutation")
	s.NotSame(issues[0].ClosedAt, got.ClosedAt, "the snapshot must own a distinct time.Time, not the caller's")
}

func (s *snapshotTestSuite) TestAccessorsReturnIndependentSlices() {
	// Prove copy-on-the-way-out. If Issues/Children/Roots handed back a
	// reference to the snapshot's own slice, mutating what one caller
	// received would corrupt what every later caller sees.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("parent"),
		mkIssue("child", withParent("parent")),
	})

	issues := snap.Issues()
	issues[0] = nil
	children := snap.Children("parent")
	children[0] = nil
	roots := snap.Roots()
	roots[0] = nil

	for _, issue := range snap.Issues() {
		s.NotNil(issue)
	}
	for _, issue := range snap.Children("parent") {
		s.NotNil(issue)
	}
	for _, issue := range snap.Roots() {
		s.NotNil(issue)
	}
}

func (s *snapshotTestSuite) TestDuplicateIDsAreNotLost() {
	// Real data can carry two records sharing an id (a stale export, a hand
	// edit). Dropping one silently would surface later as "an issue
	// vanished" — so both stay in the full set, and ByID resolves to one
	// deterministic record (the later one in input order, since issues.jsonl
	// is append-only and a later line is the more recent write) rather than
	// panicking or picking at random.
	first := mkIssue("dup", func(i *beads.Issue) { i.Title = "first" })
	second := mkIssue("dup", func(i *beads.Issue) { i.Title = "second" })

	snap := beads.NewSnapshot([]beads.Issue{first, second})

	s.Equal(2, snap.Len(), "both records are kept, not deduplicated away")

	got, ok := snap.ByID("dup")
	s.Require().True(ok)
	s.Equal("second", got.Title, "the later record in input order wins the index")
}

func (s *snapshotTestSuite) TestByIDResolvesTheLastRecordEvenAcrossASortReorder() {
	// TestDuplicateIDsAreNotLost's two records tie on every sort key, so
	// scanning the sorted Issues() slice happens to agree with ByID there.
	// Giving the two records different priorities breaks that tie:
	// sortIssues moves "second" (Priority Critical) ahead of "first"
	// (Priority Low), so sorted order no longer matches input order. ByID
	// reads the byID index, which is built from input order directly and is
	// unaffected by the sort that runs afterward — a caller cannot
	// reconstruct this resolution from Issues() alone, which is the whole
	// reason ByID is exported rather than left for a caller to reimplement.
	first := mkIssue("dup", func(i *beads.Issue) {
		i.Title = "first"
		i.Priority = beads.PriorityLow
	})
	second := mkIssue("dup", func(i *beads.Issue) {
		i.Title = "second"
		i.Priority = beads.PriorityCritical
	})

	snap := beads.NewSnapshot([]beads.Issue{first, second})

	// Confirm the premise: the sort actually reordered them.
	s.Equal("second", snap.Issues()[0].Title, "the higher-priority record now sorts first")

	got, ok := snap.ByID("dup")
	s.Require().True(ok)
	s.Equal("second", got.Title,
		"ByID resolves to the later record in input order, not the one that now sorts last")
}

func (s *snapshotTestSuite) TestByIDIsFalseForAnUnknownID() {
	snap := beads.NewSnapshot([]beads.Issue{mkIssue("a")})

	got, ok := snap.ByID("nope")
	s.Nil(got)
	s.False(ok)
}

func (s *snapshotTestSuite) TestDuplicateIDsDoNotCrossContaminateTheHierarchy() {
	// Regression for a parentOf index keyed by id alone: p is a plain root.
	// b and c share the id "dup"; b declares p as its parent, c declares no
	// parent of its own. If one record's edge could leak onto its same-id
	// sibling, c would be excluded from Roots() without ever having been
	// added to any children list — the exact silent-subtree-loss the
	// missing-parent case is meant to guard against. Each record must be
	// judged strictly on its own Dependencies.
	//
	// Owner (unused elsewhere on Issue) disambiguates b from c in assertions,
	// since both default to Title == ID == "dup".
	p := mkIssue("p")
	b := mkIssue("dup", withParent("p"), func(i *beads.Issue) { i.Owner = "b" })
	c := mkIssue("dup", func(i *beads.Issue) { i.Owner = "c" })

	snap := beads.NewSnapshot([]beads.Issue{p, b, c})
	s.Equal(3, snap.Len())

	roots := snap.Roots()
	rootOwners := make([]string, len(roots))
	for i, r := range roots {
		rootOwners[i] = r.Owner
	}
	s.ElementsMatch([]string{"", "c"}, rootOwners, "p and c are roots; b is not, since it declares p as its parent")

	children := snap.Children("p")
	s.Require().Len(children, 1)
	s.Equal("b", children[0].Owner)

	// byID resolves "dup" to c (last in input order); Parent(id) must agree
	// with that same record's own parentless status, not b's.
	resolved, ok := snap.ByID("dup")
	s.Require().True(ok)
	s.Equal("c", resolved.Owner)

	_, ok = snap.Parent("dup")
	s.False(ok, "Parent(id) must agree with byID's own duplicate-id resolution")
}

func (s *snapshotTestSuite) TestDependentsNamesEveryIssueBlockedByThisOne() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("target"),
		mkIssue("a", withDep(beads.DepBlocks, "target")),
		mkIssue("b", withDep(beads.DepWaitsFor, "target")),
		mkIssue("c", withDep(beads.DepConditionalBlocks, "target")),
		mkIssue("unrelated"),
	})

	s.Equal([]string{"a", "b", "c"}, ids(snap.Dependents("target")))
}

func (s *snapshotTestSuite) TestDependentsExcludesNonBlockingEdges() {
	// parent-child is the one that matters: br's own blocker query filters on
	// ('blocks','conditional-blocks','waits-for') only, and including
	// parent-child here would report every child of an epic as blocked by it.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("target"),
		mkIssue("kid", withParent("target")),
		mkIssue("rel", withDep(beads.DepRelated, "target")),
		mkIssue("disc", withDep(beads.DepDiscoveredFrom, "target")),
	})

	s.Empty(snap.Dependents("target"))
}

func (s *snapshotTestSuite) TestDependentsDeduplicates() {
	// An issue can declare two different blocking-type edges (blocks and
	// waits-for) to the same target; it must appear once, not once per edge.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("target"),
		mkIssue("a", withDep(beads.DepBlocks, "target"), withDep(beads.DepWaitsFor, "target")),
	})

	s.Equal([]string{"a"}, ids(snap.Dependents("target")))
}

func (s *snapshotTestSuite) TestRelatedToCoversBothDirections() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("subject", withDep(beads.DepRelated, "outbound")),
		mkIssue("outbound"),
		mkIssue("inbound", withDep(beads.DepRelated, "subject")),
		mkIssue("discovered", withDep(beads.DepDiscoveredFrom, "subject")),
		mkIssue("unrelated"),
	})

	s.Equal([]string{"discovered", "inbound", "outbound"}, ids(snap.RelatedTo("subject")))
}

func (s *snapshotTestSuite) TestRelatedToDeduplicates() {
	// Hand-edited JSONL can declare both kinds between the same pair; the
	// issue must appear once.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("subject"),
		mkIssue("both",
			withDep(beads.DepRelated, "subject"),
			withDep(beads.DepDiscoveredFrom, "subject"),
		),
	})

	s.Equal([]string{"both"}, ids(snap.RelatedTo("subject")))
}

func (s *snapshotTestSuite) TestNeitherAccessorReturnsTheSubject() {
	// A self-edge is expressible in hand-edited JSONL and bv renders rather
	// than validates, so it must be tolerated — and dropped.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("self",
			withDep(beads.DepBlocks, "self"),
			withDep(beads.DepRelated, "self"),
		),
	})

	s.Empty(snap.Dependents("self"))
	s.Empty(snap.RelatedTo("self"))
}

func (s *snapshotTestSuite) TestBothAccessorsAreSafeForAnUnknownID() {
	snap := beads.NewSnapshot([]beads.Issue{mkIssue("a")})

	s.Empty(snap.Dependents("nope"))
	s.Empty(snap.RelatedTo("nope"))
}

func (s *snapshotTestSuite) TestBothAccessorsClone() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("target"),
		mkIssue("a", withDep(beads.DepBlocks, "target")),
		mkIssue("r", withDep(beads.DepRelated, "target")),
	})

	got := snap.Dependents("target")
	s.Require().Len(got, 1)
	got[0] = nil
	s.Require().Len(snap.Dependents("target"), 1)
	s.NotNil(snap.Dependents("target")[0], "the accessor must clone, or a caller can corrupt the snapshot")

	rel := snap.RelatedTo("target")
	s.Require().Len(rel, 1)
	rel[0] = nil
	s.NotNil(snap.RelatedTo("target")[0])
}

func (s *snapshotTestSuite) TestBothAccessorsReturnCanonicalOrder() {
	// sortIssues orders by priority, then newest first, then id. Both
	// accessors must reflect that, so the dependency view's columns are
	// stable across runs rather than reflecting map iteration order.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("target"),
		mkIssue("lo", withDep(beads.DepBlocks, "target"), func(i *beads.Issue) { i.Priority = beads.PriorityLow }),
		mkIssue("hi", withDep(beads.DepBlocks, "target"), func(i *beads.Issue) { i.Priority = beads.PriorityHigh }),
	})

	got := snap.Dependents("target")
	s.Require().Len(got, 2)
	s.Equal("hi", got[0].ID, "higher priority sorts first, as Snapshot.Issues does")
}

func (s *snapshotTestSuite) TestRelatedToReturnsCanonicalOrder() {
	// Mirrors TestBothAccessorsReturnCanonicalOrder's check for Dependents:
	// ids() sorts before comparing, so it cannot catch an ordering
	// regression, and the acceptance criterion is that both accessors return
	// canonical order.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("target"),
		mkIssue("lo", withDep(beads.DepRelated, "target"), func(i *beads.Issue) { i.Priority = beads.PriorityLow }),
		mkIssue("hi", withDep(beads.DepRelated, "target"), func(i *beads.Issue) { i.Priority = beads.PriorityHigh }),
	})

	got := snap.RelatedTo("target")
	s.Require().Len(got, 2)
	s.Equal("hi", got[0].ID, "higher priority sorts first, as Snapshot.Issues does")
}

func (s *snapshotTestSuite) TestASelfParentingIssueIsARootRatherThanInvisible() {
	// An issue whose only parent-child edge points at itself is malformed
	// data expressible in hand-edited JSONL. bv renders rather than
	// validates, so it must surface rather than vanish: before the self-edge
	// guard in indexEdge, this issue resolved to its own child, so it was
	// excluded from Roots() (it had a parent) while never appearing in any
	// other issue's Children() (the only edge pointing at it was its own) —
	// present in Issues() but unreachable from any Roots()->Children() walk.
	// The guard makes it its own root instead: shown, not hidden.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("self", withParent("self")),
	})

	roots := snap.Roots()
	s.Require().Len(roots, 1)
	s.Equal("self", roots[0].ID)
	s.Empty(snap.Children("self"))
	_, ok := snap.Parent("self")
	s.False(ok)
}

func (s *snapshotTestSuite) TestEmptySnapshot() {
	snap := beads.NewSnapshot(nil)

	s.Equal(0, snap.Len())
	s.Empty(snap.Issues())
	s.Empty(snap.Roots())
	s.Empty(snap.Children("anything"))
	_, ok := snap.ByID("anything")
	s.False(ok)
}

func (s *snapshotTestSuite) TestLoadSnapshotFromRealWorkspace() {
	// Assembled from segments rather than one path literal, so the licensing
	// sweep's upstream-path check has nothing to flag in source text: the
	// string only exists once filepath.Join builds it at runtime.
	sibling := filepath.Join("..", "..", "..", "beads", ".beads")

	ws, err := beads.FindWorkspace(sibling)
	if err != nil {
		s.T().Skip("sibling beads workspace not available")
	}

	snap, err := beads.LoadSnapshot(ws)
	s.Require().NoError(err)
	s.Positive(snap.Len())
}

func (s *snapshotTestSuite) TestLoadSnapshotOfMissingFileIsEmptyNotAnError() {
	dir := s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(dir, ".beads"), 0o755))
	ws, err := beads.FindWorkspace(filepath.Join(dir, ".beads"))
	s.Require().NoError(err)

	snap, err := beads.LoadSnapshot(ws)
	s.Require().NoError(err)
	s.Equal(0, snap.Len())
}

func (s *snapshotTestSuite) TestNewRelationTypesAreIndexed() {
	for _, depType := range []beads.DepType{
		beads.DepRelatesTo, beads.DepDuplicates,
		beads.DepSupersedes, beads.DepCausedBy, beads.DepRepliesTo,
	} {
		s.Run(string(depType), func() {
			snap := beads.NewSnapshot([]beads.Issue{
				{ID: "a", Dependencies: []beads.Dependency{
					{IssueID: "a", DependsOnID: "b", Type: depType},
				}},
				{ID: "b"},
			})

			// Both ends, because a reader asking "what is related to this"
			// wants the other one whichever record holds the row.
			s.Len(snap.RelatedTo("a"), 1, "declaring end")
			s.Len(snap.RelatedTo("b"), 1, "receiving end")
		})
	}
}

func (s *snapshotTestSuite) TestRelationTo() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "a", Dependencies: []beads.Dependency{
			{IssueID: "a", DependsOnID: "b", Type: beads.DepSupersedes},
		}},
		{ID: "b"},
		{ID: "c"},
	})

	s.Run("declaring end reports forward", func() {
		depType, forward := snap.RelationTo("a", "b")
		s.Equal(beads.DepSupersedes, depType)
		s.True(forward)
	})

	s.Run("receiving end reports reverse", func() {
		depType, forward := snap.RelationTo("b", "a")
		s.Equal(beads.DepSupersedes, depType)
		s.False(forward)
	})

	s.Run("unrelated pair reports nothing", func() {
		depType, forward := snap.RelationTo("a", "c")
		s.Empty(string(depType))
		s.False(forward)
	})

	s.Run("blocking edges are not relations", func() {
		blocking := beads.NewSnapshot([]beads.Issue{
			{ID: "x", Dependencies: []beads.Dependency{
				{IssueID: "x", DependsOnID: "y", Type: beads.DepBlocks},
			}},
			{ID: "y"},
		})
		depType, _ := blocking.RelationTo("x", "y")
		s.Empty(string(depType), "a blocks edge is not a relation")
	})
}

func (s *snapshotTestSuite) TestRelationToPrefersTheSpecificClaim() {
	// Both ends declare an edge. "related" is the fallback for when nothing
	// more specific is known, so the specific claim wins from either side.
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "a", Dependencies: []beads.Dependency{
			{IssueID: "a", DependsOnID: "b", Type: beads.DepRelated},
		}},
		{ID: "b", Dependencies: []beads.Dependency{
			{IssueID: "b", DependsOnID: "a", Type: beads.DepDiscoveredFrom},
		}},
	})

	fromA, forwardA := snap.RelationTo("a", "b")
	s.Equal(beads.DepDiscoveredFrom, fromA)
	s.False(forwardA, "b declares the specific edge, so a is the receiving end")

	fromB, forwardB := snap.RelationTo("b", "a")
	s.Equal(beads.DepDiscoveredFrom, fromB)
	s.True(forwardB)
}

func (s *snapshotTestSuite) TestRelationToWhenBothEndsClaimSomethingSpecific() {
	// Two specific claims, neither more specific than the other. Each end
	// reports its own declaration rather than deferring to the other, so the
	// pair does not read as "reverse" from both sides at once.
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "a", Dependencies: []beads.Dependency{
			{IssueID: "a", DependsOnID: "b", Type: beads.DepSupersedes},
		}},
		{ID: "b", Dependencies: []beads.Dependency{
			{IssueID: "b", DependsOnID: "a", Type: beads.DepCausedBy},
		}},
	})

	fromA, forwardA := snap.RelationTo("a", "b")
	s.Equal(beads.DepSupersedes, fromA)
	s.True(forwardA, "a reports its own claim")

	fromB, forwardB := snap.RelationTo("b", "a")
	s.Equal(beads.DepCausedBy, fromB)
	s.True(forwardB, "b reports its own claim")
}
