package treeview_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
)

// keyMsg builds the key press Update expects for a single printable
// character, or the literal space bar.
func keyMsg(key string) tea.KeyPressMsg {
	if key == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace}
	}

	r, _ := utf8.DecodeRuneInString(key)

	return tea.KeyPressMsg{Code: r}
}

type navTestSuite struct {
	suite.Suite
}

func (s *navTestSuite) model(depth, breadth, height int) *treeview.Model {
	issues := make([]beads.Issue, 0, 1+breadth*(1+depth))
	issues = append(issues, beads.Issue{ID: "root", Title: "root", Status: beads.StatusOpen})
	for i := range breadth {
		id := fmt.Sprintf("c%d", i)
		issues = append(issues, child(id, "root"))
		for j := range depth {
			issues = append(issues, child(fmt.Sprintf("%s-g%d", id, j), id))
		}
	}
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, height)
	m.SetSnapshot(beads.NewSnapshot(issues))

	return m
}

func (s *navTestSuite) TestMovementStaysInBounds() {
	m := s.model(2, 3, 10)
	m.ExpandAll()

	for range 200 {
		m.MoveUp()
	}
	s.Equal("root", m.SelectedID(), "moving up past the top must stop at the top")

	for range 200 {
		m.MoveDown()
	}
	s.NotEmpty(m.SelectedID(), "moving down past the end must stop at the end")
	s.NotPanics(func() { _ = m.View() })
}

func (s *navTestSuite) TestPagingAndJumps() {
	m := s.model(2, 5, 8)
	m.ExpandAll()

	m.JumpToBottom()
	bottom := m.SelectedID()
	m.JumpToTop()
	s.Equal("root", m.SelectedID())

	m.PageDown()
	s.NotEqual("root", m.SelectedID())
	m.PageUp()
	s.Equal("root", m.SelectedID())

	for range 100 {
		m.PageDown()
	}
	s.Equal(bottom, m.SelectedID())
}

func (s *navTestSuite) TestJumpToParent() {
	m := s.model(1, 2, 20)
	m.ExpandAll()
	s.Require().True(m.SelectByID("c0-g0"))

	m.JumpToParent()
	s.Equal("c0", m.SelectedID())
	m.JumpToParent()
	s.Equal("root", m.SelectedID())
	m.JumpToParent()
	s.Equal("root", m.SelectedID(), "a root has no parent to jump to")
}

func (s *navTestSuite) TestExpandOrDescendAndCollapseOrAscend() {
	m := s.model(1, 2, 20)
	m.CollapseAll()
	s.Require().True(m.SelectByID("root"))

	m.ExpandOrDescend() // collapsed: expands
	s.Equal("root", m.SelectedID())
	m.ExpandOrDescend() // expanded: moves to the first child
	s.Equal("c0", m.SelectedID())

	m.CollapseOrAscend() // expanded child: collapses
	m.CollapseOrAscend() // already collapsed: ascends
	s.Equal("root", m.SelectedID())
}

// TestShrinkingRowCountDoesNotPanic is the negative-offset regression guard.
func (s *navTestSuite) TestShrinkingRowCountDoesNotPanic() {
	m := s.model(3, 5, 5)
	m.ExpandAll()
	m.JumpToBottom() // offset is now well past zero

	// Collapsing everything cuts the row count to one, far below the offset.
	s.NotPanics(func() {
		m.CollapseAll()
		_ = m.View()
	})

	// So does a reload that shrinks the data.
	m.ExpandAll()
	m.JumpToBottom()
	s.NotPanics(func() {
		m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
			{ID: "root", Title: "root", Status: beads.StatusOpen},
		}))
		_ = m.View()
	})
}

func (s *navTestSuite) TestCursorStaysVisible() {
	m := s.model(3, 5, 5)
	m.ExpandAll()

	for range 30 {
		m.MoveDown()
		s.Contains(m.View(), m.SelectedID(),
			"the selected row must always be inside the window")
	}
}

