package depsview_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/cardfmt"
	"github.com/Toshik1978/beads-viewer/internal/tui/depsview"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

func TestDepsview(t *testing.T) {
	suite.Run(t, new(depsTestSuite))
	suite.Run(t, new(depsview.WhiteBoxSuite))
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

func (s *depsTestSuite) TestFocusedColumnIsEmptyForAnEmptyID() {
	snap := beads.NewSnapshot([]beads.Issue{mkIssue("a")})

	s.Empty(depsview.Columns(snap, "")[1].Entries, "an empty id has no subject to name")
}

// TestFocusedColumnNamesTheSubjectEvenWhenItHasLeftTheSnapshot pins the fix
// for a focus that leaves the snapshot: focusEntries used to return nothing,
// so a filtered-out or reloaded-away focus left "focused (0)" beside a
// populated "blocks" or "related" column with nothing on screen naming what
// those columns were about. It now renders the same way a dangling blocker
// does — a bare tagged id, with no *beads.Issue behind it.
func (s *depsTestSuite) TestFocusedColumnNamesTheSubjectEvenWhenItHasLeftTheSnapshot() {
	snap := beads.NewSnapshot([]beads.Issue{mkIssue("a")})

	entries := depsview.Columns(snap, "gone")[1].Entries
	s.Require().Len(entries, 1)
	s.Equal("gone", entries[0].ID)
	s.Nil(entries[0].Issue, "the view is about an id this snapshot no longer has an issue for")
	s.Equal(depsview.RelationNotInView, entries[0].Relation)
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

// TestBlockedByCollapsesAnExactDuplicateButNotATwoRelationCase pins Minor 5.
// Snapshot.Blockers is not deduped at the source the way Snapshot.dependents
// is (see snapshot.go), so a "blocks" edge and a "waits-for" edge to the same
// target both resolve to RelationBlocker here and produced two identical
// cards. The other half of the fixture proves the fix does not overreach:
// "parent" is both a direct blocker (RelationBlocker) and the inherited
// ancestor (RelationInherited) — two genuine facts under two different
// relations — and must still appear twice.
func (s *depsTestSuite) TestBlockedByCollapsesAnExactDuplicateButNotATwoRelationCase() {
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("outsider"),
		mkIssue("parent", withDep(beads.DepBlocks, "outsider")),
		mkIssue("focus",
			func(i *beads.Issue) {
				i.Dependencies = append(i.Dependencies, beads.Dependency{
					IssueID: "focus", DependsOnID: "parent", Type: beads.DepParentChild,
				})
			},
			withDep(beads.DepBlocks, "double"),
			withDep(beads.DepWaitsFor, "double"),
			withDep(beads.DepBlocks, "parent"),
		),
		mkIssue("double"),
	})

	entries := depsview.Columns(snap, "focus")[0].Entries

	doubleCount, parentBlockerCount, parentInheritedCount := 0, 0, 0
	for _, e := range entries {
		switch {
		case e.ID == "double":
			doubleCount++
		case e.ID == "parent" && e.Relation == depsview.RelationBlocker:
			parentBlockerCount++
		case e.ID == "parent" && e.Relation == depsview.RelationInherited:
			parentInheritedCount++
		}
	}

	s.Equal(1, doubleCount, "a blocks edge and a waits-for edge to the same target must collapse to one card")
	s.Equal(1, parentBlockerCount, "the direct blocker fact must still appear")
	s.Equal(1, parentInheritedCount, "the inherited fact is a different relation and must not be dropped")
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
	// spike declares "spike discovered-from focus" — spike is the declaring
	// end, so on focus's own pane (the receiving end) the label is the
	// reverse word: focus led to spike's discovery, not the other way round.
	s.Equal(depsview.RelationLedTo, byID["spike"])
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
	// discovered-from is the more specific claim and wins a contested pair
	// regardless of which side declares it — but which side declares it still
	// decides whether focus's own pane reads the forward or reverse word.
	for _, tc := range []struct {
		name          string
		focusDeclares beads.DepType
		otherDeclares beads.DepType
		want          depsview.Relation
	}{
		{
			name:          "focus says related, other says discovered-from",
			focusDeclares: beads.DepRelated,
			otherDeclares: beads.DepDiscoveredFrom,
			want:          depsview.RelationLedTo,
		},
		{
			name:          "focus says discovered-from, other says related",
			focusDeclares: beads.DepDiscoveredFrom,
			otherDeclares: beads.DepRelated,
			want:          depsview.RelationDiscovered,
		},
	} {
		s.Run(tc.name, func() {
			snap := beads.NewSnapshot([]beads.Issue{
				mkIssue("focus", withDep(tc.focusDeclares, "other")),
				mkIssue("other", withDep(tc.otherDeclares, "focus")),
			})

			entries := depsview.Columns(snap, "focus")[3].Entries
			s.Require().Len(entries, 1)
			s.Equal(tc.want, entries[0].Relation)
		})
	}
}

