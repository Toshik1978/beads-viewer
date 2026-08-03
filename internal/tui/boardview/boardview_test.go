package boardview_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/boardview"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// boardTestSuite is registered from group_test.go's single TestBoardview
// entry point, per this package's one-entry-point rule.
type boardTestSuite struct {
	suite.Suite
}

func (s *boardTestSuite) model(issues []beads.Issue, w, h int) *boardview.Model {
	m := boardview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(w, h)
	m.SetSnapshot(beads.NewSnapshot(issues))

	return m
}

// jumpToColumn is a test-local stand-in for the exported JumpToColumn method
// I9 removed (zero non-test callers): it steps MoveRight i times from a
// freshly built model sitting at column 0. That no longer lands on raw
// column index i in general — MoveLeft/MoveRight skip empty columns
// (bv-llf.2.1) — so which column i actually reaches depends on the fixture;
// see each call site's own comment for where it lands there.
func jumpToColumn(m *boardview.Model, i int) {
	for range i {
		m.MoveRight()
	}
}

func (s *boardTestSuite) sample() []beads.Issue {
	return []beads.Issue{
		{ID: "a", Title: "open one", Status: beads.StatusOpen, Priority: beads.PriorityHigh},
		{ID: "b", Title: "open two", Status: beads.StatusOpen, Priority: beads.PriorityMedium},
		{ID: "c", Title: "in flight", Status: beads.StatusInProgress},
		{ID: "d", Title: "finished", Status: beads.StatusClosed},
	}
}

// gapped puts an empty column between two populated ones under the status
// lane, which is the shape the skip exists for — a fixture without the gap
// passes whether or not MoveRight skips anything.
func (s *boardTestSuite) gapped() []beads.Issue {
	return []beads.Issue{
		{ID: "bv-1", Title: "open one", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "blocked one", Status: beads.StatusBlocked},
	}
}

// columnIndexOf finds which of the board's status columns holds id, by
// asking Group directly for the same grouping s.gapped() feeds into m — the
// suite has no accessor for a Model's own column index, and going through
// Group instead of driving the cursor with MoveLeft/MoveRight keeps this
// helper independent of the very skipping behaviour the tests that use it
// are trying to pin.
func (s *boardTestSuite) columnIndexOf(m *boardview.Model, id string) int {
	s.T().Helper()

	for i, col := range boardview.Group(beads.NewSnapshot(s.gapped()), m.Lane()) {
		for _, issue := range col.Issues {
			if issue.ID == id {
				return i
			}
		}
	}

	s.Fail("id not found in any column", id)

	return -1
}

func (s *boardTestSuite) TestRendersEveryColumnHeader() {
	out := s.model(s.sample(), 140, 30).View()

	for _, want := range []string{"Open", "In Progress", "Blocked", "Closed"} {
		s.Contains(out, want)
	}
}

// TestNavigationIsTotal is strengthened beyond NotPanics (see this task's
// brief, "The defect class this project keeps hitting"): NotPanics alone
// would not catch a cursor that never moves at all, so this also collects
// every SelectedID seen across the run and requires more than one distinct
// answer — proof that movement actually changed the cursor, not just that it
// failed to crash.
func (s *boardTestSuite) TestNavigationIsTotal() {
	m := s.model(s.sample(), 140, 30)

	moves := []func(){
		m.MoveDown, m.MoveUp, m.MoveLeft, m.MoveRight,
		m.JumpToTop, m.JumpToBottom,
	}
	seen := map[string]bool{}
	for i := range 500 {
		s.NotPanics(func() {
			moves[i%len(moves)]()
			_ = m.View()
			seen[m.SelectedID()] = true
		})
	}
	s.Greater(len(seen), 1, "500 mixed movements over a multi-column board must visit more than one cell")
}

func (s *boardTestSuite) TestMovingIntoAShorterColumnClampsTheRow() {
	issues := []beads.Issue{
		{ID: "o1", Title: "o1", Status: beads.StatusOpen},
		{ID: "o2", Title: "o2", Status: beads.StatusOpen},
		{ID: "o3", Title: "o3", Status: beads.StatusOpen},
		{ID: "p1", Title: "p1", Status: beads.StatusInProgress},
	}
	m := s.model(issues, 140, 30)

	jumpToColumn(m, 0)
	m.MoveDown()
	m.MoveDown()  // row 2 of the Open column
	m.MoveRight() // In Progress has only one card

	s.Equal("p1", m.SelectedID())
}