// TestOffsetAdvancesOnlyWhenTheCursorLeavesTheWindow pins ensureCursorVisible
// as an actual scroll, not a no-op that happens to pass the cursor-visible
// check above. A stub ensureCursorVisible would leave View's output
// unchanged by MoveDown right up until the row it dropped off is the very
// last one on screen — this catches the "does nothing while already
// visible" case directly, by asserting the window is stable while the
// cursor stays inside it and only moves once the cursor actually leaves it.
func (s *navTestSuite) TestOffsetAdvancesOnlyWhenTheCursorLeavesTheWindow() {
	m := s.model(0, 10, 5) // root + 10 children, no grandchildren: 11 rows.
	m.ExpandAll()
	m.JumpToTop()

	m.MoveDown() // cursor=1, still inside the initial [0,5) window.
	s.NotContains(ansi.Strip(m.View()), "c4",
		"c4 (row 5) must still be outside the window while the cursor is inside it")

	for range 4 {
		m.MoveDown()
	}
	// cursor=5, outside [0,5): the window must have scrolled by exactly one,
	// bringing c4 into view and pushing root's row out of it.
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	s.Contains(lines[len(lines)-1], "c4", "the window must scroll once the cursor leaves it")
	s.NotContains(lines[0], "root", "root's row must have scrolled out of the window")
}

// TestSelectionFallbackClampsToTheNearestRowNotAlwaysZero guards against the
// exact defect this project has already shipped once: a fallback that always
// selects row 0 would pass every other fixture in this suite, because row 0
// happens to be the right answer in each of them too. Selecting at a
// non-zero cursor, then shrinking the data so that row disappears but at
// least two rows remain, is what tells "clamp to the nearest surviving row"
// apart from "always fall back to the first row".
func (s *navTestSuite) TestSelectionFallbackClampsToTheNearestRowNotAlwaysZero() {
	m := s.model(0, 5, 20) // root, c0..c4: rows root=0, c0=1, ..., c4=5.
	m.ExpandAll()
	s.Require().True(m.SelectByID("c4")) // cursor=5, the last row.

	// Drop c4 but keep three rows before it (root, c0, c1) — a "clamp to
	// cursor, which is now out of range" implementation lands on c1 (the new
	// last row); "always row 0" would land on root instead.
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		child("c0", "root"), child("c1", "root"),
	}))

	s.Equal("c1", m.SelectedID(),
		"the fallback must clamp to the nearest surviving row, not always the first one")
}

func (s *navTestSuite) TestHideClosedCounts() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		child("open", "root"),
		{
			ID: "done", Title: "done", Status: beads.StatusClosed,
			Dependencies: []beads.Dependency{
				{IssueID: "done", DependsOnID: "root", Type: beads.DepParentChild},
			},
		},
	}))
	m.ExpandAll()

	s.Zero(m.HiddenCount())
	m.ToggleHideClosed()
	s.Equal(1, m.HiddenCount())
	s.NotContains(ansi.Strip(m.View()), "done")

	// Toggling back must restore it. Without this, a ToggleHideClosed that
	// pruned the tree destructively and never rebuilt from Build(snap) would
	// pass every assertion above.
	m.ToggleHideClosed()
	s.Zero(m.HiddenCount())
	s.Contains(ansi.Strip(m.View()), "done")

	// HiddenCount must count nodes, not rows: a HiddenCount that used
	// len(m.rows) instead of countNodes(m.roots) agrees with every assertion
	// above, because ExpandAll made rows == nodes throughout. Collapsing
	// everything shrinks the row count without changing how many issues
	// hide-closed is actually hiding, so the count must not move.
	m.ToggleHideClosed()
	s.Equal(1, m.HiddenCount())
	m.CollapseAll()
	s.Equal(1, m.HiddenCount(), "collapsing must never change how many issues hide-closed hides")
	m.ToggleHideClosed()
}

// TestHideClosedWithOnlyClosedIssuesRendersAMessageNotABlankPane is
// Important 2's regression guard, reproduced from the live report: filtering
// a workspace down to a single closed issue, then pressing 'c', used to
// render an entirely blank tree pane — confirmed by rebuilding with the
// message branch removed and replaying these exact steps. The status bar
// still reports issues and a matching filter beside that blank pane, with
// nothing telling the reader 'c' is the way back, which is what this test's
// own message assertion (not merely NotEmpty) actually pins.
func (s *navTestSuite) TestHideClosedWithOnlyClosedIssuesRendersAMessageNotABlankPane() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "closed", Title: "closed only", Status: beads.StatusClosed},
	}))

	m.ToggleHideClosed()

	out := ansi.Strip(m.View())
	s.NotEmpty(strings.TrimSpace(out), "the pane must never render blank")
	s.Contains(out, "Press c to show closed", "the message must hint that 'c' is the way back")
}