func (s *depsTestSuite) TestRelationLabelsFollowDirection() {
	cases := []struct {
		depType beads.DepType
		forward depsview.Relation
		reverse depsview.Relation
	}{
		{beads.DepRelatesTo, depsview.RelationRelated, depsview.RelationRelated},
		{beads.DepDiscoveredFrom, depsview.RelationDiscovered, depsview.RelationLedTo},
		{beads.DepDuplicates, depsview.RelationDuplicates, depsview.RelationDuplicatedBy},
		{beads.DepSupersedes, depsview.RelationSupersedes, depsview.RelationSupersededBy},
		{beads.DepCausedBy, depsview.RelationCausedBy, depsview.RelationCaused},
		{beads.DepRepliesTo, depsview.RelationRepliesTo, depsview.RelationReply},
	}

	for _, tc := range cases {
		s.Run(string(tc.depType), func() {
			snap := beads.NewSnapshot([]beads.Issue{
				{ID: "a", Dependencies: []beads.Dependency{
					{IssueID: "a", DependsOnID: "b", Type: tc.depType},
				}},
				{ID: "b"},
			})

			// The related column is the fourth Columns returns.
			fromA := depsview.Columns(snap, "a")[3].Entries
			s.Require().Len(fromA, 1)
			s.Equal(tc.forward, fromA[0].Relation, "declaring end")

			fromB := depsview.Columns(snap, "b")[3].Entries
			s.Require().Len(fromB, 1)
			s.Equal(tc.reverse, fromB[0].Relation, "receiving end")
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

// TestGoldenRendering pins depsview's own frame byte-for-byte, the gap
// bv-7pt's own acceptance criteria called for: listview, treeview and
// boardview each have a golden test and this package alone did not. Column
// geometry, heading padding, the inter-column gap width and where "+N more"
// lands were pinned only by Contains assertions until now — none of which
// can catch a shift in alignment or spacing the way a byte-exact frame can.
//
// The golden file stores the ANSI-stripped frame, per the house rule (see
// boardview's identical note): an escape-laden golden is unreviewable, and
// one nobody can read gets regenerated on sight rather than checked.
// Styling stays asserted separately —
// TestSelectedCardRendersDifferentlyFromUnselected, above, already covers
// selection.
func (s *depsTestSuite) TestGoldenRendering() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	actual := ansi.Strip(m.View())
	s.Equal(golden(s.T(), "deps_120x20.golden", actual), actual)
}

func (s *depsTestSuite) TestViewFitsItsAllottedGeometry() {
	m := depsview.New(s.th())
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	// empty records whether width alone forces View to return "" at this
	// size: columnWidth floors at 0 below 7 columns' worth of width (three
	// unit gaps plus one cell per column), so {0,0} and {1,1} are expected
	// to render nothing, not merely permitted to. Asserting that directly —
	// rather than branching on whatever View happened to return — is the
	// difference between a subtest that can fail and one that cannot.
	for _, tc := range []struct {
		width, height int
		empty         bool
	}{
		{0, 0, true},
		{1, 1, true},
		{20, 3, false},
		{40, 10, false},
		{80, 24, false},
		{200, 60, false},
	} {
		s.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func() {
			m.SetSize(tc.width, tc.height)
			out := ansi.Strip(m.View())

			if tc.empty {
				s.Empty(out, "a degenerate size must render nothing, not a truncated fragment")

				return
			}

			lines := strings.Split(out, "\n")
			s.LessOrEqual(len(lines), tc.height, "pane is taller than its budget at %dx%d", tc.width, tc.height)
			for _, line := range lines {
				s.LessOrEqual(lipgloss.Width(line), tc.width, "line %q exceeds width at %dx%d", line, tc.width,
					tc.height)
			}
		})
	}
}

func (s *depsTestSuite) TestRelatedColumnBudgetsLabelledCardsHonestly() {
	// Every "related" entry is labelled (RelationRelated needs a label the
	// column heading doesn't already say), so each one costs
	// cardfmt.Height(false)+1 lines, not cardfmt.Height(false) alone. A
	// budget that divided avail by the smaller, unlabelled cost believed all
	// five fit here — 20/4 == 5, and len(entries) == 5 is not ">" 5, so the
	// old code never reserved a row for "+N more" either — while their real
	// cost (5*5 = 25) ran past the column's actual budget. clip's tail-chop
	// still held the outer pane to m.height, but by silently cutting into
	// the related column's own content instead of reporting the drop, which
	// is what the "+N more" marker exists to do everywhere else.
	issues := []beads.Issue{mkIssue("focus")}
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("rel%d", i)
		issues[0].Dependencies = append(issues[0].Dependencies, beads.Dependency{
			IssueID: "focus", DependsOnID: id, Type: beads.DepRelated,
		})
		issues = append(issues, mkIssue(id))
	}

	m := depsview.New(s.th())
	m.SetSize(200, 21)
	m.SetSnapshot(beads.NewSnapshot(issues))
	s.Require().True(m.Reveal("focus"))

	out := ansi.Strip(m.View())
	lines := strings.Split(out, "\n")
	s.LessOrEqual(len(lines), 21, "pane must not exceed its height budget")
	s.Contains(out, "+2 more", "a column that had to drop entries must say so — this is the important half: "+
		"clip() alone already makes the height bound pass, which is why the old bug was invisible")
}

// TestPlusMoreCountsOnlyWhatIsHiddenBelow pins Minor 6. Eight unlabelled
// blockers (RelationBlocker, cardfmt.Height(false) == 4 rows each, no label
// line) at a height that only fits three at a time forces the cursor,
// jumped to the last entry, to scroll the window to [5, 8) — five entries
// hidden above the window, none below. The old "+N more" counted both:
// len(Entries)-(end-start) == 8-3 == 5, so it read "+5 more" even though
// nothing was actually below the visible window to scroll down to.
func (s *depsTestSuite) TestPlusMoreCountsOnlyWhatIsHiddenBelow() {
	issues := []beads.Issue{mkIssue("focus")}
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("b%d", i)
		issues[0].Dependencies = append(issues[0].Dependencies, beads.Dependency{
			IssueID: "focus", DependsOnID: id, Type: beads.DepBlocks,
		})
		issues = append(issues, mkIssue(id))
	}

	m := depsview.New(s.th())
	// avail = height - headerLines(1) = 13; the scroll budget (avail-1) is
	// 12, exactly three unlabelled cards (3*4), so the window can show three
	// of the eight blockers at a time.
	m.SetSize(200, 14)
	m.SetSnapshot(beads.NewSnapshot(issues))
	s.Require().True(m.Reveal("focus"))

	m.MoveLeft() // onto "blocked by", the only populated column besides "focused"
	s.Require().Equal("b1", m.SelectedID())
	m.JumpToBottom() // scrolls the window to the last three blockers

	out := ansi.Strip(m.View())
	s.NotContains(out, "more",
		"nothing is hidden below the last visible blocker; everything hidden scrolled off the top")
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
	// Styling asserted separately from content, per the house rule — and
	// specifically the selection styling, not merely "the frame has ANSI in
	// it somewhere" (cardfmt's own border alone would satisfy that, whether
	// or not the cursor's position was ever threaded through).
	//
	// Reveal always parks the cursor on the newly revealed subject (there is
	// no cursor-movement method yet, that is the next task), so the only way
	// to see the same card rendered both selected and unselected in this
	// task is to reveal it once directly, and once by revealing a neighbour
	// that pulls the same issue into a different, unselected column. "focus"
	// plays both parts: revealed, it is the sole selected card; with "live"
	// revealed instead, "focus" is live's blocker and appears — same issue,
	// same width, same theme — as a plain, unselected card in blocked-by.
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("focus"),
		mkIssue("live", withDep(beads.DepBlocks, "focus")),
	})
	const width = 100
	// Mirrors depsview's own unexported columnWidth: (width -
	// columnGap*(columnCount-1)) / columnCount, with columnGap=1 and
	// columnCount=4. There is no exported way to ask the model for the
	// width it renders cards at, so reconstructing cardfmt.Render's
	// expected output requires knowing this.
	colWidth := (width - 3) / 4

	m := depsview.New(s.th())
	m.SetSize(width, 20)
	m.SetSnapshot(snap)

	s.Require().True(m.Reveal("focus"))
	selectedFrame := m.View()

	s.Require().True(m.Reveal("live"))
	unselectedFrame := m.View()

	focusIssue, ok := snap.ByID("focus")
	s.Require().True(ok)
	selectedCard := cardfmt.Render(s.th(), snap, focusIssue, colWidth, true, false)
	unselectedCard := cardfmt.Render(s.th(), snap, focusIssue, colWidth, false, false)

	s.NotEqual(selectedCard, unselectedCard, "selection must change the card's own styling")
	s.Equal(ansi.Strip(selectedCard), ansi.Strip(unselectedCard), "selection is styling, not content")

	// Only the content rows (the id/priority line and the title line) are
	// checked, not the border rows: a bordered box's top and bottom rows are
	// just repeated "─" styled by selection state alone, with nothing
	// issue-specific in them, so a selected border row from "live"'s own
	// card (genuinely selected in unselectedFrame) would match a selected
	// border row asserted absent for "focus" even though they belong to two
	// different cards — the content rows carry the id and are what actually
	// pins the check to "focus" specifically. Rows 1 and 2 are the header
	// and title lines; 0 and 3 are the top and bottom border.
	selectedContent := strings.Split(selectedCard, "\n")[1:3]
	unselectedContent := strings.Split(unselectedCard, "\n")[1:3]

	for _, line := range selectedContent {
		s.Contains(selectedFrame, line, "the revealed subject's card must render selected")
		s.NotContains(unselectedFrame, line, "focus is no longer under the cursor once live is revealed")
	}
	for _, line := range unselectedContent {
		s.NotContains(selectedFrame, line, "focus must not render as selected in a frame where it is the cursor")
		s.Contains(unselectedFrame, line, "focus renders unselected once it is no longer under the cursor")
	}
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