// TestClampStillResolvesToAnEmptyColumnAfterARegroup replaces the older
// TestEmptyColumnIsReachableAndSelectsNothing, which pinned exactly the
// behaviour this task removes: MoveRight landing deliberately on an empty
// column. That is now covered from the other direction by
// TestRightSkipsAnEmptyColumn and friends below, plus TestEmptyColumnsStillRender
// for the "still on screen" half of the old test's name. What survives from
// it is clamp's own contract, which this task explicitly leaves unchanged: a
// regrouping can still strand the cursor on a stale row, with no id left to
// preserve the selection by, and clamp alone (not movement) has to resolve
// that safely.
//
// The fixture is deliberately NOT "the column becomes wholly empty": that
// version passes with clamp's body deleted entirely, because Selected()'s
// own row < 0 || row >= len(issues) bounds check already reads row 0 against
// a 0-issue column as "nothing selected" whether or not clamp ever ran —
// verified by emptying clamp and watching that version stay green (see this
// task's review round). Leaving the column with exactly one issue, and the
// stale row at 2 rather than 0, is what makes clamp's own pull-back the only
// thing that can produce the right answer: without it, row 2 against a
// 1-issue column is just as clearly out of range as row 2 against a 0-issue
// one, but WITH it, row clamps to 0 and lands on that one issue rather than
// staying stuck reading "nothing selected".
func (s *boardTestSuite) TestClampStillResolvesToAnEmptyColumnAfterARegroup() {
	m := s.model([]beads.Issue{
		{ID: "b1", Title: "b1", Status: beads.StatusBlocked},
		{ID: "b2", Title: "b2", Status: beads.StatusBlocked},
		{ID: "b3", Title: "b3", Status: beads.StatusBlocked},
	}, 140, 30)
	s.Require().True(m.SelectByID("b3"), "fixture sanity: the cursor starts at row 2 of the Blocked column")

	// None of "b1".."b3" survive into the new snapshot, so regroup's
	// find(preserveID) fails and the cursor's row stays stale at 2 — one
	// past the new Blocked column's only row (0).
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "other", Title: "other", Status: beads.StatusBlocked},
	}))

	s.Equal("other", m.SelectedID(),
		"clamp must pull the stale row 2 back onto the column's one remaining issue at row 0")
	s.NotPanics(func() { _ = m.View() })
}

func (s *boardTestSuite) TestRightSkipsAnEmptyColumn() {
	m := s.model(s.gapped(), 200, 20)
	s.Require().Equal("bv-1", m.SelectedID())

	before := s.columnIndexOf(m, "bv-1")
	m.MoveRight()

	s.Equal("bv-2", m.SelectedID(),
		"the cursor must land on the next column that holds something")
	s.Greater(s.columnIndexOf(m, "bv-2"), before+1,
		"fixture assumption: at least one empty column sat between them")
}

func (s *boardTestSuite) TestLeftSkipsAnEmptyColumn() {
	m := s.model(s.gapped(), 200, 20)
	s.Require().True(m.SelectByID("bv-2"))

	m.MoveLeft()

	s.Equal("bv-1", m.SelectedID())
}

func (s *boardTestSuite) TestMovingPastTheLastPopulatedColumnStaysPut() {
	m := s.model(s.gapped(), 200, 20)
	s.Require().True(m.SelectByID("bv-2"))

	for range 5 {
		m.MoveRight()
	}

	s.Equal("bv-2", m.SelectedID(),
		"with nothing populated to the right, the cursor does not move")
}

// TestAnAllEmptyBoardDoesNotMoveOrPanic asserts the frame itself, not just
// SelectedID, stays put: SelectedID() == "" holds on an all-empty board
// however the cursor moves, so that assertion alone is satisfied by the
// m.col++ mutant Step 5 exists to catch just as much as by the fix. Width 58
// is narrow enough that all 5 status columns do not fit at once (see
// visibleLayout), so a cursor that ends up somewhere other than column 0
// slides the render window and changes the ANSI-stripped frame.
//
// A single MoveRight/MoveLeft pair is not enough to pin this: under the
// m.col++ mutant, one raw MoveRight only reaches column 1, which the initial
// 2-column-wide window already shows in full — same header text, same (0)
// counts, only the (ANSI-stripped-away) focus styling differs, so the frame
// compares equal despite the cursor having actually moved (verified: that
// one-call version stayed green under the mutant). Five of each is enough to
// push a raw-incrementing MoveRight to column 4 — clamp's own upper bound —
// which a skip-aware MoveLeft then cannot pull back from (nothing left of it
// is populated either), scrolling the window and changing the frame for
// real.
func (s *boardTestSuite) TestAnAllEmptyBoardDoesNotMoveOrPanic() {
	m := s.model(nil, 58, 20)
	before := ansi.Strip(m.View())

	s.NotPanics(func() {
		for range 5 {
			m.MoveRight()
		}
		for range 5 {
			m.MoveLeft()
		}
	})

	s.Empty(m.SelectedID())
	s.Equal(before, ansi.Strip(m.View()), "with nothing populated anywhere, the cursor must not move at all")
}