// TestHiddenCountIsZeroWhenHideClosedIsOff pins I4: Retain's zero filter
// still drops a tombstone-only subtree on its own (see tree.go's Retain), so
// snapshot.Len() minus countNodes(m.roots) is not zero even with hide-closed
// off — it attributes that tombstone drop to hide-closed regardless. A
// status bar built on this would render "N hidden" with the feature
// disabled, which is what this fixture (a tombstone leaf, hide-closed off)
// reproduces.
func (s *navTestSuite) TestHiddenCountIsZeroWhenHideClosedIsOff() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		{
			ID: "tomb-leaf", Title: "tomb-leaf", Status: beads.StatusTombstone,
			Dependencies: []beads.Dependency{
				{IssueID: "tomb-leaf", DependsOnID: "root", Type: beads.DepParentChild},
			},
		},
	}))
	m.ExpandAll()

	s.Zero(m.HiddenCount(), "hide-closed is off; nothing it hides should be reported")
}

// TestHideClosedKeepsAClosedParentOfAnOpenChild pins the behaviour that
// distinguishes Retain from the old Flatten(roots, hideClosed): a closed epic
// with live work under it must stay, dimmed, rather than taking its child
// down with it.
func (s *navTestSuite) TestHideClosedKeepsAClosedParentOfAnOpenChild() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "epic", Title: "finished epic", Status: beads.StatusClosed},
		child("live", "epic"),
	}))
	m.ExpandAll()
	m.ToggleHideClosed()

	out := ansi.Strip(m.View())
	s.Contains(out, "live", "an open child must never be hidden")
	s.Contains(out, "finished epic", "its closed parent is kept for reachability")
}

func (s *navTestSuite) TestSelectionSurvivesAReload() {
	m := s.model(1, 3, 20)
	m.ExpandAll()
	s.Require().True(m.SelectByID("c1"))

	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		child("c0", "root"), child("c1", "root"), child("c2", "root"),
		{
			ID: "brand-new", Title: "new", Status: beads.StatusOpen,
			Priority: beads.PriorityCritical,
		},
	}))

	s.Equal("c1", m.SelectedID(), "selection is by id, not row index")
}

func (s *navTestSuite) TestSelectionFallsBackWhenTheIssueDisappears() {
	m := s.model(1, 3, 20)
	m.ExpandAll()
	s.Require().True(m.SelectByID("c1"))

	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
	}))

	s.Equal("root", m.SelectedID(), "a deleted selection falls back, never dangles")
}

// TestSetSizeShrinkingKeepsCursorVisible pins the extra call this task adds
// beyond the brief: SetSize re-clamps the viewport too. A terminal resize
// can shrink the pane height below where the offset currently sits, exactly
// like CollapseAll or a shrinking reload do — nothing else in this suite
// calls SetSize after the cursor has scrolled, so without its own
// ensureCursorVisible call this would go uncovered.
func (s *navTestSuite) TestSetSizeShrinkingKeepsCursorVisible() {
	m := s.model(0, 20, 10) // root + 20 children: 21 rows, height 10.
	m.ExpandAll()
	m.JumpToBottom()

	s.NotPanics(func() {
		m.SetSize(80, 3)
		_ = m.View()
	})
	s.Contains(m.View(), m.SelectedID(), "the cursor must still be inside the shrunken window")
}