func (s *depsTestSuite) TestEnterPromotesTheHighlightedCardToTheFocus() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	// Cursor onto the "blocks" column, whose sole entry is waiter.
	m.MoveRight()
	s.Require().Equal("waiter", m.SelectedID())

	m.Descend()

	s.Equal("waiter", m.FocusID())
}

// TestRevealFromOutsideClearsHistory pins Minor 4: Reveal is what a view
// switch calls to land this view on another view's selection — an entry
// from outside this view's own walk — and must not leave a stale walk
// behind for backspace to retrace. Before the fix, re-rooting on "z" this
// way left history exactly as Descend had built it ([a, b]), so Back landed
// on "b", an issue this visit never passed through.
func (s *depsTestSuite) TestRevealFromOutsideClearsHistory() {
	full := beads.NewSnapshot([]beads.Issue{
		mkIssue("a"),
		mkIssue("b", withDep(beads.DepBlocks, "a")),
		mkIssue("c", withDep(beads.DepBlocks, "b")),
		mkIssue("z"),
	})

	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(full)
	s.Require().True(m.Reveal("a"))

	// Walk a -> b -> c via Descend, exactly as TestBackSkipsAStaleIntermediate-
	// AndLandsOnTheSurvivor does, so history is [a, b] afterward.
	m.MoveRight()
	m.Descend()
	s.Require().Equal("b", m.FocusID())
	m.MoveRight()
	m.Descend()
	s.Require().Equal("c", m.FocusID())

	// Simulates tui/keys.go's carrySelection landing this view on an
	// unrelated issue after a view switch — the "press 1, move to Z, press
	// 4" step from the review.
	s.Require().True(m.Reveal("z"))

	m.Back()

	s.Equal(
		"z",
		m.FocusID(),
		"an external Reveal must clear history, or backspace retraces a walk this visit never took",
	)
}

