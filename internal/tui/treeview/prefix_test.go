package treeview_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
)

type prefixTestSuite struct {
	suite.Suite
}

func (s *prefixTestSuite) TestPrefix() {
	cases := []struct {
		name                 string
		ancestors            []bool
		isLast               bool
		expandable, expanded bool
		want                 string
	}{
		// A root has no ancestors: no bars, and no connector either, since it
		// has no parent to branch from. isLast is irrelevant — the two roots
		// of a forest must agree in width.
		{"root leaf", nil, true, false, false, "  "},
		{"root leaf, not the last root", nil, false, false, false, "  "},
		{"root expanded", nil, true, true, true, "▾ "},
		{"root collapsed", nil, true, true, false, "▸ "},
		// Depth 1: one ancestor, the root, which draws a bar like any other.
		// A lone root has no later sibling, so the bar is blank...
		{"first child of two", []bool{false}, false, false, false, "  ├─  "},
		// ...but in a forest with another root still to come, it is drawn.
		{"first child, root has a sibling", []bool{true}, false, false, false, "│ ├─  "},
		{"last child", []bool{false}, true, false, false, "  └─  "},
		// Depth 2: two bars. This is the case the earlier draft got wrong —
		// the second entry is the IMMEDIATE PARENT, and its continuation guide
		// is exactly what must appear beside this row.
		{"nested under a continuing parent", []bool{false, true}, false, false, false, "  │ ├─  "},
		// And stops when the parent has nothing after it.
		{"nested under a finished parent", []bool{false, false}, false, false, false, "    ├─  "},
		// Depth 4: four bars, one per ancestor, then the connector.
		{"deep mixed", []bool{true, false, true, false}, true, true, false, "│   │   └─▸ "},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, treeview.Prefix(tc.ancestors, tc.isLast, tc.expandable, tc.expanded))
		})
	}
}

// TestPrefixWidthIsUniformPerDepth pins both that width is uniform within a
// depth AND what that width actually is: a Prefix that widened every root by
// two extra cells (the bug the brief's "root exception must not be
// conditioned on isLast" note warns about) would still pass a uniform-width
// check taken alone, since every root would widen identically. Checking the
// concrete formula (2*depth+4, or 2 at the root) is what catches that. Every
// combination of ancestor bits is exercised, not all-false: an all-false
// ancestors slice can only ever produce blank guideBlank units, so it cannot
// catch a Prefix that mishandles guideVertical's width.
func (s *prefixTestSuite) TestPrefixWidthIsUniformPerDepth() {
	for depth := range 5 {
		widths := map[int]bool{}
		for bits := range 1 << depth {
			ancestors := make([]bool, depth)
			for i := range ancestors {
				ancestors[i] = bits&(1<<i) != 0
			}
			for _, isLast := range []bool{true, false} {
				for _, expandable := range []bool{true, false} {
					for _, expanded := range []bool{true, false} {
						p := treeview.Prefix(ancestors, isLast, expandable, expanded)
						widths[lipgloss.Width(p)] = true
					}
				}
			}
		}
		s.Require().Len(widths, 1, "prefixes at depth %d have differing widths", depth)

		want := 2
		if depth > 0 {
			want = 2*depth + 4
		}
		for got := range widths {
			s.Equal(want, got, "prefix width at depth %d", depth)
		}
	}
}

