package depsview

// This file holds a white-box suite that reaches into Model's unexported
// fields directly, mirroring treeview's own internal_test.go (and, further
// back, tui's) for the same reason: some invariants cannot be observed
// through the exported API alone. Here specifically, there is no
// cursor-movement method yet — Update is still a stub, and the next task
// adds it — so positioning the cursor on an interior row to test the
// window's own cursor-visibility guarantee has no path through Model's
// exported surface at all.

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
