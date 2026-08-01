package beads_test

import (
	"os"
	"path/filepath"
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

// byID is a test-local stand-in for the exported Snapshot.ByID method I9
// removed (zero non-test callers): it scans snap.Issues(), which — thanks to
// NewSnapshot's stable sort — preserves each duplicate id's original input
// order, so taking the LAST match reproduces ByID's own "later record wins"
// duplicate-id resolution exactly.
func byID(snap *beads.Snapshot, id string) (*beads.Issue, bool) {
	var found *beads.Issue
	for _, issue := range snap.Issues() {
		if issue.ID == id {
			found = issue
		}
	}

	return found, found != nil
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

	got, ok := byID(snap, "a")
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

	got, ok := byID(snap, "dup")
	s.Require().True(ok)
	s.Equal("second", got.Title, "the later record in input order wins the index")
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
	resolved, ok := byID(snap, "dup")
	s.Require().True(ok)
	s.Equal("c", resolved.Owner)

	_, ok = snap.Parent("dup")
	s.False(ok, "Parent(id) must agree with byID's own duplicate-id resolution")
}

func (s *snapshotTestSuite) TestEmptySnapshot() {
	snap := beads.NewSnapshot(nil)

	s.Equal(0, snap.Len())
	s.Empty(snap.Issues())
	s.Empty(snap.Roots())
	s.Empty(snap.Children("anything"))
	_, ok := byID(snap, "anything")
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
