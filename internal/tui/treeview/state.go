package treeview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State is a tree view's persisted UI state: which nodes were open and which
// one was selected. It is bv's own preference, not tracker data — br owns
// .beads, and this never lives there.
//
// It used to carry hide_closed too, back when that was a tree-local
// narrowing. Now that every view shares that filter, it is dropped rather
// than migrated: restoring it would hide closed issues in two panes the user
// never set it on. Older files still load — json ignores an unknown field.
type State struct {
	Expanded []string `json:"expanded"`
	Selected string   `json:"selected"`
}

// LoadState reads a workspace's persisted tree state.
//
// A missing or unparsable file returns a zero State and no error on purpose:
// forgetting expansion state must never keep bv from starting, so every
// failure on this path — a missing file, a permission error, a truncated
// write from a crash mid-Save — folds into the same "nothing to restore"
// outcome rather than reaching the caller.
//
// found reports whether a state file was actually read and parsed, which
// (State, error) alone cannot say: a zero State is what both "nothing was
// ever saved" and "every node was saved collapsed" look like once loaded. A
// caller that applies a zero State either way — LoadTreeState, before this
// existed — overwrites Build's depth==0 default with "collapse everything".
func LoadState(beadsDir string) (state State, found bool, err error) {
	data, err := os.ReadFile(StatePath(beadsDir))
	if err != nil {
		return State{}, false, nil //nolint:nilerr // a forgotten state is not a startup failure
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, false, nil //nolint:nilerr // a corrupt state file is forgotten, not fatal
	}

	return s, true, nil
}

// StatePath returns where a workspace's tree state is stored.
//
// State lives under XDG_STATE_HOME, keyed by a hash of the workspace's
// .beads directory. It deliberately does not live inside that directory:
// .beads belongs to br, and bv's read-only guarantee over it is asserted by
// a test elsewhere in this project.
//
// The path is absolutised before hashing. beadsDir is documented as not
// normalised by its caller (beads.Workspace.Dir), so the same workspace
// addressed as "--db .beads" from two different working directories, or as
// "--db .beads" versus its absolute equivalent, hashed to two different
// files under the old filepath.Clean-only version of this function —
// confirmed by running the real binary from two distinct workspaces with a
// relative --db and getting one shared state file for both. Falling back to
// Clean when Abs fails (an unreadable working directory) keeps this total
// rather than propagating an error two non-fatal callers would have to
// thread through anyway.
func StatePath(beadsDir string) string {
	abs, err := filepath.Abs(beadsDir)
	if err != nil {
		abs = filepath.Clean(beadsDir)
	}

	sum := sha256.Sum256([]byte(abs))

	return filepath.Join(stateHome(), "bv", "trees", hex.EncodeToString(sum[:8])+".json")
}

// Save writes s for beadsDir, atomically: a temp file in the same directory
// plus a rename, so a crash mid-write leaves the previous state on disk
// rather than a truncated one that LoadState would then have to discard.
func (s State) Save(beadsDir string) error {
	path := StatePath(beadsDir)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create tree state directory: %w", err)
	}

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode tree state: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temporary tree state file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("write temporary tree state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary tree state file: %w", err)
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("rename temporary tree state file: %w", err)
	}

	return nil
}

// stateHome resolves XDG_STATE_HOME, falling back to ~/.local/state the same
// way the XDG base directory spec does when the variable is unset.
func stateHome() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// No home directory to fall back to. A relative path here — the
		// previous fallback used ".local/state" — would have Save create
		// ./.local/state/... under whatever directory bv happened to be
		// launched from, silently writing outside the state area entirely.
		// os.TempDir() is still absolute and still non-fatal for every
		// caller of StatePath (a read or write failure there is forgotten,
		// not surfaced), without risking a write relative to cwd.
		return os.TempDir()
	}

	return filepath.Join(home, ".local", "state")
}