// AMENDMENT — do NOT call lipgloss.SetColorProfile; it does not exist.
//
// `SetColorProfile` and `lipgloss.Ascii` were v1 API and are absent from
// lipgloss v2.0.5. v2 has no global renderer: `Style.Render` always emits the
// full escape sequence and downsampling happens at the writer, so its output
// is already identical under CI, tmux and a local TTY. The golden below
// compares `ansi.Strip(out)`, per the Global Constraints.
//
// This is the brief's own worked example — the acceptance test for Prefix,
// not decoration. A prior implementation drew bars from
// ancestors[:len(ancestors)-1], excluding the immediate parent's slot; that
// passed every case in TestPrefix's table (which never exercised a bar
// belonging to a node's own direct children) while rendering this exact tree
// wrong: t1/t2 lost the "│" beside f1's continuation, and f1 was not indented
// under epic at all. Keeping this fixture pinned here — not just covered by
// the golden files below — is what makes that specific failure mode
// unable to land again silently.
func (s *prefixTestSuite) TestFullTreeRendering() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("f1", "epic"),
		child("f2", "epic"),
		child("t1", "f1"),
		child("t2", "f1"),
	})
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(60, 20)
	m.SetSnapshot(snap)
	m.ExpandAll()

	out := ansi.Strip(m.View())

	// Structural assertions, independent of the exact styling.
	s.Contains(out, "├─")
	s.Contains(out, "└─")
	s.Contains(out, "│", "a continuing ancestor must draw a vertical guide")

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	s.Require().Len(lines, 5)
	s.Contains(lines[0], "epic")
	// f1 is epic's first child: indented under it, with a branch connector.
	s.Contains(lines[1], "f1")
	s.Contains(lines[1], "├─")
	// t1 and t2 sit directly under f1, and f1 still has f2 to come, so both
	// carry a bar reflecting f1's own continuation — the column this whole
	// test exists to pin.
	s.Contains(lines[2], "t1")
	s.Contains(lines[2], "│")
	s.Contains(lines[3], "t2")
	s.Contains(lines[3], "│")
	// f2 is epic's own last child: its row closes with └─ and nothing
	// continues past it.
	s.Contains(lines[4], "f2")
	s.Contains(lines[4], "└─")
	s.NotContains(lines[4], "│", "nothing continues past the only root's last child")
}

// TestFullTreeRenderingDeepNesting extends TestFullTreeRendering with a
// third level (f1 -> m1 -> t1/t2), so a guide's continuation is also
// exercised two levels down rather than only immediately under its owning
// ancestor. This is supplemental: TestFullTreeRendering above is what pins
// the brief's worked example and catches the failure that actually shipped.
func (s *prefixTestSuite) TestFullTreeRenderingDeepNesting() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("f1", "epic"),
		child("f2", "epic"),
		child("m1", "f1"),
		child("t1", "m1"),
		child("t2", "m1"),
	})
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(60, 20)
	m.SetSnapshot(snap)
	m.ExpandAll()

	out := ansi.Strip(m.View())
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	s.Require().Len(lines, 6)
	s.Contains(lines[0], "epic")
	s.Contains(lines[1], "f1")
	s.Contains(lines[2], "m1")
	// t1 and t2 sit two levels under f1, and f1 still has f2 to come, so both
	// carry a bar reflecting f1's own continuation.
	s.Contains(lines[3], "t1")
	s.Contains(lines[3], "│")
	s.Contains(lines[4], "t2")
	s.Contains(lines[4], "│")
	// f2 is epic's own last child and outside f1's subtree entirely: its row
	// closes with └─ and nothing continues past it.
	s.Contains(lines[5], "f2")
	s.Contains(lines[5], "└─")
	s.NotContains(lines[5], "│", "nothing continues past the only root's last child")
}

func (s *prefixTestSuite) TestRowsNeverExceedTheWidth() {
	snap := beads.NewSnapshot([]beads.Issue{
		//nolint:gosmopolitan // CJK width fixture, not locale text
		{ID: "epic", Title: strings.Repeat("長い題名", 20), Status: beads.StatusOpen},
		child("deep", "epic"),
	})
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSnapshot(snap)
	m.ExpandAll()

	for _, width := range []int{20, 40, 80} {
		s.Run(fmt.Sprintf("%d cols", width), func() {
			m.SetSize(width, 20)
			for line := range strings.SplitSeq(m.View(), "\n") {
				s.LessOrEqual(lipgloss.Width(line), width, "line: %q", line)
			}
		})
	}
}

func (s *prefixTestSuite) TestDeepNestingDegradesGracefully() {
	// At depth 20+ in an 80-column terminal the prefix alone is 40+ cells.
	// The title must still get room rather than the row collapsing.
	issues := []beads.Issue{{ID: "n0", Title: "root", Status: beads.StatusOpen}}
	for i := 1; i < 25; i++ {
		issues = append(issues, child(fmt.Sprintf("n%d", i), fmt.Sprintf("n%d", i-1)))
	}
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 40)
	m.SetSnapshot(beads.NewSnapshot(issues))
	m.ExpandAll()

	s.NotPanics(func() { _ = m.View() })
	for line := range strings.SplitSeq(m.View(), "\n") {
		s.LessOrEqual(lipgloss.Width(line), 80)
	}
}

