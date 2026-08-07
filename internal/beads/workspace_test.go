package beads_test

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

type workspaceTestSuite struct {
	suite.Suite
}

// makeWorkspace creates <root>/.beads/issues.jsonl and returns root.
func (s *workspaceTestSuite) makeWorkspace(root string, withIssues bool) string {
	dir := filepath.Join(root, ".beads")
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	if withIssues {
		s.Require().NoError(os.WriteFile(filepath.Join(dir, "issues.jsonl"), []byte("{}\n"), 0o600))
	}

	return root
}

func (s *workspaceTestSuite) TestExplicitPathToBeadsDir() {
	root := s.makeWorkspace(s.T().TempDir(), true)

	ws, err := beads.FindWorkspace(filepath.Join(root, ".beads"))
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, ".beads"), ws.Dir)
	s.Equal(filepath.Join(root, ".beads", "issues.jsonl"), ws.IssuesPath)
}

func (s *workspaceTestSuite) TestExplicitPathToProjectRoot() {
	root := s.makeWorkspace(s.T().TempDir(), true)

	ws, err := beads.FindWorkspace(root)
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, ".beads"), ws.Dir)
}

func (s *workspaceTestSuite) TestExplicitPathToIssuesFile() {
	root := s.makeWorkspace(s.T().TempDir(), true)

	ws, err := beads.FindWorkspace(filepath.Join(root, ".beads", "issues.jsonl"))
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, ".beads"), ws.Dir)
}

func (s *workspaceTestSuite) TestExplicitPathMissingIsAnError() {
	_, err := beads.FindWorkspace(filepath.Join(s.T().TempDir(), "nope"))
	s.Require().Error(err)
	s.ErrorIs(err, beads.ErrNoWorkspace)
}

// TestUnreadablePathIsNotReportedAsMissing separates the two failures os.Stat
// reports through one return: a path that is not there, and a path that is
// there but cannot be looked at. Collapsing the second into ErrNoWorkspace
// tells a user with a permissions problem to check their spelling.
func (s *workspaceTestSuite) TestUnreadablePathIsNotReportedAsMissing() {
	if os.Geteuid() == 0 {
		s.T().Skip("root bypasses directory permissions, so os.Stat cannot fail with EACCES")
	}

	// The permission has to be removed from the *parent*: stat on a directory
	// needs search permission on the directory holding it, not on the
	// directory itself.
	parent := filepath.Join(s.T().TempDir(), "locked")
	target := s.makeWorkspace(parent, true)
	s.Require().NoError(os.Chmod(parent, 0o000))
	s.T().Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	_, err := beads.FindWorkspace(filepath.Join(target, ".beads"))
	s.Require().Error(err)
	s.Require().NotErrorIs(err, beads.ErrNoWorkspace)
	s.Require().ErrorIs(err, fs.ErrPermission)
}

func (s *workspaceTestSuite) TestEnvironmentVariable() {
	root := s.makeWorkspace(s.T().TempDir(), true)
	s.T().Setenv("BEADS_DIR", filepath.Join(root, ".beads"))

	ws, err := beads.FindWorkspace("")
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, ".beads"), ws.Dir)
}

func (s *workspaceTestSuite) TestExplicitBeatsEnvironment() {
	wanted := s.makeWorkspace(s.T().TempDir(), true)
	other := s.makeWorkspace(s.T().TempDir(), true)
	s.T().Setenv("BEADS_DIR", filepath.Join(other, ".beads"))

	ws, err := beads.FindWorkspace(filepath.Join(wanted, ".beads"))
	s.Require().NoError(err)
	s.Equal(filepath.Join(wanted, ".beads"), ws.Dir)
}

func (s *workspaceTestSuite) TestWalksUpFromNestedDirectory() {
	root := s.makeWorkspace(s.T().TempDir(), true)
	nested := filepath.Join(root, "a", "b", "c")
	s.Require().NoError(os.MkdirAll(nested, 0o755))
	s.T().Setenv("BEADS_DIR", "")
	s.T().Chdir(nested)

	ws, err := beads.FindWorkspace("")
	s.Require().NoError(err)
	// EvalSymlinks because macOS resolves /var to /private/var, and TempDir
	// hands back the unresolved form.
	got, err := filepath.EvalSymlinks(ws.Dir)
	s.Require().NoError(err)
	want, err := filepath.EvalSymlinks(filepath.Join(root, ".beads"))
	s.Require().NoError(err)
	s.Equal(want, got)
}

func (s *workspaceTestSuite) TestNoWorkspaceAnywhere() {
	s.T().Setenv("BEADS_DIR", "")
	s.T().Chdir(s.T().TempDir())

	_, err := beads.FindWorkspace("")
	s.Require().ErrorIs(err, beads.ErrNoWorkspace)
	s.Contains(err.Error(), "br init")
}

// TestNoWorkspaceAnywhereNamesWhereItLooked pins the half of that message a
// reader cannot supply for themselves. "At or above the working directory"
// is unanswerable when the reader disagrees about which directory that is.
func (s *workspaceTestSuite) TestNoWorkspaceAnywhereNamesWhereItLooked() {
	s.T().Setenv("BEADS_DIR", "")
	s.T().Chdir(s.T().TempDir())

	// Against os.Getwd rather than the TempDir path: on macOS the two spell
	// the same directory differently (/var versus /private/var), and it is
	// the working directory as the process sees it that the message has to
	// name — that is the spelling the reader will compare against their own
	// shell.
	want, err := os.Getwd()
	s.Require().NoError(err)

	_, err = beads.FindWorkspace("")
	s.Require().ErrorIs(err, beads.ErrNoWorkspace)
	s.Contains(err.Error(), want)
}

func (s *workspaceTestSuite) TestDirectoryWithoutIssuesFileIsValid() {
	// br init creates .beads/ before anything is written into it. That must
	// open as an empty viewer, not fail.
	root := s.makeWorkspace(s.T().TempDir(), false)

	ws, err := beads.FindWorkspace(filepath.Join(root, ".beads"))
	s.Require().NoError(err)
	s.Equal(filepath.Join(root, ".beads"), ws.Dir)
}

func (s *workspaceTestSuite) TestExplicitPathDoesNotSearchUpward() {
	// A workspace exists at root, but the caller names a subdirectory of root
	// that has no .beads of its own. Silently climbing from there back up to
	// root's .beads would show the wrong project's issues, so this must error
	// rather than resolve.
	root := s.makeWorkspace(s.T().TempDir(), true)
	sub := filepath.Join(root, "sub")
	s.Require().NoError(os.MkdirAll(sub, 0o755))

	_, err := beads.FindWorkspace(sub)
	s.Require().ErrorIs(err, beads.ErrNoWorkspace)
}
