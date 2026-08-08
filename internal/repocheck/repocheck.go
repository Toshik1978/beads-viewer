// Package repocheck exposes the repository facts its own test suites need to
// enforce this project's repository-wide invariants: where the repository
// root is, and which files git tracks.
//
// The suites are what do the enforcing, and there are four — the licensing
// sweep, the CI workflow rules, the coverage floor and the size caps. Only
// the first is about licensing, which is what this package was called until
// bv-67u. The name was not merely stale: a line-count check is not something
// anyone thinks to look for in a package called `licensing`, and
// internal/tui/app.go sat over the per-file cap for six releases partly
// because of it.
//
// Nothing here is imported by a package that ships. It exists so the suites
// can ask git what the repository contains, which is why this is the one
// package cmd/bv's TestNoSubprocessInTheBinary exempts from the os/exec ban —
// that exemption matches on this import path, so it moved with the rename.
package repocheck

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// RepoRoot returns the absolute path of the repository root.
func RepoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("locate repository root: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// TrackedFiles returns every git-tracked path, repository-relative.
//
// Enumerating tracked files rather than walking the filesystem is deliberate:
// a walk would also sweep untracked scratch directories that quote the old
// names legitimately, and it would descend into build output.
func TrackedFiles(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}

	var files []string
	for name := range strings.SplitSeq(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}

	return files, nil
}