// TestColumnsDropTheIDWhenDepthEatsTheLine pins the degrade path render.go's
// columns describes: at a narrow width and a deep enough node, the id column
// is dropped before the title is truncated to nothing. Without this, a
// columns that always kept the id (truncating the title to empty instead)
// would satisfy every width-bound assertion above vacuously.
// TestColumnsKeepsAPartialTitleWhenNeitherLayoutClearsTheFloor pins that
// minTitleWidth gates which layout columns prefers, not whether a title
// renders at all. At width 17 a depth-2 row's fixed columns (id-less) leave
// only 4 cells for the title — short of minTitleWidth (10) — so before this
// fix columns fell all the way through to a bare "withoutID" with no title
// whatsoever, even though those 4 cells were free and unused. Reverting the
// fallback to plain `return withoutID` turns this red.
func (s *prefixTestSuite) TestColumnsKeepsAPartialTitleWhenNeitherLayoutClearsTheFloor() {
	epic := beads.Issue{ID: "epic", Title: "epic", Status: beads.StatusOpen}
	f1 := child("f1", "epic")
	f2 := child("f2", "epic")
	t1 := child("t1", "f1")
	t1.Title = "task a longer title here"
	t2 := child("t2", "f1")

	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(17, 10)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{epic, f1, f2, t1, t2}))
	m.ExpandAll()

	lines := strings.Split(strings.TrimRight(ansi.Strip(m.View()), "\n"), "\n")
	s.Require().Len(lines, 5)
	deepest := lines[2] // t1: depth 2, under f1, with f2 still to come and t2 to follow.

	// The bare fixed columns alone ("- P0 ", glyph+priority) carry no title
	// content; the fallback must append something of the title beyond them.
	s.NotEqual("  │ ├─  - P0 ", deepest, "a title must still be attempted even below minTitleWidth")
	s.Contains(deepest, "tas", "part of the title must survive, not just the fixed columns")
	s.LessOrEqual(lipgloss.Width(deepest), 17)
}

func (s *prefixTestSuite) TestColumnsDropTheIDWhenDepthEatsTheLine() {
	issues := []beads.Issue{{ID: "n0", Title: "root", Status: beads.StatusOpen}}
	for i := 1; i <= 5; i++ {
		issues = append(issues, child(fmt.Sprintf("n%d", i), fmt.Sprintf("n%d", i-1)))
	}
	issues[5].ID = "very-long-identifier-here"
	issues[5].Title = "a title that is long enough to need the freed space"

	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(30, 20)
	m.SetSnapshot(beads.NewSnapshot(issues))
	m.ExpandAll()

	var deepest string
	for line := range strings.SplitSeq(m.View(), "\n") {
		if strings.Contains(ansi.Strip(line), "title") {
			deepest = line
		}
	}
	s.Require().NotEmpty(deepest, "the deepest row must still show part of its title")
	s.NotContains(ansi.Strip(deepest), "very-long-identifier-here",
		"the id column must be dropped once depth leaves too little room for both it and a legible title")
}

// TestMatchesFilterFalseRendersMuted pins that a node kept only for
// reachability — not a match itself — renders visibly differently from one
// that matched. A tombstone with a live child is the fixture Retain's own
// doc comment names for this: Filter{}.Matches never returns true for a
// tombstone, so SetSnapshot's unconditional Retain call marks it
// MatchesFilter == false while still keeping it, for its live child's sake.
//
// "other-root" sorts ahead of "tomb-root" (sortIssues' id tiebreak), so it —
// not the tombstone — becomes the default selection. Without a second root
// ahead of it, this task's Model (which has no cursor-movement method yet;
// Task 5.3 adds one) always selects the first visible row, and that row
// would be the tombstone itself: Selected's style would then mask Muted with
// Selected, and the two styles being compared unequal would follow from
// *that* rather than from the MatchesFilter branch this test exists to pin.
// Verified: with only the tombstone fixture, deleting the MatchesFilter
// branch in renderRow entirely still left this assertion green.
// TestSanitizesControlCharactersInBothTitleAndID pins that a raw newline in
// either field is stripped before it reaches the rendered line — not just
// the title. columns interpolates issue.ID directly alongside the sanitised
// title; without sanitising the id too, lipgloss.Width (which reports a
// multi-line string's max line width) cannot see the extra physical row a
// raw "\n" in the id introduces, so only a line-count assertion like this one
// catches it.
func (s *prefixTestSuite) TestSanitizesControlCharactersInBothTitleAndID() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "first\nsecond", Title: "first\nsecond", Status: beads.StatusOpen},
	})
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(snap)

	out := ansi.Strip(m.View())
	lines := strings.Split(out, "\n")
	s.Len(lines, 1, "a raw newline in the id or title must not emit a second physical row for one row")
}

