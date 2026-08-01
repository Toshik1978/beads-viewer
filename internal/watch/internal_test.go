package watch

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/stretchr/testify/suite"
)

// WhiteBoxSuite holds tests that need access to unexported fields or
// constants — checks that verify a load-bearing decision directly rather
// than inferring it from OS-dependent event delivery.
type WhiteBoxSuite struct {
	suite.Suite
}

// TestWatchesDirectoryNotFile is the real guard for the atomic-rename fix.
// fsnotify's kqueue backend (macOS) silently re-watches a renamed single-file
// target, so the rename tests in watch_test.go pass even against a
// file-level watch on that platform — they exercise real behaviour but are
// known not to discriminate there. This test inspects fsnotify's own watch
// list instead, which fails identically on every backend.
func (s *WhiteBoxSuite) TestWatchesDirectoryNotFile() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))

	w, err := New(slog.New(slog.DiscardHandler), path, time.Millisecond)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = w.Close() })

	list := w.fsw.WatchList()
	s.Contains(list, dir)
	s.NotContains(list, path)
}

// TestForcesReloadWhenBurstExceedsCeiling proves the debounce timer cannot
// defer a reload forever: a stream of writes spaced under the debounce
// interval must still produce an event once maxDebounce elapses. Against the
// original design (which only ever resets the debounce timer), this test
// times out, because the stream never stops for the debounce window to pass.
func (s *WhiteBoxSuite) TestForcesReloadWhenBurstExceedsCeiling() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(path, []byte("{}\n"), 0o600))

	debounce := 100 * time.Millisecond

	w, err := New(slog.New(slog.DiscardHandler), path, debounce)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = w.Close() })

	stop := make(chan struct{})
	defer close(stop)

	go func() {
		ticker := time.NewTicker(debounce / 2)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = os.WriteFile(path, []byte("{}\n"), 0o600)
			}
		}
	}()

	select {
	case <-w.Events():
	case <-time.After(maxDebounce + time.Second):
		s.FailNow("expected the ceiling to force a reload but none arrived")
	}
}
