package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// runLimit bounds how long the spawned bv may take to exit.
//
// The bound is a backstop, not the mechanism: with Setsid below, bv exits in
// milliseconds. It exists so that a future regression fails in seconds with
// the message on line ~104 rather than blocking until `go test`'s 10-minute
// timeout, which prints a panic dump for the whole binary instead of naming
// the test that hung.
const runLimit = 30 * time.Second

// testVersion is injected into the binary under test through the same
// -X main.version path .goreleaser.yaml uses at release time, so what
// --version prints is checkable against a value this test chose rather than
// against the "dev" default the source would print anyway.
const testVersion = "0.0.0-main-test"

// detachFromTerminal runs the child in a new session, so it has no
// controlling terminal.
//
// This used to be the only thing standing between this test and a hang on
// every developer machine, while passing in CI: with stdin redirected but a
// controlling terminal present, bubbletea fell back to opening /dev/tty and
// blocked there forever. CI runners have no controlling terminal, so that
// open failed there and bv exited immediately — green on every runner, hung
// on every laptop.
//
// requireTerminal (program.go) now refuses that start outright, so the hang
// is gone at the source and this is no longer load-bearing. It stays because
// what it removes is the terminal itself: if requireTerminal ever regresses,
// this test still fails in CI rather than hanging a laptop, which is the
// asymmetry that let the original problem survive review. Unix-only, which
// matches what this project builds and tests.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func TestMain(t *testing.T) {
	suite.Run(t, new(mainTestSuite))
}

type mainTestSuite struct {
	suite.Suite

	binary string
}

func (s *mainTestSuite) SetupSuite() {
	s.binary = filepath.Join(s.T().TempDir(), "bv")
	out, err := exec.CommandContext(s.T().Context(), "go", "build",
		"-ldflags", "-X main.version="+testVersion, "-o", s.binary, ".").CombinedOutput()
	s.Require().NoError(err, string(out))
}

// TestVersionNamesTheVersion asserts what the flag is for, not merely that it
// printed. The previous assertion — non-empty output, exit zero — passes on a
// usage dump, an error, or anything else the command might emit, so it could
// not fail for the reason it was written for. Naming testVersion checks the
// whole chain instead: the -X path .goreleaser.yaml sets, cobra's Version
// wiring, and the string reaching stdout.
func (s *mainTestSuite) TestVersionNamesTheVersion() {
	out, err := exec.CommandContext(s.T().Context(), s.binary, "--version").CombinedOutput()
	s.Require().NoError(err)
	s.Contains(string(out), testVersion)
}

func (s *mainTestSuite) TestMissingWorkspaceExitsOneWithGuidance() {
	cmd := exec.CommandContext(s.T().Context(), s.binary)
	cmd.Dir = s.T().TempDir()
	cmd.Env = append(os.Environ(), "BEADS_DIR=")
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	s.Require().ErrorAs(err, &exitErr)
	s.Equal(1, exitErr.ExitCode())
	s.Contains(string(out), "br init", "the error must say how to fix it")
	s.NotContains(string(out), "panic")
}

func (s *mainTestSuite) TestInvalidFlagValueExitsOne() {
	cmd := exec.CommandContext(s.T().Context(), s.binary, "--theme", "purple")
	cmd.Dir = s.T().TempDir()
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	s.Require().ErrorAs(err, &exitErr)
	s.Equal(1, exitErr.ExitCode())
	s.Contains(string(out), "purple")
}