// TestUpdateRoutesKeysToNavigation exercises Update itself, the one thing
// nothing else in this suite touches: every test above calls the navigation
// methods directly. A tui.View that satisfies the interface but never wires
// a single key to it would pass every other test here.
func (s *navTestSuite) TestUpdateRoutesKeysToNavigation() {
	m := s.model(1, 3, 20)
	m.Update(keyMsg("2")) // '2' is unbound in the tree; a no-op key.

	m.Update(keyMsg("j"))
	s.NotEqual("root", m.SelectedID(), "j must move the cursor down")

	m.Update(keyMsg("k"))
	s.Equal("root", m.SelectedID(), "k must move the cursor back up")

	m.Update(keyMsg("G"))
	bottom := m.SelectedID()
	s.NotEqual("root", bottom, "G must jump to the bottom")

	m.Update(keyMsg("g"))
	s.Equal("root", m.SelectedID(), "g must jump back to the top")

	m.CollapseAll()
	s.Require().True(m.SelectByID("root"))
	m.Update(keyMsg("space"))
	s.Contains(ansi.Strip(m.View()), "c0", "space must expand the node under the cursor")
}

// TestSelectionAndExpansionSurviveAWatcherReloadAtDepthTwo is C1's
// regression guard. rebuild used to call Build(m.snapshot) unconditionally,
// and Build's Expanded default is depth == 0 — a fresh Build collapses every
// node below the root, discarding whatever the user had expanded. SetSnapshot
// is the live watcher-reload path (app.go's applySnapshot -> every view's
// SetSnapshot), so any br write to issues.jsonl while bv was open used to
// collapse the tree out from under the user and strand the selection, once
// it sat two or more levels deep — nav_test.go's fixtures at depth 1 could
// not see this, because Build leaves a depth-1 node visible regardless.
func (s *navTestSuite) TestSelectionAndExpansionSurviveAWatcherReloadAtDepthTwo() {
	issues := []beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		child("mid", "root"),
		child("leaf", "mid"), // depth 2: only visible once "mid" is expanded.
	}
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot(issues))

	s.Require().True(m.SelectByID("mid"))
	m.ToggleExpand() // reveal "leaf".
	s.Require().True(m.SelectByID("leaf"))

	before := strings.Count(ansi.Strip(m.View()), "\n") + 1

	// An identical snapshot reload, exactly as the watcher delivers on every
	// br write to issues.jsonl.
	m.SetSnapshot(beads.NewSnapshot(issues))

	after := strings.Count(ansi.Strip(m.View()), "\n") + 1
	s.Equal(before, after, "a reload must not collapse expansion the user already chose")
	s.Equal("leaf", m.SelectedID(), "the depth-2 selection must survive an identical reload")
}

// TestToggleHideClosedTwiceRestoresExpansionExactly is C1's second guard:
// ToggleHideClosed shares rebuild with SetSnapshot, and the same
// Build-resets-everything defect collapsed the tree on every toggle, not
// only on a reload.
func (s *navTestSuite) TestToggleHideClosedTwiceRestoresExpansionExactly() {
	m := s.model(1, 3, 20)
	s.Require().True(m.SelectByID("c0"))
	m.ToggleExpand() // c0 expanded, c0-g0 visible.

	before := ansi.Strip(m.View())

	m.ToggleHideClosed()
	m.ToggleHideClosed()

	s.Equal(before, ansi.Strip(m.View()),
		"toggling hide-closed twice must restore the exact expanded row set")
}

// TestToggleHideClosedTwiceRestoresASubtreeHideClosedPrunedEntirely is a
// stronger version of the sibling test above, using a fixture the reviewer's
// own manual repro against this repository's real workspace exposed: a
// subtree that is wholly closed gets removed from the tree entirely by
// Retain while hide-closed is on, not merely hidden a row at a time. A
// version of the fix that captured "currently expanded" fresh from m.roots on
// every rebuild — rather than keeping a persistent, id-keyed record — loses
// that subtree's expansion permanently the moment it is pruned: toggling
// hide-closed on and back off left this fixture's row count at 5 instead of
// the original 7, never recovering "leaf"'s visibility.
func (s *navTestSuite) TestToggleHideClosedTwiceRestoresASubtreeHideClosedPrunedEntirely() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		child("open-mid", "root"),
		{
			ID: "closed-mid", Title: "closed-mid", Status: beads.StatusClosed,
			Dependencies: []beads.Dependency{
				{IssueID: "closed-mid", DependsOnID: "root", Type: beads.DepParentChild},
			},
		},
		{
			ID: "leaf", Title: "leaf", Status: beads.StatusClosed,
			Dependencies: []beads.Dependency{
				{IssueID: "leaf", DependsOnID: "closed-mid", Type: beads.DepParentChild},
			},
		},
	}))
	s.Require().True(m.SelectByID("closed-mid"))
	m.ToggleExpand() // reveal "leaf" — a wholly-closed subtree, entirely pruned once hide-closed is on.

	before := ansi.Strip(m.View())

	m.ToggleHideClosed() // "closed-mid" and "leaf" both vanish: nothing under root matches or has a live descendant.
	m.ToggleHideClosed()

	s.Equal(before, ansi.Strip(m.View()),
		"a subtree hide-closed pruned entirely must still recover its exact expansion once the filter loosens again")
}

