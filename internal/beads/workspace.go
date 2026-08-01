package beads

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoWorkspace reports that no .beads directory could be located.
var ErrNoWorkspace = errors.New("no .beads directory found")

// beadsDirName is the directory br creates in a project root.
const beadsDirName = ".beads"

// issuesFileName is the JSONL export br keeps current by auto-flushing after
// every mutating command. It is the only file bv reads.
const issuesFileName = "issues.jsonl"

// Workspace locates the .beads directory bv reads and the file inside it.
type Workspace struct {
	// Dir is the .beads directory itself.
	Dir string
	// IssuesPath is Dir/issues.jsonl. It need not exist: br init creates the
	// directory before writing anything into it.
	IssuesPath string
}

// FindWorkspace locates the workspace to read, preferring an explicit path,
// then BEADS_DIR, then the nearest .beads directory at or above the working
// directory.
//
// Symlinks are not explicitly resolved: os.Stat follows a symlink for the
// final path component (standard library behavior), but Workspace.Dir and
// IssuesPath are built from the input path as given, not normalized through
// filepath.EvalSymlinks. A workspace reached through a symlinked ancestor
// directory therefore reports that symlinked path, not its real location.
func FindWorkspace(explicit string) (Workspace, error) {
	if explicit != "" {
		ws, err := workspaceAt(explicit)
		if err != nil {
			return Workspace{}, fmt.Errorf("resolve --db %q: %w", explicit, err)
		}

		return ws, nil
	}

	if env := os.Getenv("BEADS_DIR"); env != "" {
		ws, err := workspaceAt(env)
		if err != nil {
			return Workspace{}, fmt.Errorf("resolve BEADS_DIR %q: %w", env, err)
		}

		return ws, nil
	}

	return searchUpwards()
}

// workspaceAt accepts the .beads directory, a project root containing one, or
// a path to a file inside one. All three are spellings a user reasonably
// types, and rejecting two of them buys nothing.
func workspaceAt(path string) (Workspace, error) {
	// gosec flags this as tainted-path traversal, but resolving a
	// user-supplied --db/BEADS_DIR path is exactly this function's job, not a
	// vulnerability: bv only reads the file, and the user names their own path.
	info, err := os.Stat(path) //nolint:gosec // intentional: user-supplied workspace path
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: %s", ErrNoWorkspace, path)
	}

	dir := path
	if !info.IsDir() {
		dir = filepath.Dir(path)
	}

	if filepath.Base(dir) != beadsDirName {
		if nested := filepath.Join(dir, beadsDirName); isDir(nested) {
			dir = nested
		}
	}

	if filepath.Base(dir) != beadsDirName || !isDir(dir) {
		return Workspace{}, fmt.Errorf("%w: %s", ErrNoWorkspace, path)
	}

	return Workspace{Dir: dir, IssuesPath: filepath.Join(dir, issuesFileName)}, nil
}

// searchUpwards walks from the working directory to the filesystem root.
func searchUpwards() (Workspace, error) {
	dir, err := os.Getwd()
	if err != nil {
		return Workspace{}, fmt.Errorf("determine working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, beadsDirName)
		if isDir(candidate) {
			return Workspace{
				Dir:        candidate,
				IssuesPath: filepath.Join(candidate, issuesFileName),
			}, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return Workspace{}, fmt.Errorf(
				"%w at or above the working directory — run 'br init' first", ErrNoWorkspace)
		}

		dir = parent
	}
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	// Same gosec taint finding as workspaceAt, and the same answer: path is
	// composed from a user-supplied workspace root, never from raw request
	// input, so there is nothing to traverse into that the user didn't name.
	info, err := os.Stat(path) //nolint:gosec // intentional: user-supplied workspace path

	return err == nil && info.IsDir()
}