func (s *depsTestSuite) TestBackWalksTheChainAndThenStops() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	m.MoveRight()
	m.Descend()
	s.Require().Equal("waiter", m.FocusID())

	m.Back()
	s.Equal("focus", m.FocusID())

	m.Back()
	s.Equal("focus", m.FocusID(), "an exhausted history is a no-op, not a reset")
}

func (s *depsTestSuite) TestBackSkipsAStaleIntermediateAndLandsOnTheSurvivor() {
	// A three-issue chain: focus -> waiter -> waiter2, each blocking the
	// previous, so Descend can walk it via the "blocks" column at every step.
	full := beads.NewSnapshot([]beads.Issue{
		mkIssue("focus"),
		mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
		mkIssue("waiter2", withDep(beads.DepBlocks, "waiter")),
	})

	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(full)
	s.Require().True(m.Reveal("focus"))

	m.MoveRight()
	m.Descend()
	s.Require().Equal("waiter", m.FocusID())

	m.MoveRight()
	m.Descend()
	s.Require().Equal("waiter2", m.FocusID(), "history is now [focus, waiter], most recent last")

	// The shared filter (or a reload) has since dropped "waiter" — the top
	// of the history stack, and the very next id Back would try — while
	// "focus", one hop further down, survives untouched.
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		mkIssue("focus"),
		mkIssue("waiter2", withDep(beads.DepBlocks, "focus")),
	}))

	m.Back()

	s.Equal("focus", m.FocusID(),
		"a stale history entry must be popped and skipped, not stop the walk")
}

