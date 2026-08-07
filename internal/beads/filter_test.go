package beads_test

import (
	"slices"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

// Register in the entry point: suite.Run(t, new(filterTestSuite))

type filterTestSuite struct {
	suite.Suite
}

func (s *filterTestSuite) sample() *beads.Snapshot {
	return beads.NewSnapshot([]beads.Issue{
		{
			ID: "bv-1", Title: "Add tree view", Status: beads.StatusOpen,
			IssueType: beads.TypeFeature, Labels: []string{"ui", "tree"},
		},
		{
			ID: "bv-2", Title: "Fix decoder panic", Status: beads.StatusClosed,
			IssueType: beads.TypeBug, Labels: []string{"domain"},
		},
		{
			ID: "bv-3", Title: "Board columns", Status: beads.StatusInProgress,
			IssueType: beads.TypeFeature, Labels: []string{"ui"},
		},
		{ID: "bv-4", Title: "Deleted thing", Status: beads.StatusTombstone},
		{
			ID: "bv-5", Title: "TREE shouting", Status: beads.StatusOpen,
			Labels: []string{"ui", "tree"},
		},
	})
}

func (s *filterTestSuite) ids(snap *beads.Snapshot) []string {
	out := make([]string, 0, snap.Len())
	for _, i := range snap.Issues() {
		out = append(out, i.ID)
	}
	slices.Sort(out)

	return out
}

func (s *filterTestSuite) TestZeroFilterHidesOnlyTombstones() {
	got := beads.Filter{}.Apply(s.sample())
	s.Equal([]string{"bv-1", "bv-2", "bv-3", "bv-5"}, s.ids(got))
}

func (s *filterTestSuite) TestShowTombstones() {
	got := beads.Filter{ShowTombstones: true}.Apply(s.sample())
	s.Len(s.ids(got), 5)
}

func (s *filterTestSuite) TestTextMatching() {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"matches title", "tree", []string{"bv-1", "bv-5"}},
		{"is case-insensitive", "TREE", []string{"bv-1", "bv-5"}},
		{"matches id", "bv-3", []string{"bv-3"}},
		{"matches a label", "domain", []string{"bv-2"}},
		{"matches a substring mid-word", "ecoder", []string{"bv-2"}},
		{"no match yields nothing", "zzzz", nil},
		{"empty text matches all", "", []string{"bv-1", "bv-2", "bv-3", "bv-5"}},
		{"whitespace is trimmed", "  tree  ", []string{"bv-1", "bv-5"}},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			got := beads.Filter{Text: tc.text}.Apply(s.sample())
			if tc.want == nil {
				s.Equal(0, got.Len())

				return
			}
			s.Equal(tc.want, s.ids(got))
		})
	}
}

func (s *filterTestSuite) TestStatusFilter() {
	got := beads.Filter{Status: beads.StatusOpen}.Apply(s.sample())
	s.Equal([]string{"bv-1", "bv-5"}, s.ids(got))
}

func (s *filterTestSuite) TestLabelsAreConjunctive() {
	both := beads.Filter{Labels: []string{"ui", "tree"}}.Apply(s.sample())
	s.Equal([]string{"bv-1", "bv-5"}, s.ids(both))

	one := beads.Filter{Labels: []string{"ui"}}.Apply(s.sample())
	s.Equal([]string{"bv-1", "bv-3", "bv-5"}, s.ids(one))

	none := beads.Filter{Labels: []string{"ui", "nonexistent"}}.Apply(s.sample())
	s.Equal(0, none.Len())
}

func (s *filterTestSuite) TestHideClosed() {
	got := beads.Filter{HideClosed: true}.Apply(s.sample())
	s.Equal([]string{"bv-1", "bv-3", "bv-5"}, s.ids(got))
}

func (s *filterTestSuite) TestCriteriaCombine() {
	got := beads.Filter{Text: "tree", Labels: []string{"ui"}, Status: beads.StatusOpen}.
		Apply(s.sample())
	s.Equal([]string{"bv-1", "bv-5"}, s.ids(got))
}

func (s *filterTestSuite) TestHideClosedAndShowTombstonesCombine() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "t-1", Title: "deleted ui thing", Status: beads.StatusTombstone, Labels: []string{"ui"}},
		{ID: "o-1", Title: "open ui thing", Status: beads.StatusOpen, Labels: []string{"ui"}},
	})

	// ShowTombstones alone (combined with Labels) surfaces the tombstone: a
	// baseline showing the two criteria already given already combine as AND.
	withTombstones := beads.Filter{ShowTombstones: true, Labels: []string{"ui"}}.Apply(snap)
	s.Equal([]string{"o-1", "t-1"}, s.ids(withTombstones))

	// Adding HideClosed must still exclude it: a tombstone's status is
	// terminal, so HideClosed and ShowTombstones combine with AND across the
	// whole criteria set, not with ShowTombstones overriding HideClosed for
	// tombstones specifically.
	got := beads.Filter{HideClosed: true, ShowTombstones: true, Labels: []string{"ui"}}.Apply(snap)
	s.Equal([]string{"o-1"}, s.ids(got))
}