func (s *boardTestSuite) TestEmptyColumnsStillRender() {
	out := ansi.Strip(s.model(s.gapped(), 200, 20).View())

	// Skipping is a navigation rule, not a visibility one: a status with
	// nothing in it is worth seeing on a board used for triage.
	s.Contains(out, "(0)")
}

func (s *boardTestSuite) TestSCyclesTheSwimlane() {
	m := s.model(s.sample(), 100, 20)
	before := m.Lane()

	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})

	s.NotEqual(before, m.Lane())
}

// TestTabNoLongerCyclesTheSwimlane pins bv-f7b.2.3: tab is the app-level
// focus key now. A board that still consumed it would cycle the swimlane
// every time the user moved focus.
func (s *boardTestSuite) TestTabNoLongerCyclesTheSwimlane() {
	m := s.model(s.sample(), 100, 20)
	before := m.Lane()

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	s.Equal(before, m.Lane())
}

func (s *boardTestSuite) TestCycleSwimLanePreservesSelection() {
	m := s.model(s.sample(), 140, 30)
	s.Require().True(m.SelectByID("c"))

	for range 4 {
		m.CycleSwimLane()
		s.Equal("c", m.SelectedID(), "selection is by id and survives regrouping")
	}
}

// TestNoLineExceedsTheWidth is strengthened with a lower bound (see this
// task's brief): an upper-bound-only assertion is satisfied by an empty
// render. Requiring the wide CJK fixture's own id to survive somewhere in
// the frame at every one of the standard tested widths proves cards
// actually rendered, not just that nothing overflowed because nothing was
// drawn.
//
// The width-only loop is extended down to 1 (F7): visibleLayout's fallback
// for a terminal too narrow for even one column plus both marker slots used
// to hand the single column m.width unreserved, then append a strip on top
// of it, overflowing the very invariant this test exists to pin — verified
// broken at widths 1 through 9 before the fix. "wide" is dropped from that
// narrow range's own assertion: below minColumnWidth, cardfmt.Render's fallback
// draws one truncated line rather than a full card, so whether that specific
// substring survives is no longer a meaningful signal, only the width bound
// is.
func (s *boardTestSuite) TestNoLineExceedsTheWidth() {
	issues := append(s.sample(), beads.Issue{
		ID:     "wide",
		Title:  "日本語のとても長いタイトルです", //nolint:gosmopolitan // CJK width fixture, not locale text
		Status: beads.StatusOpen,
	})
	for _, width := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 80, 100, 140, 200} {
		s.Run(strconv.Itoa(width), func() {
			m := s.model(issues, width, 30)
			out := m.View()
			for line := range strings.SplitSeq(out, "\n") {
				s.LessOrEqual(lipgloss.Width(line), width, "line: %q", line)
			}
		})
	}
	for _, width := range []int{80, 100, 140, 200} {
		s.Run(strconv.Itoa(width)+"/renders", func() {
			m := s.model(issues, width, 30)
			s.Contains(m.View(), "wide", "the Open column, always shown first, must actually render its card")
		})
	}
}

// TestNoFrameExceedsTheHeight guards the invariant a width-only check cannot:
// lipgloss's Style.Render wraps content that is too long for its Width
// rather than letting it overflow that width (verified directly against
// lipgloss.NewStyle().Width(10).Border(...).Render on an overlong string,
// which came back eight rows tall and never wider than ten cells). A card
// renderer that forgot to pre-truncate a title would therefore stay within
// every width assertion in this file while silently blowing the column's
// height budget — exactly Task 5.3's regression, recurring in a different
// view. Height, not width, is the axis that catches it.
func (s *boardTestSuite) TestNoFrameExceedsTheHeight() {
	issues := append(s.sample(), beads.Issue{
		ID: "long", Title: strings.Repeat("overflow ", 20), Status: beads.StatusOpen,
	})
	for _, size := range [][2]int{{80, 10}, {80, 20}, {140, 8}, {30, 30}} {
		s.Run("", func() {
			m := s.model(issues, size[0], size[1])
			lines := strings.Split(m.View(), "\n")
			s.LessOrEqual(len(lines), size[1], "frame at %dx%d must not exceed its own height", size[0], size[1])
		})
	}
}