// TestWindowStaysFullAfterAShrinkThatLeavesEnoughRows pins I3: the reviewer
// found ensureCursorVisible's final clamp line
// (m.offset = min(max(m.offset, 0), max(len(m.rows)-m.height, 0))) reachable
// through exported calls alone, contrary to the previous implementation
// report. Deleting that single line left the entire go test ./... suite
// green while the window rendered fewer lines than its own height after a
// reload that shrinks the row count but still leaves more than height rows —
// a half-empty pane, not a crash, so nothing else in this suite happened to
// notice.
func (s *navTestSuite) TestWindowStaysFullAfterAShrinkThatLeavesEnoughRows() {
	m := s.model(0, 20, 5) // root + 20 children: 21 rows, height 5.
	m.ExpandAll()
	m.JumpToBottom()

	issues := make([]beads.Issue, 0, 19)
	issues = append(issues, beads.Issue{ID: "root", Title: "root", Status: beads.StatusOpen})
	for i := range 18 {
		issues = append(issues, child(fmt.Sprintf("d%d", i), "root"))
	}
	m.SetSnapshot(beads.NewSnapshot(issues)) // 19 rows: still more than height.
	m.ExpandAll()

	s.Len(strings.Split(ansi.Strip(m.View()), "\n"), 5,
		"the window must stay full after a shrink that leaves enough rows")
}

// TestMoveDownThenMoveUpReturnsToTheStartingRow guards MoveUp against an off
// by one: a MoveUp implemented as cursor-2 would still stop at row 0 from
// near the top (every fixture elsewhere in this suite starts there), but from
// a genuine mid-list position it lands one row short of where MoveDown put
// the cursor back from.
func (s *navTestSuite) TestMoveDownThenMoveUpReturnsToTheStartingRow() {
	m := s.model(0, 10, 20) // root + 10 children, all visible at height 20.
	m.ExpandAll()
	s.Require().True(m.SelectByID("c5")) // a genuine mid-list position.

	m.MoveDown()
	m.MoveUp()

	s.Equal("c5", m.SelectedID(), "MoveDown then MoveUp must return to the exact starting row")
}

// TestSelectByIDReportsFalseOnAMiss guards the miss branch: a SelectByID that
// always returns true regardless of whether the id exists would pass every
// other test in this suite, since none of them check the return value on a
// failing lookup.
func (s *navTestSuite) TestSelectByIDReportsFalseOnAMiss() {
	m := s.model(0, 3, 20)

	s.False(m.SelectByID("does-not-exist"))
}