func (s *depsTestSuite) TestBackExhaustsAnAllStaleHistoryWithoutPanicking() {
	full := beads.NewSnapshot([]beads.Issue{
		mkIssue("focus"),
		mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
		mkIssue("waiter2", withDep(beads.DepBlocks, "waiter")),
	})

	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(full)
	s.Require().True(m.Reveal("focus"))

	m.MoveRight()
	m.Descend()
	m.MoveRight()
	m.Descend()
	s.Require().Equal("waiter2", m.FocusID(), "history is now [focus, waiter]")

	// A reload drops every id the history remembers, leaving only the
	// current focus behind.
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{mkIssue("waiter2")}))

	s.NotPanics(m.Back)
	s.Equal("waiter2", m.FocusID(), "an exhausted, all-stale history leaves the focus untouched")
}

func (s *depsTestSuite) TestMovingColumnsResetsTheRowRatherThanPreservingIt() {
	// "blocked by" and "focused" both read the focus issue's own identity
	// (Blockers needs its Dependencies; focusEntries needs ByID to resolve
	// it), so dropping "focus" itself empties both — while "blocks" and
	// "related" are reverse indexes keyed by the id string and survive,
	// putting two multi-entry columns directly adjacent once the empty pair
	// is skipped. That is what lets a single MoveRight cross from one
	// multi-row column straight into another, with nothing in between to
	// reset the row incidentally via clamp.
	full := beads.NewSnapshot([]beads.Issue{
		mkIssue("focus"),
		mkIssue("w1", withDep(beads.DepBlocks, "focus")),
		mkIssue("w2", withDep(beads.DepBlocks, "focus")),
		mkIssue("r1", withDep(beads.DepRelated, "focus")),
		mkIssue("r2", withDep(beads.DepRelated, "focus")),
	})

	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(full)
	s.Require().True(m.Reveal("focus"))

	narrow := beads.NewSnapshot([]beads.Issue{
		mkIssue("w1", withDep(beads.DepBlocks, "focus")),
		mkIssue("w2", withDep(beads.DepBlocks, "focus")),
		mkIssue("r1", withDep(beads.DepRelated, "focus")),
		mkIssue("r2", withDep(beads.DepRelated, "focus")),
	})
	m.SetSnapshot(narrow)
	relatedFirst := depsview.Columns(narrow, "focus")[3].Entries[0].ID

	m.MoveRight() // "blocked by" and "focused" are both empty; lands on "blocks", row 0.
	m.MoveDown()  // row 1 within "blocks".
	m.MoveRight() // "related": must land on row 0, not carry row 1 over from "blocks".

	s.Equal(relatedFirst, m.SelectedID(), "a column change must reset the row, not preserve it")
}