func (s *boardTestSuite) TestNarrowTerminalShowsFewerColumns() {
	m := s.model(s.sample(), 50, 30)

	out := m.View()
	s.NotPanics(func() { _ = out })
	for line := range strings.SplitSeq(out, "\n") {
		s.LessOrEqual(lipgloss.Width(line), 50)
	}
	// 5 status columns at width 50 fit 2 (see boardview.go's visibleLayout);
	// the cursor starts on column 0, so nothing is hidden to its left and
	// the right-hand marker names the 3 columns hidden beyond the window.
	s.Contains(out, "3>", "hidden columns must be announced, not silently dropped")
	s.NotContains(out, "<", "nothing is hidden to the left of the initial cursor position")
}

func (s *boardTestSuite) TestSelectionSurvivesAReload() {
	m := s.model(s.sample(), 140, 30)
	s.Require().True(m.SelectByID("b"))

	m.SetSnapshot(beads.NewSnapshot(append(s.sample(), beads.Issue{
		ID: "new", Title: "new", Status: beads.StatusOpen, Priority: beads.PriorityCritical,
	})))

	s.Equal("b", m.SelectedID())
}

func (s *boardTestSuite) TestDegenerateSizes() {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {10, 3}} {
		s.Run("", func() {
			s.NotPanics(func() { _ = s.model(s.sample(), size[0], size[1]).View() })
		})
	}
}

// TestColumnHeaderReflectsStatsNotJustCount guards against a card/column
// renderer that ignores group.go's Stats entirely and only ever counts
// len(Issues) itself. OldestDays cannot be recovered from the issue count or
// even from the issues slice's length in any of the columns visible here, so
// this fixture — one issue, ten days old — is the sharpest single fact that
// distinguishes "reads Stats" from "recomputes count".
func (s *boardTestSuite) TestColumnHeaderReflectsStatsNotJustCount() {
	// time.Now().Add(-10 * 24 * time.Hour), not AddDate(0, 0, -10): AddDate
	// steps 10 calendar days, which is 239h rather than 240h across a
	// spring-forward DST transition, and int(239.x/24) floors to 9 instead
	// of 10 — a flake this fixed-duration form cannot hit (F11).
	old := time.Now().Add(-10 * 24 * time.Hour)
	issues := []beads.Issue{
		{ID: "old1", Title: "old", Status: beads.StatusOpen, CreatedAt: old},
	}
	m := s.model(issues, 140, 30)

	s.Contains(m.View(), "10d", "column header must surface Stats.OldestDays, not just the issue count")
}

// TestCatchallColumnIsMarkedDistinctly pins amendment 1: Catchall, not
// Title, is what a renderer must key off. A project can have a real column
// literally titled "Unassigned" that collides with the catch-all's own
// title, so the rendered header for the two must differ even though
// col.Title is identical for both.
//
// The two columns are given different issue counts (1 vs 2) deliberately: a
// renderer that keys the marker off Title == "Unassigned" instead of
// col.Catchall would mark both columns identically and still pass an
// assertion built only from the shared title string, since neither
// "Contains .. marker+title" nor "NotContains .. marker+title twice" can
// tell two same-titled, same-marked columns apart. Pinning the header
// against each column's own count is what makes a Title-keyed mutant fail
// here: it stamps the marker onto the real column's "Unassigned (1)" too, so
// "Unassigned (1)" without a leading marker never appears, and NotContains
// below catches that directly.
func (s *boardTestSuite) TestCatchallColumnIsMarkedDistinctly() {
	m := s.model([]beads.Issue{
		{ID: "1", Title: "1", Status: beads.StatusOpen, Assignee: "Unassigned"},
		{ID: "2", Title: "2", Status: beads.StatusOpen},
		{ID: "3", Title: "3", Status: beads.StatusOpen},
	}, 140, 30)
	m.CycleSwimLane() // Status -> Priority
	m.CycleSwimLane() // Priority -> Assignee

	out := m.View()
	s.Contains(out, "Unassigned (1)", "the real assignee column's header must render unmarked")
	s.NotContains(out, boardview.CatchallMarker+"Unassigned (1)",
		"the real column, count 1, must not carry the catch-all marker")
	s.Contains(out, boardview.CatchallMarker+"Unassigned (2)",
		"the catch-all column, count 2, must carry the catch-all marker")
}

