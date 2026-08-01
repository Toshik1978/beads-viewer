// Package licensing exposes the repository facts its own test suite needs to
// enforce the licensing invariants: where the repository root is, and which
// files git tracks.
package licensing

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