// TestNonTerminalStdinReportsRatherThanHangs is the regression guard for the
// failure detachFromTerminal used to paper over. It reproduces the user's
// case exactly — a redirected stdin with the session's controlling terminal
// left in place, which is what `echo | bv` and `bv < /dev/null` produce — so
// it deliberately does not call detachFromTerminal.
//
// The context deadline is what turns a regression into a failure instead of
// a hang: without it, this test would block until `go test`'s own timeout and
// print a whole-binary panic dump rather than naming itself.
func (s *mainTestSuite) TestNonTerminalStdinReportsRatherThanHangs() {
	dir := s.T().TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	s.Require().NoError(os.MkdirAll(beadsDir, 0o755))

	ctx, cancel := context.WithTimeout(s.T().Context(), runLimit)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.binary, "--db", beadsDir)
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+s.T().TempDir())
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()

	s.Require().NotErrorIs(ctx.Err(), context.DeadlineExceeded,
		"bv hung on a redirected stdin instead of reporting it")

	var exitErr *exec.ExitError
	s.Require().ErrorAs(err, &exitErr)
	s.Equal(1, exitErr.ExitCode())
	s.Contains(string(out), "stdin is not a terminal",
		"the user must be told what is wrong, not left looking at a dead process")
}

// TestNeverWritesInsideBeads is a spec acceptance criterion. bv is read-only;
// the tracker's data is br's alone.
func (s *mainTestSuite) TestNeverWritesInsideBeads() {
	dir := s.T().TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	s.Require().NoError(os.MkdirAll(beadsDir, 0o755))
	issues := filepath.Join(beadsDir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(issues,
		[]byte(`{"id":"bv-1","title":"One","status":"open"}`+"\n"), 0o600))

	before := s.snapshotDir(beadsDir)

	ctx, cancel := context.WithTimeout(s.T().Context(), runLimit)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.binary, "--db", beadsDir)
	// XDG_STATE_HOME is redirected so the tree view's persisted-state feature
	// writes into this test's own temp dir rather than the real machine's
	// ~/.local/state — that write is legitimate (it is not inside .beads,
	// which is the only thing this test asserts on) but would otherwise
	// leave files behind on whatever machine runs this suite.
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+s.T().TempDir())
	// Both are needed: the reader alone leaves bubbletea free to fall back to
	// /dev/tty. See detachFromTerminal.
	cmd.Stdin = strings.NewReader("")
	detachFromTerminal(cmd)
	_, _ = cmd.CombinedOutput()

	// Checked before the invariant below, because a bv killed at the deadline
	// says nothing either way about what it would have written, and reporting
	// "did not write to .beads" about a process that was shot is worse than
	// reporting nothing.
	s.Require().NotErrorIs(ctx.Err(), context.DeadlineExceeded,
		"bv did not exit within %s; it is asleep in the event loop, not slow", runLimit)

	s.Equal(before, s.snapshotDir(beadsDir),
		"bv must not create, modify or remove anything inside .beads")
}

// snapshotDir records name, size and mtime for every entry.
func (s *mainTestSuite) snapshotDir(dir string) map[string][3]any {
	entries, err := os.ReadDir(dir)
	s.Require().NoError(err)

	out := map[string][3]any{}
	for _, e := range entries {
		info, err := e.Info()
		s.Require().NoError(err)
		out[e.Name()] = [3]any{info.Name(), info.Size(), info.ModTime()}
	}

	return out
}

// TestNoSubprocessInTheBinary is the other read-only guarantee. It is a source
// check rather than a runtime one, because proving the negative at runtime
// needs process tracing.
func (s *mainTestSuite) TestNoSubprocessInTheBinary() {
	out, err := exec.CommandContext(s.T().Context(), "go", "list", "-deps", "-f",
		"{{.ImportPath}} {{join .Imports \" \"}}", "./...").Output()
	s.Require().NoError(err)

	for line := range strings.SplitSeq(string(out), "\n") {
		if !strings.HasPrefix(line, "github.com/Toshik1978/beads-viewer/") {
			continue
		}
		// internal/repocheck shells out to git, but it is test-support code
		// that never reaches a bv run.
		if strings.Contains(line, "/internal/repocheck") {
			continue
		}
		s.NotContains(line, "os/exec",
			"the shipped binary must never spawn a subprocess: "+line)
	}
}