func (s *boardTestSuite) TestToggleExpandAddsBlockedByLine() {
	// The blocker's title is deliberately NOT its id ("the blocker", not
	// "opener"): the original fixture gave it Title == ID, so
	// s.Contains(expanded, "opener") was satisfied by the opener card's own
	// title sitting in the same Open column, regardless of whether
	// blockedByLine ever named a blocker at all — proved by making
	// blockedByLine return the raw status instead, which never emits
	// "blocked" or "ready", and watching the suite stay green (F3).
	// Asserting the "blocked by opener" phrase itself, plus a NotContains
	// on the collapsed frame, is what a status-only blockedByLine cannot
	// satisfy.
	m := s.model([]beads.Issue{
		{ID: "opener", Title: "the blocker", Status: beads.StatusOpen},
		{
			ID: "worker", Title: "worker", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "worker", DependsOnID: "opener", Type: beads.DepBlocks},
			},
		},
	}, 140, 30)
	s.Require().True(m.SelectByID("worker"))

	collapsed := m.View()
	s.NotContains(collapsed, "blocked by opener", "collapsed cards must not show the blocked-by line at all")

	m.ToggleExpand()
	expanded := m.View()

	s.NotEqual(collapsed, expanded, "expanding must change the rendered frame")
	s.Contains(expanded, "blocked by opener", "the expanded card must name its blocker by id")
}

// AMENDMENT — do NOT call lipgloss.SetColorProfile; it does not exist.
//
// `SetColorProfile` and `lipgloss.Ascii` were v1 API and are absent from
// lipgloss v2.0.5. v2 has no global renderer: `Style.Render` always emits the
// full escape sequence and downsampling happens at the writer, so golden
// output is already deterministic across CI, tmux and a local TTY — verified
// across TERM=xterm-256color, TERM=dumb, NO_COLOR=1 and COLORTERM=truecolor.
//
// The golden file stores the ANSI-stripped frame, matching Task 4.1: an
// escape-laden golden is unreviewable, and a golden nobody reads gets
// regenerated rather than checked. Assert styling separately — for the board
// that means the selected card is visually distinct from its neighbours.
func (s *boardTestSuite) TestGoldenRendering() {
	m := s.model(s.sample(), 120, 20)
	m.SelectByID("a")

	actual := ansi.Strip(m.View())
	s.Equal(golden(s.T(), "board_120x20.golden", actual), actual)
}

// TestSelectedCardIsStyledDistinctly is the styling half of the golden note
// above: stripping colour out of the golden must not leave selection styling
// untested. The selected card's first content line must carry different SGR
// codes from the same card unselected.
func (s *boardTestSuite) TestSelectedCardIsStyledDistinctly() {
	m := s.model(s.sample(), 140, 30)
	s.Require().True(m.SelectByID("a"))
	selected := m.View()

	s.Require().True(m.SelectByID("b"))
	moved := m.View()

	s.NotEqual(selected, moved, "moving the cursor must change the rendered (unstripped) frame")
	s.Equal(ansi.Strip(selected), ansi.Strip(moved),
		"only the SGR styling should differ between which card is selected; the visual content must not")
}

// TestGoldenRenderingWithScrollMarkers is TestGoldenRendering's counterpart
// at a size that actually exercises overflow (F10): the committed
// board_120x20 golden shows all 5 status columns at once and never has a
// board-level "+N more"/scroll marker of any kind, so nothing pinned that
// path at all. 58 columns-wide fits exactly 2 of the 5 status columns (see
// boardview.go's visibleLayout). jumpToColumn(2) used to land on Blocked
// (index 2, empty in this fixture); now that MoveRight skips empty columns
// it lands on Closed (index 3) instead, which still pushes the window to
// [2,3] — two columns hidden on the left, one on the right — so this still
// forces both markers, just off a different column than before.
func (s *boardTestSuite) TestGoldenRenderingWithScrollMarkers() {
	m := s.model(s.sample(), 58, 20)
	jumpToColumn(m, 2) // Closed: forces both a left and a right hidden-column marker

	actual := ansi.Strip(m.View())
	s.Equal(golden(s.T(), "board_58x20_scrolled.golden", actual), actual)
}

