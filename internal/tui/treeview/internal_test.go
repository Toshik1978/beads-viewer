package treeview

// This file holds a white-box suite that reaches into Model's unexported
// fields directly, mirroring internal/tui's own WhiteBoxSuite (internal/tui/
// internal_test.go) for the same reason: some invariants cannot be observed,
// or in this case cannot even be *threatened*, through the exported API
// alone.

import (
	"path/filepath"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// WhiteBoxSuite is exported so treeview_test's entry point (tree_test.go)
// can wire it in, the same way tui_test wires in tui.WhiteBoxSuite.
type WhiteBoxSuite struct {
	suite.Suite
}

// TestVisibleRangeClampsOffsetsTheNavigationAPICanNeverProduce proves the
// negative- and out-of-range guards in visibleRange are load-bearing, even
// though no sequence of exported navigation calls can currently drive
// m.offset out of range: every path that touches offset runs through
// ensureCursorVisible, and ensureCursorVisible only ever runs after the
// cursor has already been reconciled into [0, len(rows)) — ensureCursorVisible's
// own "cursor < offset" branch already lands offset back on a valid index
// whenever the reconciled cursor has moved below the previous offset. That
// closed loop is what makes nav_test.go's own shrinking-tree and resize
// scenarios pass even with the clamp lines deleted (tried directly against
// this suite's sibling in nav_test.go); it does not make the clamp
// pointless, only unreachable via the public surface today. This test
// reaches around that loop — by writing m.offset directly, which no exported
// method allows — to pin the guard against a future change that breaks the
// closed loop from some other call site, rather than leaving it provably
// untested.
func (s *WhiteBoxSuite) TestVisibleRangeClampsOffsetsTheNavigationAPICanNeverProduce() {
	m := New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(80, 5)
	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "a", Title: "a", Status: beads.StatusOpen},
		{ID: "b", Title: "b", Status: beads.StatusOpen},
	}))

	m.offset = -50 // ensureCursorVisible would never produce this itself.
	start, end := m.visibleRange()
	s.GreaterOrEqual(start, 0, "a negative start would panic when View slices rows[start:end]")
	s.LessOrEqual(end, len(m.rows))
	s.NotPanics(func() { _ = m.View() })

	m.offset = 999 // likewise unreachable through the exported API.
	s.NotPanics(func() { _, _ = m.visibleRange() })
}

// TestStateHomeFallsBackToAnAbsolutePath pins M4: when os.UserHomeDir()
// fails (no $HOME — reachable via a sandboxed launcher or a stripped-down
// container, and config.Load already fails earlier in that case in
// practice), the old fallback was the relative literal ".local/state", so a
// Save would create ./.local/state/... under whatever directory bv was
// launched from rather than under an actual state area. os.TempDir() is
// always absolute, which this asserts directly rather than trusting that a
// relative fallback merely "looks fine" in the common case where $HOME is
// set and this branch never runs.
func (s *WhiteBoxSuite) TestStateHomeFallsBackToAnAbsolutePath() {
	s.T().Setenv("XDG_STATE_HOME", "")
	s.T().Setenv("HOME", "")

	s.True(filepath.IsAbs(stateHome()), "stateHome must never fall back to a path relative to cwd")
}