func (s *filterTestSuite) TestApplyPreservesHierarchy() {
	// A narrowed snapshot must still index parent-child edges among survivors,
	// otherwise the tree view would flatten whenever a filter is active.
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "p", Title: "parent keep", Status: beads.StatusOpen},
		{
			ID: "c", Title: "child keep", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "c", DependsOnID: "p", Type: beads.DepParentChild},
			},
		},
	})

	got := beads.Filter{Text: "keep"}.Apply(snap)
	s.Require().Equal(2, got.Len())
	s.Len(got.Children("p"), 1)
}

// TestAnyAgreesWithApply pins Any as a short-circuiting equivalent of
// checking Apply's own result for emptiness, across the same criteria the
// suite above already exercises against Apply directly — this is what
// catches an Any that (say) inverted Matches or ignored one field of it.
func (s *filterTestSuite) TestAnyAgreesWithApply() {
	cases := []beads.Filter{
		{},
		{Text: "tree"},
		{Text: "zzzz"},
		{Status: beads.StatusOpen},
		{HideClosed: true},
		{Labels: []string{"ui", "tree"}},
		{Labels: []string{"ui", "nonexistent"}},
		{ShowTombstones: true},
	}
	for _, f := range cases {
		want := f.Apply(s.sample()).Len() > 0
		s.Equal(want, f.Any(s.sample()), "Any must agree with Apply for filter %+v", f)
	}
}

// TestAnyOnAnEmptySnapshotIsFalse pins the degenerate case a purely
// short-circuiting loop could get wrong by, say, treating no iterations as a
// match.
func (s *filterTestSuite) TestAnyOnAnEmptySnapshotIsFalse() {
	s.False(beads.Filter{}.Any(beads.NewSnapshot(nil)))
}

func (s *filterTestSuite) TestIsZero() {
	s.True(beads.Filter{}.IsZero())
	s.False(beads.Filter{Text: "x"}.IsZero())
	s.False(beads.Filter{HideClosed: true}.IsZero())
	s.False(beads.Filter{Labels: []string{"ui"}}.IsZero())
	// ShowTombstones widens rather than narrows, but it is still not the
	// default view: a "clear filter" affordance gated on IsZero must offer a
	// way back to hiding tombstones again.
	s.False(beads.Filter{ShowTombstones: true}.IsZero())
}

func (s *filterTestSuite) TestDescribe() {
	s.Empty(beads.Filter{}.Describe())
	s.Contains(beads.Filter{Text: "tree"}.Describe(), `"tree"`)
	s.Contains(beads.Filter{Status: beads.StatusOpen}.Describe(), "open")
	s.Contains(beads.Filter{Labels: []string{"ui", "tree"}}.Describe(), "ui+tree")
	// Deletion markers are an active, non-default state; the status bar must
	// say so, or unfamiliar rows on screen have no explanation.
	s.Contains(beads.Filter{ShowTombstones: true}.Describe(), "+tombstones")
	s.NotContains(beads.Filter{Text: "tree"}.Describe(), "tombstones")
}

// TestTextMatchingIsUnicodeAware proves case folding is rune-wise, not
// byte-wise. Lower-casing by byte (`s[:1]`) is exactly what corrupted "任务"
// elsewhere in this package (see IssueType.Display) — the same bug here would
// make an upper-case non-ASCII query silently fail to match its own lower-case
// title.
func (s *filterTestSuite) TestTextMatchingIsUnicodeAware() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "bv-6", Title: "Исправить дерево задач", Status: beads.StatusOpen},
	})

	upper := beads.Filter{Text: "ДЕРЕВО"}.Apply(snap)
	s.Equal([]string{"bv-6"}, s.ids(upper))

	lower := beads.Filter{Text: "дерево"}.Apply(snap)
	s.Equal([]string{"bv-6"}, s.ids(lower))
}

// TestApplyPreservesSnapshotOrder proves Apply narrows without re-sorting: the
// survivors come out in the snapshot's own priority/created/id order, not
// insertion order and not alphabetical order, and nothing about map iteration
// leaks into the result.
func (s *filterTestSuite) TestApplyPreservesSnapshotOrder() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "z-low", Title: "keep low", Status: beads.StatusOpen, Priority: beads.PriorityLow},
		{ID: "a-crit", Title: "keep crit", Status: beads.StatusOpen, Priority: beads.PriorityCritical},
		{ID: "m-high", Title: "keep high", Status: beads.StatusOpen, Priority: beads.PriorityHigh},
		{ID: "drop-me", Title: "excluded", Status: beads.StatusOpen, Priority: beads.PriorityMedium},
	})

	got := beads.Filter{Text: "keep"}.Apply(snap)

	gotIDs := make([]string, 0, got.Len())
	for _, i := range got.Issues() {
		gotIDs = append(gotIDs, i.ID)
	}
	s.Equal([]string{"a-crit", "m-high", "z-low"}, gotIDs)
}

func (s *filterTestSuite) TestTextMatchesAFormerID() {
	issue := &beads.Issue{ID: "e-1", Title: "unrelated", FormerIDs: []string{"old-1"}}

	s.True(beads.Filter{Text: "old-1"}.Matches(issue))
	s.True(beads.Filter{Text: "OLD-1"}.Matches(issue), "matching is case-insensitive")
	s.False(beads.Filter{Text: "old-2"}.Matches(issue))
}