// TestColumnWindowFollowsTheCursorInBothDirections is F1's regression guard.
// Before this fix, the render window was always [0, visible): once the
// cursor moved past the last visible column, three presses of MoveRight
// left the frame byte-identical to before it, with the selection changing
// only in the (untested) detail pane. TestNavigationIsTotal could not catch
// this — it only requires more than one distinct SelectedID, which
// MoveDown alone already produces — so this drives the cursor with
// MoveRight/MoveLeft specifically and asserts the FRAME itself changes and
// the window's contents shift with it.
//
// 6 distinct assignees plus the trailing Unassigned catch-all is 7 columns;
// at width 58 that fits 2 at a time (same arithmetic as the scrolled golden
// above), so u1 starts inside the window and u4 (three MoveRight presses
// away) starts three columns past its right edge.
func (s *boardTestSuite) TestColumnWindowFollowsTheCursorInBothDirections() {
	issues := make([]beads.Issue, 6)
	for i := range issues {
		name := fmt.Sprintf("u%d", i+1)
		issues[i] = beads.Issue{ID: name, Title: name, Status: beads.StatusOpen, Assignee: name}
	}
	m := s.model(issues, 58, 40)
	m.CycleSwimLane() // Status -> Priority
	m.CycleSwimLane() // Priority -> Assignee

	before := m.View()
	s.Contains(before, "u1", "the first column starts inside the initial window")
	s.NotContains(before, "u4", "u4's column starts off the right edge of a 2-of-7 window")

	for range 3 {
		m.MoveRight()
	}
	after := m.View()
	s.Equal("u4", m.SelectedID(), "fixture sanity: three right-moves from u1 lands on u4")
	s.NotEqual(before, after, "the frame must change once the cursor leaves the visible window")
	s.Contains(after, "u4", "the window must have followed the cursor onto u4's column")
	s.NotContains(after, "u1", "u1's column must have scrolled out of the window")

	for range 3 {
		m.MoveLeft()
	}
	back := m.View()
	s.Equal("u1", m.SelectedID())
	s.Contains(back, "u1", "the window must follow the cursor back left too")
}

// TestFocusedColumnHeaderIsBolderThanUnfocused pins F13: the focused
// column's header used th.Accent (not bold) while every unfocused header
// used th.Title (bold), so the focused column was the only non-bold one on
// the board — backwards from what focus is supposed to signal. Nothing
// caught this: TestSelectedCardIsStyledDistinctly only ever moves between
// two cards in the SAME column, so the header style never varies there.
//
// sgrPrefix extracts just the opening escape sequence from a style's own
// Render output, which depends only on the style's attributes (Bold,
// Foreground) and not on the text or width that follows — comparing that
// prefix against the frame is what avoids having to reproduce boardview's
// internal column-width arithmetic to find the exact padded substring.
func (s *boardTestSuite) TestFocusedColumnHeaderIsBolderThanUnfocused() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)
	focusedPrefix := sgrPrefix(th.Accent.Bold(true).Render("X"))
	unfocusedPrefix := sgrPrefix(th.Title.Render("X"))
	s.Require().NotEqual(focusedPrefix, unfocusedPrefix, "fixture sanity: the two styles must differ")

	// Width 25 fits exactly 1 of the 5 status columns, so the only column
	// ever rendered is whichever one the cursor is on — always focused.
	m := s.model(s.sample(), 25, 30)

	s.Contains(m.View(), focusedPrefix, "the focused column's header must carry Accent+Bold styling")
}

// sgrPrefix returns s's leading "\x1b[...m" escape sequence, or "" if s has
// none.
func sgrPrefix(s string) string {
	start := strings.Index(s, "\x1b[")
	if start < 0 {
		return ""
	}
	end := strings.Index(s[start:], "m")
	if end < 0 {
		return ""
	}

	return s[start : start+end+1]
}

// TestControlCharactersDoNotAddRenderedRows pins amendment 3 (this task's
// design doc): every uitext.Sanitize call in card.go can be deleted and the
// suite stays green, because nothing asserts what a raw control character
// does to the frame specifically — a width assertion cannot see it, since
// ansi.StringWidth counts a control byte as zero-width (F2). A raw '\n'
// left in place emits a second physical row that only a row-COUNT
// assertion catches; comparing against a control-character-free equivalent
// of the same issue is what makes this exact, rather than an arbitrary
// threshold.
func (s *boardTestSuite) TestControlCharactersDoNotAddRenderedRows() {
	dirty := beads.Issue{
		ID: "a\nb\x1bc", Title: "title\x1bline\nmore", Status: beads.StatusOpen,
		Assignee: "bob\nsmith\x1bx", Labels: []string{"x\x1by\nz"},
	}
	clean := beads.Issue{
		ID: "abc", Title: "titlelinemore", Status: beads.StatusOpen,
		Assignee: "bobsmithx", Labels: []string{"xyz"},
	}

	dirtyModel := s.model([]beads.Issue{dirty}, 140, 30)
	dirtyModel.ToggleExpand()
	cleanModel := s.model([]beads.Issue{clean}, 140, 30)
	cleanModel.ToggleExpand()

	s.Equal(strings.Count(cleanModel.View(), "\n"), strings.Count(dirtyModel.View(), "\n"),
		"control characters embedded in id/title/assignee/label must not add physical rows to the frame")
}