func (s *depsTestSuite) TestDescendOnADanglingEntryIsANoOp() {
	// A dangling blocker names an id this workspace has no issue for; there is
	// nothing to re-root on, and re-rooting on it would render four empty
	// columns with no way back except backspace.
	snap := beads.NewSnapshot([]beads.Issue{mkIssue("focus", withDep(beads.DepBlocks, "gone"))})

	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(snap)
	s.Require().True(m.Reveal("focus"))

	m.MoveLeft()
	s.Require().Equal("gone", m.SelectedID())

	m.Descend()
	s.Equal("focus", m.FocusID())
}

func (s *depsTestSuite) TestMovementSkipsEmptyColumns() {
	// Only "focused" and "blocks" are populated here, so one MoveRight from
	// the focus column must land on blocks, skipping nothing, and MoveLeft
	// from there must come back rather than parking on empty "blocked by".
	snap := beads.NewSnapshot([]beads.Issue{
		mkIssue("focus"),
		mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
	})

	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(snap)
	s.Require().True(m.Reveal("focus"))
	s.Require().Equal("focus", m.SelectedID())

	m.MoveRight()
	s.Equal("waiter", m.SelectedID())

	m.MoveLeft()
	s.Equal("focus", m.SelectedID(), "the empty blocked-by column is skipped, not parked on")
}

func (s *depsTestSuite) TestUpdateRoutesKeysToMovement() {
	m := depsview.New(s.th())
	m.SetSize(120, 20)
	m.SetSnapshot(s.sample())
	s.Require().True(m.Reveal("focus"))

	before := m.SelectedID()
	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	s.NotEqual(before, m.SelectedID(), "l must move the cursor right")
}

func (s *depsTestSuite) TestHelpKeysCoversEveryBinding() {
	keys := depsview.HelpKeys()
	for _, want := range []string{
		"up", "k", "down", "j", "left", "h", "right", "l", "enter",
		"backspace",
	} {
		s.Contains(keys, want)
	}
}

// golden reads a golden file from testdata, or writes actual to it when the
// UPDATE_GOLDEN environment variable is set. Mirrors internal/tui/boardview,
// internal/tui/listview and internal/tui/treeview's helper of the same name
// and shape.
func golden(t *testing.T, name, actual string) string {
	t.Helper()

	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(actual), 0o600); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}

		return actual
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}

	return string(b)
}