func (s *prefixTestSuite) TestMatchesFilterFalseRendersMuted() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "other-root", Title: "other-root", Status: beads.StatusOpen},
		{ID: "tomb-root", Title: "tomb-root", Status: beads.StatusTombstone},
		child("live-child", "tomb-root"),
	})
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(snap)
	m.ExpandAll()

	var muted, matched string
	for line := range strings.SplitSeq(m.View(), "\n") {
		switch {
		case strings.Contains(ansi.Strip(line), "tomb-root"):
			muted = line
		case strings.Contains(ansi.Strip(line), "live-child"):
			matched = line
		}
	}
	s.Require().NotEmpty(muted)
	s.Require().NotEmpty(matched)
	s.NotEqual(styleCode(muted), styleCode(matched),
		"a node kept only for reachability must be styled differently from a match")
}

// TestSetSnapshotHidesTombstoneLeavesByDefault pins that SetSnapshot's
// unconditional Retain call actually runs, not merely that a kept-back
// tombstone renders muted (TestMatchesFilterFalseRendersMuted above): a
// tombstone leaf with no live descendant has nothing to stay reachable for,
// so Retain drops it outright.
func (s *prefixTestSuite) TestSetSnapshotHidesTombstoneLeavesByDefault() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		{
			ID: "tomb-leaf", Title: "tomb-leaf", Status: beads.StatusTombstone,
			Dependencies: []beads.Dependency{
				{IssueID: "tomb-leaf", DependsOnID: "root", Type: beads.DepParentChild},
			},
		},
	})
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(snap)
	m.ExpandAll()

	s.NotContains(ansi.Strip(m.View()), "tomb-leaf",
		"a tombstone leaf must stay hidden by default, even though bv otherwise renders rather than validates")
}

func (s *prefixTestSuite) TestExpandAllAndCollapseAll() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("a", "epic"),
		child("b", "epic"),
	})
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(snap)

	m.ExpandAll()
	expanded := strings.Split(strings.TrimRight(ansi.Strip(m.View()), "\n"), "\n")
	s.Len(expanded, 3, "expanding must reveal both children")

	m.CollapseAll()
	collapsed := strings.Split(strings.TrimRight(ansi.Strip(m.View()), "\n"), "\n")
	s.Len(collapsed, 1, "collapsing must hide every child, including the root's own")
}

// TestSelectedIsNilWhenEmpty pins that an empty tree neither panics nor
// selects anything. It renders "" rather than a placeholder: Model.body
// (internal/tui, empty.go) intercepts an empty workspace, an empty filter
// result and a failed initial load before joinPanes ever calls this View, so
// the only way to reach this state is the direct construction below — the
// same arrangement listview and boardview have had since that consolidation.
func (s *prefixTestSuite) TestSelectedIsNilWhenEmpty() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot(nil))

	s.Nil(m.Selected())
	var out string
	s.NotPanics(func() { out = m.View() })
	s.Empty(out)
}

func (s *prefixTestSuite) TestSetSnapshotPreservesSelectionAcrossReload() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
	}))
	s.Require().Equal("epic", m.Selected().ID, "SetSnapshot's fallback selects the first visible row")

	// A reload where "epic" is no longer the first visible row must still
	// keep it selected. "aaa" sorts ahead of "epic" (sortIssues' id
	// tiebreak), so if the reload merely fell back to rows[0] instead of
	// actually looking "epic" up, this would select "aaa" instead — which is
	// exactly what disabling FindNode's lookup here produces.
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "aaa", Title: "aaa", Status: beads.StatusOpen},
		{ID: "epic", Title: "epic (renamed)", Status: beads.StatusOpen},
	}))
	s.Equal("epic", m.Selected().ID, "reload must preserve selection even once it is no longer row 0")

	// A reload that drops "epic" must fall back rather than keep a stale,
	// invisible selection.
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "new-root", Title: "new-root", Status: beads.StatusOpen},
	}))
	s.Equal("new-root", m.Selected().ID)
}