// boardKeyMsg builds the key press Update expects, mirroring
// treeview/nav_test.go's keyMsg helper for the handful of names that are not
// a single printable rune.
func boardKeyMsg(key string) tea.KeyPressMsg {
	named := map[string]tea.KeyPressMsg{
		"up": {Code: tea.KeyUp}, "down": {Code: tea.KeyDown},
		"left": {Code: tea.KeyLeft}, "right": {Code: tea.KeyRight},
		"home": {Code: tea.KeyHome}, "end": {Code: tea.KeyEnd},
		"space": {Code: tea.KeySpace},
	}
	if msg, ok := named[key]; ok {
		return msg
	}

	r, _ := utf8.DecodeRuneInString(key)

	return tea.KeyPressMsg{Code: r}
}

// TestUpdateDrivesEveryKeyBinding is F5's regression guard: every other test
// in this file calls MoveUp/MoveDown/CycleSwimLane/etc. directly in Go, so a
// typo in keyActions' string-literal map (boardview.go) — "spae" for
// "space", say — would leave that binding permanently dead in the actual
// binary while every other test here stayed green. treeview's nav_test.go
// already pins this for its own package; boardview pinned neither "space"
// nor "s" before this — "s" is now covered by TestSCyclesTheSwimlane above
// instead of a table entry here, since driving it through Update is the same
// typo guard either way. Driving Update itself, once per binding (plus its
// letter alias where it has one), is what a literal-string typo cannot
// survive.
func (s *boardTestSuite) TestUpdateDrivesEveryKeyBinding() {
	fixture := func() *boardview.Model {
		return s.model([]beads.Issue{
			{ID: "a", Title: "a", Status: beads.StatusOpen},
			{ID: "b", Title: "b", Status: beads.StatusOpen},
			{ID: "c", Title: "c", Status: beads.StatusOpen},
			{ID: "p", Title: "p", Status: beads.StatusInProgress},
		}, 140, 30)
	}

	tests := []struct {
		name string
		run  func(m *boardview.Model)
	}{
		{"down moves the cursor down", func(m *boardview.Model) {
			m.Update(boardKeyMsg("down"))
			s.Equal("b", m.SelectedID())
		}},
		{"j is down's letter alias", func(m *boardview.Model) {
			m.Update(boardKeyMsg("j"))
			s.Equal("b", m.SelectedID())
		}},
		{"up moves the cursor back up", func(m *boardview.Model) {
			m.MoveDown()
			m.MoveDown()
			m.Update(boardKeyMsg("up"))
			s.Equal("b", m.SelectedID())
		}},
		{"k is up's letter alias", func(m *boardview.Model) {
			m.MoveDown()
			m.MoveDown()
			m.Update(boardKeyMsg("k"))
			s.Equal("b", m.SelectedID())
		}},
		{"right moves into the next column", func(m *boardview.Model) {
			m.Update(boardKeyMsg("right"))
			s.Equal("p", m.SelectedID())
		}},
		{"l is right's letter alias", func(m *boardview.Model) {
			m.Update(boardKeyMsg("l"))
			s.Equal("p", m.SelectedID())
		}},
		{"left moves back into the previous column", func(m *boardview.Model) {
			m.MoveRight()
			m.Update(boardKeyMsg("left"))
			s.Equal("a", m.SelectedID())
		}},
		{"h is left's letter alias", func(m *boardview.Model) {
			m.MoveRight()
			m.Update(boardKeyMsg("h"))
			s.Equal("a", m.SelectedID())
		}},
		{"end jumps to the column's last row", func(m *boardview.Model) {
			m.Update(boardKeyMsg("end"))
			s.Equal("c", m.SelectedID())
		}},
		{"G is end's letter alias", func(m *boardview.Model) {
			m.Update(boardKeyMsg("G"))
			s.Equal("c", m.SelectedID())
		}},
		{"home jumps to the column's first row", func(m *boardview.Model) {
			m.JumpToBottom()
			m.Update(boardKeyMsg("home"))
			s.Equal("a", m.SelectedID())
		}},
		{"g is home's letter alias", func(m *boardview.Model) {
			m.JumpToBottom()
			m.Update(boardKeyMsg("g"))
			s.Equal("a", m.SelectedID())
		}},
		{"space toggles expansion", func(m *boardview.Model) {
			before := strings.Count(m.View(), "\n")
			m.Update(boardKeyMsg("space"))
			s.Greater(strings.Count(m.View(), "\n"), before, "space must expand the card and add rows")
		}},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.run(fixture())
		})
	}
}

