package depsview_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/depsview"
)

func TestDepsview(t *testing.T) {
	suite.Run(t, new(depsTestSuite))
}

type depsTestSuite struct {
	suite.Suite
}

func mkIssue(id string, opts ...func(*beads.Issue)) beads.Issue {
	i := beads.Issue{ID: id, Title: id, Status: beads.StatusOpen, IssueType: beads.TypeTask}
	for _, opt := range opts {
		opt(&i)
	}

	return i
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

func entryIDs(c depsview.Column) []string {
	out := make([]string, len(c.Entries))
	for i, e := range c.Entries {
		out[i] = e.ID
	}

	return out
}

func (s *depsTestSuite) TestAlwaysFourColumnsInFixedOrder() {
	for _, tc := range []struct {
		name string
		snap *beads.Snapshot
		id   string
	}{
		{name: "nil snapshot", snap: nil, id: "x"},
		{name: "empty snapshot", snap: beads.NewSnapshot(nil), id: "x"},
		{name: "unknown id", snap: beads.NewSnapshot([]beads.Issue{mkIssue("a")}), id: "nope"},
		{name: "empty id", snap: beads.NewSnapshot([]beads.Issue{mkIssue("a")}), id: ""},
	} {
		s.Run(tc.name, func() {
			cols := depsview.Columns(tc.snap, tc.id)
			s.Require().Len(cols, 4, "a missing column reads identically to an empty one")
			s.Equal("blocked by", cols[0].Title)
			s.Equal("focused", cols[1].Title)
			s.Equal("blocks", cols[2].Title)
			s.Equal("related", cols[3].Title)
		})
	}
}

func (s *depsTestSuite) TestFocusedColumnHoldsExactlyTheFocusedIssue() {
	snap := beads.NewSnapshot([]beads.Issue{mkIssue("a"), mkIssue("b")})

	cols := depsview.Columns(snap, "a")

	s.Require().Len(cols[1].Entries, 1)
	s.Equal("a", cols[1].Entries[0].ID)
	s.Require().NotNil(cols[1].Entries[0].Issue)
	s.Equal(depsview.RelationFocus, cols[1].Entries[0].Relation)
}

func (s *depsTestSuite) TestFocusedColumnIsEmptyForAnUnknownID() {
	snap := beads.NewSnapshot([]beads.Issue{mkIssue("a")})

	s.Empty(depsview.Columns(snap, "gone")[1].Entries)
}

func (s *depsTestSuite) TestBlockedByCarriesAllFourReasons() {
	snap := beads.NewSnapshot([]beads.Issue{
		// parent holds a live blocking edge of its own, so the block is
		// inherited by every descendant.
		mkIssue("parent", withDep(beads.DepBlocks, "outsider")),
		mkIssue("outsider"),
		mkIssue("focus",
			func(i *beads.Issue) {
				i.Dependencies = append(i.Dependencies, beads.Dependency{
					IssueID: "focus", DependsOnID: "parent", Type: beads.DepParentChild,
				})
			},
			withDep(beads.DepBlocks, "live", "vanished"),
		),
		mkIssue("live"),
	})

	byRelation := map[depsview.Relation][]string{}
	for _, e := range depsview.Columns(snap, "focus")[0].Entries {
		byRelation[e.Relation] = append(byRelation[e.Relation], e.ID)
	}

	s.Equal([]string{"live"}, byRelation[depsview.RelationBlocker])
	s.Equal([]string{"vanished"}, byRelation[depsview.RelationDangling])
	s.Equal([]string{"parent"}, byRelation[depsview.RelationInherited])
}

func (s *depsTestSuite) TestDanglingBlockerHasAnIDButNoIssue() {
	snap := beads.NewSnapshot([]beads.Issue{mkIssue("focus", withDep(beads.DepBlocks, "gone"))})

	entries := depsview.Columns(snap, "focus")[0].Entries
	s.Require().Len(entries, 1)
	s.Equal("gone", entries[0].ID)
	s.Nil(entries[0].Issue, "an id is all there is to say about a blocker that is not here")
	s.Equal(depsview.RelationDangling, entries[0].Relation)
}

func (s *depsTestSuite) TestOpenChildBlocksAnEpic() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("epic", func(i *beads.Issue) { i.IssueType = beads.TypeEpic }),
		mkIssue("kid", func(i *beads.Issue) {
			i.Dependencies = append(i.Dependencies, beads.Dependency{
				IssueID: "kid", DependsOnID: "epic", Type: beads.DepParentChild,
			})
		}),
	})

	entries := depsview.Columns(snap, "epic")[0].Entries
	s.Require().Len(entries, 1)
	s.Equal("kid", entries[0].ID)
	s.Equal(depsview.RelationOpenChild, entries[0].Relation)
}

func (s *depsTestSuite) TestBlocksColumnIsTheReverseIndex() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("focus"),
		mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
		mkIssue("kid", func(i *beads.Issue) {
			i.Dependencies = append(i.Dependencies, beads.Dependency{
				IssueID: "kid", DependsOnID: "focus", Type: beads.DepParentChild,
			})
		}),
	})

	s.Equal([]string{"waiter"}, entryIDs(depsview.Columns(snap, "focus")[2]),
		"a parent-child edge never blocks, so kid belongs to the tree view, not here")
}

func (s *depsTestSuite) TestRelatedColumnTagsEachEdgeKind() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("focus", withDep(beads.DepRelated, "sibling")),
		mkIssue("sibling"),
		mkIssue("spike", withDep(beads.DepDiscoveredFrom, "focus")),
	})

	byID := map[string]depsview.Relation{}
	for _, e := range depsview.Columns(snap, "focus")[3].Entries {
		byID[e.ID] = e.Relation
	}

	s.Equal(depsview.RelationRelated, byID["sibling"])
	s.Equal(depsview.RelationDiscovered, byID["spike"])
}

func (s *depsTestSuite) TestNoColumnContainsTheFocusedIssue() {
	// A self-edge is expressible in hand-edited JSONL; the focused issue must
	// appear once, in the middle column, and nowhere else.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("focus", withDep(beads.DepBlocks, "focus"), withDep(beads.DepRelated, "focus")),
	})

	cols := depsview.Columns(snap, "focus")
	for _, i := range []int{0, 2, 3} {
		s.NotContains(entryIDs(cols[i]), "focus", "column %d", i)
	}
}

func (s *depsTestSuite) TestSurvivesAParentCycle() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("a", func(i *beads.Issue) {
			i.Dependencies = append(i.Dependencies, beads.Dependency{
				IssueID: "a", DependsOnID: "b", Type: beads.DepParentChild,
			})
		}),
		mkIssue("b", func(i *beads.Issue) {
			i.Dependencies = append(i.Dependencies, beads.Dependency{
				IssueID: "b", DependsOnID: "a", Type: beads.DepParentChild,
			})
		}),
	})

	s.NotPanics(func() { _ = depsview.Columns(snap, "a") })
}