func (s *prefixTestSuite) TestDegenerateSizes() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "epic", Status: beads.StatusOpen},
		child("child", "epic"),
	})
	for _, size := range [][2]int{{0, 0}, {1, 1}, {5, 2}} {
		s.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func() {
			m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
			m.SetSize(size[0], size[1])
			m.SetSnapshot(snap)
			m.ExpandAll()

			var out string
			s.NotPanics(func() { out = m.View() })
			for line := range strings.SplitSeq(out, "\n") {
				s.LessOrEqual(lipgloss.Width(line), size[0], "line: %q", line)
			}
		})
	}
}

func (s *prefixTestSuite) TestGoldenRendering() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(60, 10)
	m.SetSnapshot(beads.NewSnapshot(s.sampleHierarchy()))
	m.ExpandAll()

	actual := ansi.Strip(m.View())
	s.Equal(golden(s.T(), "tree_60x10.golden", actual), actual)
}

// TestGoldenRenderingNarrowWidthTruncates pins truncation together with the
// degrade path: at 24 columns "feature one"/"feature two" are cut with an
// ellipsis, and the deeper task rows lose their id column entirely.
//
// 24, not 22: under the corrected Prefix, every ancestor draws its own bar
// unit rather than sharing one with the connector, so depth-1 and depth-2
// prefixes are each 2 cells wider than they were under the superseded,
// narrower rule. 22 columns no longer leaves enough room to demonstrate
// ellipsis truncation at depth 1 at all (verified: at 22 the title is
// dropped to empty instead of cut), which would make this test's own
// assertions vacuous.
func (s *prefixTestSuite) TestGoldenRenderingNarrowWidthTruncates() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(24, 10)
	m.SetSnapshot(beads.NewSnapshot(s.sampleHierarchy()))
	m.ExpandAll()

	actual := ansi.Strip(m.View())
	s.Contains(actual, "…", "at this width a title must actually be cut")
	s.NotContains(actual, "t1", "at this depth and width the id column must be dropped")
	s.Equal(golden(s.T(), "tree_24x10.golden", actual), actual)
}

// sampleHierarchy builds the three-level tree used by the golden tests:
// epic -> {feature one -> {task a, task b}, feature two}.
func (s *prefixTestSuite) sampleHierarchy() []beads.Issue {
	epic := beads.Issue{ID: "epic", Title: "epic", Status: beads.StatusOpen, IssueType: beads.TypeEpic}

	f1 := child("f1", "epic")
	f1.Title, f1.IssueType = "feature one", beads.TypeFeature

	f2 := child("f2", "epic")
	f2.Title, f2.IssueType = "feature two", beads.TypeFeature

	t1 := child("t1", "f1")
	t1.Title, t1.IssueType = "task a", beads.TypeTask

	t2 := child("t2", "f1")
	t2.Title, t2.IssueType, t2.Status = "task b", beads.TypeTask, beads.StatusClosed

	return []beads.Issue{epic, f1, f2, t1, t2}
}

// leadingSGR matches a leading "Select Graphic Rendition" escape sequence —
// the \x1b[...m codes lipgloss.Style.Render emits for colour, bold, reverse
// and the like.
var leadingSGR = regexp.MustCompile(`^\x1b\[[0-9;]*m`)

// styleCode returns line's leading escape sequence, so two lines can be
// compared for differing styling by content rather than by byte count alone
// — two single-attribute styles can render to same-length codes with
// different colours, which a length comparison would miss.
func styleCode(line string) string {
	return leadingSGR.FindString(line)
}

// golden reads a golden file from testdata, or writes actual to it when the
// UPDATE_GOLDEN environment variable is set. Mirrors internal/tui/listview's
// helper of the same name and shape; kept local rather than shared, per that
// package's own note that a second caller alone does not yet justify
// promoting it into a tuitest package.
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