// TestMoveUpAndMoveLeftActuallyMoveTheCursor guards two of F6's six no-op
// mutants directly: MoveUp's and MoveLeft's bodies removed both leave
// TestNavigationIsTotal green, since that test's own len(seen) > 1
// assertion is satisfied by MoveDown alone as it cycles through the same
// 500-move sequence.
func (s *boardTestSuite) TestMoveUpAndMoveLeftActuallyMoveTheCursor() {
	issues := []beads.Issue{
		{ID: "a", Title: "a", Status: beads.StatusOpen},
		{ID: "b", Title: "b", Status: beads.StatusOpen},
		{ID: "p", Title: "p", Status: beads.StatusInProgress},
	}
	m := s.model(issues, 140, 30)

	m.MoveDown()
	s.Require().Equal("b", m.SelectedID())
	m.MoveUp()
	s.Equal("a", m.SelectedID(), "MoveUp must move the cursor back up")

	m.MoveRight()
	s.Require().Equal("p", m.SelectedID())
	m.MoveLeft()
	s.Equal("a", m.SelectedID(), "MoveLeft must move the cursor back left")
}

// TestJumpToTopAndBottomReachTheColumnEnds guards F6's JumpToTop/JumpToBottom
// no-op mutants: either body removed leaves the cursor exactly where
// TestNavigationIsTotal's mixed-move loop already put it via some other
// call, which len(seen) > 1 cannot distinguish from correct behaviour.
func (s *boardTestSuite) TestJumpToTopAndBottomReachTheColumnEnds() {
	issues := []beads.Issue{
		{ID: "a", Title: "a", Status: beads.StatusOpen},
		{ID: "b", Title: "b", Status: beads.StatusOpen},
		{ID: "c", Title: "c", Status: beads.StatusOpen},
	}
	m := s.model(issues, 140, 30)

	m.JumpToBottom()
	s.Equal("c", m.SelectedID(), "JumpToBottom must land on the column's last id")

	m.JumpToTop()
	s.Equal("a", m.SelectedID(), "JumpToTop must return to the column's first id")
}

// TestSelectByIDMissLeavesTheCursorUntouched guards F6's "SelectByID
// returning true on a miss" mutant: without this, nothing in the suite ever
// checks SelectByID's own boolean result for a lookup that is expected to
// fail, nor that a failed lookup leaves the prior selection alone.
func (s *boardTestSuite) TestSelectByIDMissLeavesTheCursorUntouched() {
	m := s.model(s.sample(), 140, 30)
	s.Require().True(m.SelectByID("b"))

	s.False(m.SelectByID("nope"), "a missing id must report failure")
	s.Equal("b", m.SelectedID(), "a failed lookup must leave the cursor exactly where it was")
}

// TestJumpToBottomScrollsTheOverflowingColumnIntoView guards F6's last
// mutant, columnWindow never following the cursor (start = 0 always) — the
// vertical analogue of F1. A column of 30 cards in a 12-row-tall pane fits
// only a handful at once; JumpToBottom must not just select the last card
// but scroll it into the rendered window, or a viewport pinned at the top
// would select "off-screen" with nothing on screen ever changing.
func (s *boardTestSuite) TestJumpToBottomScrollsTheOverflowingColumnIntoView() {
	issues := make([]beads.Issue, 0, 30)
	for i := range 30 {
		issues = append(issues, beads.Issue{
			ID: fmt.Sprintf("o%02d", i), Title: fmt.Sprintf("o%02d", i), Status: beads.StatusOpen,
		})
	}
	m := s.model(issues, 140, 12)

	m.JumpToBottom()

	s.Equal("o29", m.SelectedID())
	s.Contains(m.View(), "o29", "the last card must actually be rendered, not just selected off-screen")
}

// golden reads a golden file from testdata, or writes actual to it when the
// UPDATE_GOLDEN environment variable is set. Mirrors internal/tui/listview
// and internal/tui/treeview's helper of the same name and shape.
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