// TestExpandByIDRoundTripsExactExpansionAtDepthTwo is M1's guard for both
// directions of the mutant space: expandByID as a no-op, and expandByID
// expanding everything regardless of the ids given, both survive the rest of
// this suite. A fixture with one node the user actually expanded and a
// sibling the user left alone, both at depth two, tells the two mutants
// apart from the real behaviour and from each other.
func (s *navTestSuite) TestExpandByIDRoundTripsExactExpansionAtDepthTwo() {
	stateDir := s.T().TempDir()
	s.T().Setenv("XDG_STATE_HOME", stateDir)
	beadsDir := filepath.Join(s.T().TempDir(), ".beads")

	issues := []beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		child("expanded-mid", "root"),
		child("expanded-leaf", "expanded-mid"),
		child("collapsed-mid", "root"),
		child("collapsed-leaf", "collapsed-mid"),
	}

	saver := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	saver.SetSize(80, 20)
	saver.SetSnapshot(beads.NewSnapshot(issues))
	s.Require().True(saver.SelectByID("expanded-mid"))
	saver.ToggleExpand() // only expanded-mid opens; collapsed-mid stays shut.
	s.Require().NoError(saver.ExportState().Save(beadsDir))

	loader := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	loader.SetSize(80, 20)
	loader.SetSnapshot(beads.NewSnapshot(issues))
	state, found, err := treeview.LoadState(beadsDir)
	s.Require().NoError(err)
	s.Require().True(found)
	loader.ApplyState(state)

	out := ansi.Strip(loader.View())
	s.Contains(out, "expanded-leaf",
		"a saved depth-2 expansion must be restored — an expandByID no-op survives without this")
	s.NotContains(out, "collapsed-leaf",
		"a node the user never expanded must stay collapsed — "+
			"an expandByID that expands everything survives without this")
}

// TestApplyStateAppliesHideClosed closes M1's other gap: ApplyState's own
// s.HideClosed round-trips through State (state_test.go's TestRoundTrip
// covers that) but was never observed to actually take effect once restored.
func (s *navTestSuite) TestApplyStateAppliesHideClosed() {
	m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 20)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "root", Title: "root", Status: beads.StatusOpen},
		child("open", "root"),
		{
			ID: "done", Title: "done", Status: beads.StatusClosed,
			Dependencies: []beads.Dependency{
				{IssueID: "done", DependsOnID: "root", Type: beads.DepParentChild},
			},
		},
	}))

	// Expanded explicitly names "root" so the root's own collapse (every
	// unnamed node closes, per expandByID) cannot be mistaken for hide-closed
	// having applied — "done" must be missing because Retain pruned a
	// non-matching closed leaf outright, not because the whole tree closed.
	m.ApplyState(treeview.State{Expanded: []string{"root"}, HideClosed: true})

	out := ansi.Strip(m.View())
	s.Contains(out, "open", "an open sibling must still render, proving the tree is not just collapsed")
	s.NotContains(out, "done",
		"ApplyState must apply s.HideClosed, not merely carry it in the struct")
}

// TestSelectedRowStyleDiffersFromUnselected pins the Global Constraints'
// minimum styling bar: the cursor row must differ from an unselected one. It
// isolates the styling difference from content by rendering the exact same
// node (c0) once while it is not the cursor and once while it is, rather
// than comparing two different rows whose text alone would already differ —
// a `selected := false` regression in render.go would pass a same-row
// comparison for the wrong reason if the content itself changed too.
func (s *navTestSuite) TestSelectedRowStyleDiffersFromUnselected() {
	m := s.model(0, 3, 10)
	m.JumpToTop() // cursor on root; c0 (row index 1) is not selected.

	unselectedC0 := strings.Split(m.View(), "\n")[1]

	m.MoveDown() // cursor now on c0.
	selectedC0 := strings.Split(m.View(), "\n")[1]

	s.NotEqual(unselectedC0, selectedC0,
		"a row's rendered style must change once it becomes the selected row")
}

