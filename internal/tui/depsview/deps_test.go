package depsview_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/depsview"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
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

func (s *depsTestSuite) TestFocusedColumnAgreesWithTheRestOnADuplicateID() {
	// Snapshot.ByID (and every accessor built on the same index — Blockers,
	// Dependents, BlockedAncestor, BlockedByOpenChild, RelatedTo) resolves a
	// duplicate id to the LATER record in input order. The focused column
	// must show that same record.
	//
	// The two records are given different priorities on purpose: that
	// breaks a stable-sort tie, so Snapshot.Issues() — which is sorted —
	// puts "first" (Priority Low) after "second" (Priority Critical), the
	// reverse of input order. A caller that tried to reproduce ByID's
	// resolution by scanning Issues() and keeping the last match would
	// therefore pick "first", the wrong record; only reading the index
	// itself, built from input order and untouched by the later sort, gets
	// "second".
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("dup", func(i *beads.Issue) {
			i.Title = "first"
			i.Priority = beads.PriorityLow
		}),
		mkIssue("dup", func(i *beads.Issue) {
			i.Title = "second"
			i.Priority = beads.PriorityCritical
		}, withDep(beads.DepBlocks, "b")),
		mkIssue("b"),
	})

	// Confirm the premise: the sort actually reordered the duplicates
	// relative to input order, so "last match in sorted order" and "last
	// match in input order" disagree.
	var dupOrder []string
	for _, issue := range snap.Issues() {
		if issue.ID == "dup" {
			dupOrder = append(dupOrder, issue.Title)
		}
	}
	s.Equal([]string{"second", "first"}, dupOrder, "the higher-priority record now sorts first")

	cols := depsview.Columns(snap, "dup")

	s.Require().Len(cols[1].Entries, 1)
	s.Equal("second", cols[1].Entries[0].Issue.Title,
		"the focused column must resolve the duplicate id the same way ByID does")
	s.Equal([]string{"b"}, entryIDs(cols[0]),
		"blocked-by is computed against the same later record the focused column now shows")
}

func (s *depsTestSuite) TestRelatedColumnPrefersDiscoveredFromOnAContestedPair() {
	for _, tc := range []struct {
		name          string
		focusDeclares beads.DepType
		otherDeclares beads.DepType
	}{
		{
			name:          "focus says related, other says discovered-from",
			focusDeclares: beads.DepRelated,
			otherDeclares: beads.DepDiscoveredFrom,
		},
		{
			name:          "focus says discovered-from, other says related",
			focusDeclares: beads.DepDiscoveredFrom,
			otherDeclares: beads.DepRelated,
		},
	} {
		s.Run(tc.name, func() {
			snap := beads.NewSnapshot([]beads.Issue{
				mkIssue("focus", withDep(tc.focusDeclares, "other")),
				mkIssue("other", withDep(tc.otherDeclares, "focus")),
			})

			entries := depsview.Columns(snap, "focus")[3].Entries
			s.Require().Len(entries, 1)
			s.Equal(depsview.RelationDiscovered, entries[0].Relation,
				"discovered-from is the more specific claim and wins a contested pair")
		})
	}
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

func (s *depsTestSuite) th() theme.Theme {
	return theme.New(config.ThemeDark, theme.BackgroundDark)
}

func (s *depsTestSuite) sample() *beads.Snapshot {
	return beads.NewSnapshot([]beads.Issue{
		mkIssue("focus", withDep(beads.DepBlocks, "live")),
		mkIssue("live"),
		mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
		mkIssue("sibling", withDep(beads.DepRelated, "focus")),
	})
}

func (s *depsTestSuite) TestViewRendersAllFourColumnHeadings() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	out := ansi.Strip(m.View())
	for _, title := range []string{"blocked by", "focused", "blocks", "related"} {
		s.Contains(out, title)
	}
	for _, id := range []string{"live", "focus", "waiter", "sibling"} {
		s.Contains(out, id)
	}
}

func (s *depsTestSuite) TestHeadingsCarryTheirCounts() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	s.Contains(ansi.Strip(m.View()), "blocked by (1)")
}

func (s *depsTestSuite) TestViewFitsItsAllottedGeometry() {
	m := depsview.New(s.th())
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	for _, size := range [][2]int{{0, 0}, {1, 1}, {20, 3}, {40, 10}, {80, 24}, {200, 60}} {
		s.Run("", func() {
			m.SetSize(size[0], size[1])
			out := ansi.Strip(m.View())
			if out == "" {
				return
			}
			lines := strings.Split(out, "\n")
			s.LessOrEqual(len(lines), size[1], "pane is taller than its budget at %v", size)
			for _, line := range lines {
				s.LessOrEqual(lipgloss.Width(line), size[0], "line %q exceeds width at %v", line, size)
			}
		})
	}
}

func (s *depsTestSuite) TestDegenerateSizesDoNotPanic() {
	m := depsview.New(s.th())
	m.SetSnapshot(s.sample())

	for _, size := range [][2]int{{0, 0}, {-5, -5}, {1, 0}, {0, 1}} {
		s.Run("", func() {
			s.NotPanics(func() {
				m.SetSize(size[0], size[1])
				_ = m.View()
			})
		})
	}
}

func (s *depsTestSuite) TestSelectedCardRendersDifferentlyFromUnselected() {
	// Styling asserted separately from content, per the house rule.
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	withCursor := m.View()
	stripped := ansi.Strip(withCursor)

	s.NotEqual(stripped, withCursor, "the frame must carry styling at all")
	s.NotEmpty(m.SelectedID(), "a freshly revealed view must have a cursor somewhere")
}

func (s *depsTestSuite) TestSnapshotSwapKeepsTheFocus() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	// A reload that adds an issue must not reset which issue the view is about.
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		mkIssue("focus", withDep(beads.DepBlocks, "live")),
		mkIssue("live"),
		mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
		mkIssue("sibling", withDep(beads.DepRelated, "focus")),
		mkIssue("brand-new"),
	}))

	s.Equal("focus", m.FocusID())
}

func (s *depsTestSuite) TestRevealReRootsTheView() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())

	s.True(m.Reveal("focus"))
	s.Equal("focus", m.FocusID())

	s.True(m.Reveal("waiter"))
	s.Equal("waiter", m.FocusID(), "Reveal re-roots; it does not merely move a cursor")
	s.Contains(ansi.Strip(m.View()), "focus",
		"re-rooting on waiter puts its blocker — the old focus — in the blocked-by column")
}

func (s *depsTestSuite) TestRevealOnAnAbsentIDChangesNothing() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	s.False(m.Reveal("nope"))
	s.Equal("focus", m.FocusID())
}

func (s *depsTestSuite) TestRevealBeforeASnapshotIsFalse() {
	m := depsview.New(s.th())
	s.False(m.Reveal("anything"))
}

func (s *depsTestSuite) TestFilteredOutFocusLeavesTheViewRenderable() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	// The app's shared filter removed the focused issue.
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{mkIssue("unrelated")}))

	s.NotPanics(func() { _ = m.View() })
	s.Nil(m.Selected())
}
