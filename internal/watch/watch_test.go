package watch_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/watch"
)

func TestWatch(t *testing.T) {
	suite.Run(t, new(watchTestSuite))
	suite.Run(t, new(watch.WhiteBoxSuite))
}

type watchTestSuite struct {
	suite.Suite
}

func (s *watchTestSuite) newWatcher(path string, debounce time.Duration) *watch.Watcher {
	w, err := watch.New(slog.New(slog.DiscardHandler), path, debounce)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = w.Close() })

	return w
}

// awaitEvent waits for one event, failing the test on timeout.
func (s *watchTestSuite) awaitEvent(w *watch.Watcher, within time.Duration) {
	select {
	case <-w.Events():
	case <-time.After(within):
		s.FailNow("expected a change event but none arrived")
	}
}

// expectNoEvent asserts nothing arrives in the window.
func (s *watchTestSuite) expectNoEvent(w *watch.Watcher, within time.Duration) {
	select {
	case <-w.Events():
		s.FailNow("unexpected change event")
	case <-time.After(within):
	}
}

func (s *watchTestSuite) TestDetectsPlainWrite() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))

	w := s.newWatcher(path, 20*time.Millisecond)
	s.Require().NoError(os.WriteFile(path, []byte("{\"id\":\"a\"}\n"), 0o600))

	s.awaitEvent(w, 2*time.Second)
}

// TestDetectsAtomicRename is the reason this package watches the directory.
// br writes via temp-file-then-rename; a file-level watch is bound to the old
// inode and stops delivering events with no error at all.
//
// This test (and TestSurvivesRepeatedAtomicRenames below) exercises real
// behaviour but is known not to discriminate on macOS: fsnotify's kqueue
// backend silently re-watches a renamed single-file target, so both still
// pass against a broken file-level watch on that platform. The guard that
// actually catches the file-vs-directory mistake on every platform is
// WhiteBoxSuite.TestWatchesDirectoryNotFile in internal_test.go, which
// inspects fsnotify's own watch list instead of inferring it from event
// delivery.
func (s *watchTestSuite) TestDetectsAtomicRename() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))

	w := s.newWatcher(path, 20*time.Millisecond)

	tmp := filepath.Join(dir, ".issues.jsonl.tmp")
	s.Require().NoError(os.WriteFile(tmp, []byte("{\"id\":\"a\"}\n"), 0o600))
	s.Require().NoError(os.Rename(tmp, path))

	s.awaitEvent(w, 2*time.Second)
}

// TestSurvivesRepeatedAtomicRenames proves the watch is not consumed by the
// first replacement — the failure mode is that reload works exactly once.
func (s *watchTestSuite) TestSurvivesRepeatedAtomicRenames() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))

	w := s.newWatcher(path, 20*time.Millisecond)

	for round := range 3 {
		tmp := filepath.Join(dir, ".issues.jsonl.tmp")
		s.Require().NoError(os.WriteFile(tmp, []byte("{}\n"), 0o600))
		s.Require().NoError(os.Rename(tmp, path))
		s.awaitEvent(w, 2*time.Second)
		_ = round
	}
}

func (s *watchTestSuite) TestIgnoresOtherFilesInTheDirectory() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))

	w := s.newWatcher(path, 20*time.Millisecond)

	// br touches these constantly; reloading on each would be pure waste.
	for _, name := range []string{"beads.db", "beads.db-wal", ".write.lock", "last-touched"} {
		s.Require().NoError(os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
	}

	s.expectNoEvent(w, 300*time.Millisecond)
}

func (s *watchTestSuite) TestDebouncesABurst() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))

	w := s.newWatcher(path, 150*time.Millisecond)

	for i := range 10 {
		s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))
		_ = i
		time.Sleep(10 * time.Millisecond)
	}

	s.awaitEvent(w, 2*time.Second)
	// A burst collapses to one event; a second would mean the timer is not
	// resetting and every write reloads the file.
	s.expectNoEvent(w, 400*time.Millisecond)
}

func (s *watchTestSuite) TestWatchesADirectoryThatIsEmpty() {
	// br init creates .beads/ before writing issues.jsonl. Starting bv in that
	// window must work, and the file appearing later must be seen.
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")

	w := s.newWatcher(path, 20*time.Millisecond)
	s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))

	s.awaitEvent(w, 2*time.Second)
}

func (s *watchTestSuite) TestMissingDirectoryIsAnError() {
	_, err := watch.New(slog.New(slog.DiscardHandler),
		filepath.Join(s.T().TempDir(), "nope", "issues.jsonl"), time.Millisecond)
	s.Error(err)
}

// TestCloseReleasesAConsumer pins the guarantee cmd/bv's forward goroutine
// rests on: ranging over Events() ends when the watcher does. Against the
// original code — which closed w.done and the fsnotify watcher but never
// w.events — this test times out, and every restarted watcher would strand
// one goroutine per restart.
func (s *watchTestSuite) TestCloseReleasesAConsumer() {
	dir := s.T().TempDir()
	w := s.newWatcher(filepath.Join(dir, "issues.jsonl"), time.Millisecond)

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			if _, open := <-w.Events(); !open {
				return
			}
		}
	}()

	s.Require().NoError(w.Close())

	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		s.FailNow("a consumer ranging over Events() was left blocked after Close returned")
	}

	// Close waits rather than merely arranging: by the time it returns, the
	// channel is observably closed, so a caller may start a replacement
	// watcher immediately instead of guessing when the old one let go.
	_, open := <-w.Events()
	s.False(open, "Events() must be closed once Close has returned")
}

func (s *watchTestSuite) TestCloseIsIdempotent() {
	dir := s.T().TempDir()
	w := s.newWatcher(filepath.Join(dir, "issues.jsonl"), time.Millisecond)

	s.NoError(w.Close())
	s.NoError(w.Close(), "Close runs from a defer and may be called twice")
}
