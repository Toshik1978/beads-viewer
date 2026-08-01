package config

import (
	"fmt"
	"log/slog"
	"os"
)

// NewLogger returns a file logger when path is non-empty, and one that
// discards otherwise. It lives here rather than in cmd/bv because LogPath is
// this package's own resolved value, and cmd/bv's composition root is capped
// at 150 lines — writing to stderr by default would corrupt the TUI's
// rendering, so discarding must be the fallback, never stderr.
func NewLogger(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log %s: %w", path, err)
	}

	return slog.New(slog.NewTextHandler(file, nil)), func() { _ = file.Close() }, nil
}
