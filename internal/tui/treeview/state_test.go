package treeview_test

import (
	"os"
	"path/filepath"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
)

type stateTestSuite struct {
	suite.Suite
}

func (s *stateTestSuite) TestRoundTrip() {
	dir := s.T().TempDir()
	s.T().Setenv("XDG_STATE_HOME", dir)

	want := treeview.State{
		Expanded: []string{"epic", "epic.1"},
		Selected: "epic.1",
	}
	s.Require().NoError(want.Save("/some/project/.beads"))

	got, found, err := treeview.LoadState("/some/project/.beads")
	s.Require().NoError(err)
	s.True(found, "a state that was just saved must be reported as found")
	s.ElementsMatch(want.Expanded, got.Expanded)
	s.Equal(want.Selected, got.Selected)
}

func (s *stateTestSuite) TestAStateFileFromBeforeThePromotionStillLoads() {
	dir := s.T().TempDir()
	s.T().Setenv("XDG_STATE_HOME", dir)

	// Written by a version that persisted hide_closed. An unknown field must
	// be ignored, not refuse the whole file — losing a user's expansion state
	// over a retired field would be worse than the field itself.
	s.Require().NoError(treeview.State{Selected: "bv-1"}.Save("/p/.beads"))
	path := treeview.StatePath("/p/.beads")
	s.Require().NoError(os.WriteFile(path,
		[]byte(`{"expanded":["bv-1"],"selected":"bv-1","hide_closed":true}`), 0o600))

	state, found, err := treeview.LoadState("/p/.beads")

	s.Require().NoError(err)
	s.True(found)
	s.Equal([]string{"bv-1"}, state.Expanded)
	s.Equal("bv-1", state.Selected)
}

func (s *stateTestSuite) TestWorkspacesDoNotShareState() {
	dir := s.T().TempDir()
	s.T().Setenv("XDG_STATE_HOME", dir)

	s.Require().NoError(treeview.State{Selected: "a"}.Save("/project/one/.beads"))
	s.Require().NoError(treeview.State{Selected: "b"}.Save("/project/two/.beads"))

	one, found, err := treeview.LoadState("/project/one/.beads")
	s.Require().NoError(err)
	s.True(found)
	s.Equal("a", one.Selected)
}

// TestRelativeWorkspacesDoNotShareState is I2's regression guard.
// TestWorkspacesDoNotShareState above uses only absolute paths, so it cannot
// see the bug: StatePath used to hash filepath.Clean(beadsDir) — the path as
// typed — and beads.Workspace.Dir is documented as not normalised, so two
// distinct workspaces both invoked as the relative "--db .beads" from
// different working directories hashed to the exact same file. This resolves
// each relative path against its own working directory, the same way two
// separate `bv` process invocations would, and asserts the states stay apart.
func (s *stateTestSuite) TestRelativeWorkspacesDoNotShareState() {
	dir := s.T().TempDir()
	s.T().Setenv("XDG_STATE_HOME", dir)

	one := s.T().TempDir()
	two := s.T().TempDir()

	withWorkdir := func(wd string, fn func()) {
		prev, err := os.Getwd()
		s.Require().NoError(err)
		s.Require().NoError(os.Chdir(wd))
		defer func() { s.Require().NoError(os.Chdir(prev)) }()
		fn()
	}

	withWorkdir(one, func() {
		s.Require().NoError(treeview.State{Selected: "a"}.Save(".beads"))
	})
	withWorkdir(two, func() {
		s.Require().NoError(treeview.State{Selected: "b"}.Save(".beads"))
	})

	var gotOne, gotTwo treeview.State
	withWorkdir(one, func() {
		state, found, err := treeview.LoadState(".beads")
		s.Require().NoError(err)
		s.Require().True(found)
		gotOne = state
	})
	withWorkdir(two, func() {
		state, found, err := treeview.LoadState(".beads")
		s.Require().NoError(err)
		s.Require().True(found)
		gotTwo = state
	})

	s.Equal("a", gotOne.Selected)
	s.Equal("b", gotTwo.Selected, "a relative --db from a different working directory must not share state")
}

func (s *stateTestSuite) TestMissingStateIsEmptyNotAnError() {
	s.T().Setenv("XDG_STATE_HOME", s.T().TempDir())

	got, found, err := treeview.LoadState("/never/seen/.beads")
	s.Require().NoError(err)
	s.False(found, "nothing was ever saved for this workspace")
	s.Empty(got.Expanded)
}

func (s *stateTestSuite) TestCorruptStateIsIgnored() {
	dir := s.T().TempDir()
	s.T().Setenv("XDG_STATE_HOME", dir)
	s.Require().NoError(treeview.State{Selected: "a"}.Save("/p/.beads"))

	// Truncate the file mid-write, as a crash would.
	path := treeview.StatePath("/p/.beads")
	s.Require().NoError(os.WriteFile(path, []byte("{\"expanded\":"), 0o600))

	got, found, err := treeview.LoadState("/p/.beads")
	s.Require().NoError(err, "forgetting expansion state must never block startup")
	s.False(found, "a corrupt file is nothing usable to restore")
	s.Empty(got.Expanded)
}

// TestNeverWritesInsideBeads guards the read-only invariant. Tree state is
// bv's own preference, not tracker data, and .beads belongs to br.
func (s *stateTestSuite) TestNeverWritesInsideBeads() {
	stateHome := s.T().TempDir()
	s.T().Setenv("XDG_STATE_HOME", stateHome)

	workspace := s.T().TempDir()
	beadsDir := filepath.Join(workspace, ".beads")
	s.Require().NoError(os.MkdirAll(beadsDir, 0o755))

	s.Require().NoError(treeview.State{Selected: "x"}.Save(beadsDir))

	entries, err := os.ReadDir(beadsDir)
	s.Require().NoError(err)
	s.Empty(entries, "state must live under XDG_STATE_HOME, never in .beads")
}
