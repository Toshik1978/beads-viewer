package depsview

// This file holds a white-box suite that reaches into Model's unexported
// fields directly, mirroring treeview's own internal_test.go (and, further
// back, tui's) for the same reason: some invariants cannot be observed
// through the exported API alone — positioning the cursor on an interior row,
// or reading m.col/m.row back after a move, has no path through Model's
// exported surface, since every exported accessor already bounds-checks the
// cursor before use.

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// WhiteBoxSuite is exported so depsview_test's entry point (deps_test.go)
// can wire it in, the same way tree_test.go wires in treeview.WhiteBoxSuite.
type WhiteBoxSuite struct {
	suite.Suite
}

// TestEntryWindowKeepsAnInteriorCursorVisible pins bv-7pt.5.2 fix round 2:
// entryWindow's start and fitCount's end must agree on real per-entry costs,
// or a cursor positioned between them renders as if it did not exist. The
// fixture matches the round-2 reproduction exactly — ten RelationRelated
// entries (each needing a label, so each costs cardfmt.Height(false)+1 = 5
// lines) at avail = 10 - headerLines(1) = 9, cursor at index 5 — which
// previously computed start=4 from an assumed uniform cost of 4 while
// fitCount, using the real cost of 5, could only fit one entry from there
// and produced end=5: a window that excluded the very cursor it was meant
// to keep visible.
func (s *WhiteBoxSuite) TestEntryWindowKeepsAnInteriorCursorVisible() {
	issues := []beads.Issue{{ID: "focus", Title: "focus", Status: beads.StatusOpen, IssueType: beads.TypeTask}}
	for i := 1; i <= 10; i++ {
		id := fmt.Sprintf("rel%d", i)
		issues[0].Dependencies = append(issues[0].Dependencies, beads.Dependency{
			IssueID: "focus", DependsOnID: id, Type: beads.DepRelated,
		})
		issues = append(issues, beads.Issue{ID: id, Title: id, Status: beads.StatusOpen, IssueType: beads.TypeTask})
	}

	m := New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(200, 10) // avail = m.height - headerLines(1) = 9, matching the reproduction.
	m.SetSnapshot(beads.NewSnapshot(issues))
	s.Require().True(m.Reveal("focus"))

	related := m.columns[3]
	s.Require().Len(related.Entries, 10, "the related column must hold all ten before the cursor is placed")
	cursorID := related.Entries[5].ID

	// There is no exported way to place the cursor on an interior row —
	// that is exactly why this test lives beside Model rather than in
	// deps_test.go.
	m.col, m.row = 3, 5

	out := ansi.Strip(m.View())
	s.Contains(out, cursorID, "the cursor's own entry must appear in the rendered frame")
}

// TestMovementNeverLeavesAnInvalidCursor pins the real postcondition every
// movement method must hold, not merely "doesn't panic": after any sequence
// of moves, m.col must index an existing column, and m.row must either index
// that column's entries or be 0 in an empty column. It lives here, not in
// deps_test.go, because every accessor the external package can reach
// (Selected, SelectedID) already bounds-checks the cursor before use — a
// movement method that forgot to call clamp() at all could not be observed
// from outside, since nothing external ever indexes m.columns[m.col] without
// its own guard.
func (s *WhiteBoxSuite) TestMovementNeverLeavesAnInvalidCursor() {
	// "gone" is a dangling blocker (an id with no issue behind it, still a
	// valid row) and "related" stays empty throughout — the two cases the
	// old, externally observable version of this test could not distinguish,
	// since sample() (deps_test.go) has neither.
	snap := beads.NewSnapshot([]beads.Issue{
		{
			ID: "focus", Title: "focus", Status: beads.StatusOpen, IssueType: beads.TypeTask,
			Dependencies: []beads.Dependency{{IssueID: "focus", DependsOnID: "gone", Type: beads.DepBlocks}},
		},
		{
			ID: "live", Title: "live", Status: beads.StatusOpen, IssueType: beads.TypeTask,
			Dependencies: []beads.Dependency{{IssueID: "live", DependsOnID: "focus", Type: beads.DepBlocks}},
		},
	})

	m := New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(120, 20)
	m.SetSnapshot(snap)
	s.Require().True(m.Reveal("focus"))

	moves := []func(){m.MoveUp, m.MoveDown, m.MoveLeft, m.MoveRight, m.JumpToTop, m.JumpToBottom}
	for i := range 200 {
		moves[i%len(moves)]()

		s.Require().True(m.col >= 0 && m.col < len(m.columns), "col %d out of range after move %d", m.col, i)

		entries := m.columns[m.col].Entries
		if len(entries) == 0 {
			s.Require().Equal(0, m.row, "an empty column must park row at 0, move %d", i)

			continue
		}
		s.Require().True(m.row >= 0 && m.row < len(entries), "row %d out of range for column %d, move %d",
			m.row, m.col, i)
	}

	s.NotPanics(func() { _ = m.View() }, "the cursor the loop leaves behind must still be renderable")
}