// TestEveryBoundKeyProducesItsOwnObservableEffect is I6's guard. Mutants
// deleting h/l, deleting o/O/c/p, and deleting the paging keys from
// keyActions all survived the rest of this suite — TestUpdateRoutesKeys-
// ToNavigation only drives j/k/g/G/space, and every other assertion in this
// file calls the underlying method (m.ExpandAll(), m.ToggleHideClosed(), …)
// directly rather than routing a key through Update. This drives every entry
// in keyActions through Update itself, both spellings where one exists, and
// asserts an effect that specific key alone produces.
func (s *navTestSuite) TestEveryBoundKeyProducesItsOwnObservableEffect() {
	// A fixture with a two-level open branch (so expand/collapse/descend have
	// somewhere to go) and a closed leaf (so hide-closed has something to
	// hide) that "c" alone can toggle.
	build := func() *treeview.Model {
		m := treeview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
		m.SetSize(80, 20)
		m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
			{ID: "root", Title: "root", Status: beads.StatusOpen},
			child("open-mid", "root"),
			child("open-leaf", "open-mid"),
			{
				ID: "closed-mid", Title: "closed-mid", Status: beads.StatusClosed,
				Dependencies: []beads.Dependency{
					{IssueID: "closed-mid", DependsOnID: "root", Type: beads.DepParentChild},
				},
			},
		}))

		return m
	}

	s.Run("up and k move the cursor up", func() {
		for _, msg := range []tea.KeyPressMsg{{Code: tea.KeyUp}, keyMsg("k")} {
			m := build()
			m.MoveDown()
			m.Update(msg)
			s.Equal("root", m.SelectedID(), "%v must move the cursor back up", msg)
		}
	})

	s.Run("down and j move the cursor down", func() {
		for _, msg := range []tea.KeyPressMsg{{Code: tea.KeyDown}, keyMsg("j")} {
			m := build()
			m.Update(msg)
			s.NotEqual("root", m.SelectedID(), "%v must move the cursor down", msg)
		}
	})

	s.Run("left and h collapse the node under the cursor", func() {
		for _, msg := range []tea.KeyPressMsg{{Code: tea.KeyLeft}, keyMsg("h")} {
			m := build()
			s.Require().True(m.SelectByID("open-mid"))
			m.ToggleExpand()
			s.Require().Contains(ansi.Strip(m.View()), "open-leaf")

			m.Update(msg)
			s.NotContains(ansi.Strip(m.View()), "open-leaf", "%v must collapse the expanded node", msg)
		}
	})

	s.Run("right and l expand the node under the cursor", func() {
		for _, msg := range []tea.KeyPressMsg{{Code: tea.KeyRight}, keyMsg("l")} {
			m := build()
			s.Require().True(m.SelectByID("open-mid"))

			m.Update(msg)
			s.Contains(ansi.Strip(m.View()), "open-leaf", "%v must expand the collapsed node", msg)
		}
	})

	s.Run("space toggles the node under the cursor", func() {
		m := build()
		s.Require().True(m.SelectByID("open-mid"))

		m.Update(keyMsg("space"))
		s.Contains(ansi.Strip(m.View()), "open-leaf", "space must expand the node under the cursor")
	})

	s.Run("home and g jump to the top", func() {
		for _, msg := range []tea.KeyPressMsg{{Code: tea.KeyHome}, keyMsg("g")} {
			m := build()
			m.MoveDown()
			m.Update(msg)
			s.Equal("root", m.SelectedID(), "%v must jump to the top", msg)
		}
	})

	s.Run("end and G jump to the bottom", func() {
		for _, msg := range []tea.KeyPressMsg{{Code: tea.KeyEnd}, keyMsg("G")} {
			m := build()
			m.Update(msg)
			s.NotEqual("root", m.SelectedID(), "%v must jump to the bottom", msg)
		}
	})

	s.Run("p jumps to the parent", func() {
		m := build()
		s.Require().True(m.SelectByID("open-mid"))

		m.Update(keyMsg("p"))
		s.Equal("root", m.SelectedID(), "p must jump to the parent")
	})

	s.Run("o expands every node", func() {
		m := build()
		m.Update(keyMsg("o"))
		s.Contains(ansi.Strip(m.View()), "open-leaf", "o must expand every node")
	})

	s.Run("O collapses every node", func() {
		m := build()
		m.ExpandAll()
		s.Require().Contains(ansi.Strip(m.View()), "open-leaf")

		m.Update(keyMsg("O"))
		s.NotContains(ansi.Strip(m.View()), "open-leaf", "O must collapse every node")
	})

	s.Run("c toggles hide-closed", func() {
		m := build()
		s.Require().Zero(m.HiddenCount())

		m.Update(keyMsg("c"))
		s.Equal(1, m.HiddenCount(), "c must toggle hide-closed")
	})

	s.Run("ctrl+b and ctrl+f page the tree", func() {
		m := s.model(0, 20, 5) // enough rows that a page actually moves the cursor.
		m.ExpandAll()

		m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
		s.NotEqual("root", m.SelectedID(), "ctrl+f must page down")

		m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
		s.Equal("root", m.SelectedID(), "ctrl+b must page back up")
	})
}
