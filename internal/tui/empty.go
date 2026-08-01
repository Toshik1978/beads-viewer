package tui

// This file renders the single "nothing to show" body Model.View reaches for
// in place of the active pane whenever there is nothing in it to render.
// Before this task, listview, treeview and boardview each carried their own
// "No issues" branch, and every one of them said the same thing regardless
// of why the pane was empty — an over-narrow filter read exactly the same as
// a genuinely empty workspace, which sends the reader looking for a bug in
// br instead of at their own filter. This is the one place that decision is
// made now; see Model.body in app.go for the caller.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// emptyReason names why a pane has nothing to show.
type emptyReason int

const (
	// emptyWorkspace is an unfiltered snapshot with no issues at all.
	emptyWorkspace emptyReason = iota
	// emptyFilter is a snapshot that has issues, none of which survive the
	// active filter.
	emptyFilter
	// emptyLoadError is a reload that failed before any snapshot was ever
	// loaded successfully — the only way in, since an initial-load failure
	// exits before the TUI ever starts (cmd/bv's composition root). errText
	// (renderEmpty's own parameter, threaded from Model.status.Message) is
	// what this branch actually shows: the status line Model.View renders
	// below the body is one line shared with the workspace's absolute path,
	// which routinely leaves the decode reason itself — the only part of the
	// message actually worth reading — truncated off past a 200-column
	// terminal. This body owns the full BodyHeight, so it is wrapped here
	// instead of clipped to one line.
	emptyLoadError
)

// createHint is emptyWorkspace's fixed text.
const createHint = `No issues in this workspace. Create one with: br create "…"`

// nothingLoadedHint is emptyLoadError's fixed text, appended after errText
// when there is one. It replaces the previous wording ("Showing the last
// good snapshot."), which was false in every case this branch can actually
// render: body() (app.go) only reaches emptyLoadError when the snapshot is
// empty — precisely when there is no last-good snapshot to show.
const nothingLoadedHint = "Nothing has loaded yet."

// renderEmpty centres reason's message in width x height, clamped so it
// never overflows either dimension — the same discipline every pane's own
// View already owes joinPanes, its caller. errText is emptyLoadError's
// decode error (see that constant's doc comment); the other two reasons
// ignore it. Wrapping rather than truncating to one line is what lets a long
// errText actually be read, using the height renderEmpty owns rather than
// the status line's single, already-crowded row.
func renderEmpty(reason emptyReason, f beads.Filter, errText string, th theme.Theme, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	wrapped := lipgloss.Wrap(emptyMessage(reason, f, errText), width, "")
	if lines := strings.Split(wrapped, "\n"); len(lines) > height {
		wrapped = strings.Join(lines[:height], "\n")
	}

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, th.Muted.Render(wrapped))
}

// emptyMessage picks reason's wording. An over-narrow filter, an empty
// workspace and a failed reload are different problems for the user to act
// on, so each names its own fix rather than sharing one "No issues" that
// fits none of them.
//
// Untrusted fragments (errText, f.Describe()) are sanitized individually
// here rather than the composed message being sanitized wholesale by the
// caller, as an earlier version of this method did: uitext.Sanitize strips
// every rune below 0x20, which includes '\n' — sanitizing the whole string
// after emptyLoadError's own "\n\n" separator was spliced in silently ate
// both newlines, gluing errText directly onto nothingLoadedHint with no
// space between them at all ("...beginning of valueNothing has loaded
// yet."). Confirmed live: RenderEmptyForTest with a realistic decode error
// reproduced exactly that run-together text before this change.
func emptyMessage(reason emptyReason, f beads.Filter, errText string) string {
	switch reason {
	case emptyWorkspace:
		return createHint
	case emptyFilter:
		desc := uitext.Sanitize(f.Describe())
		if desc == "" {
			desc = "the current filter"
		}

		return fmt.Sprintf("No issues match %s. Press esc to clear the filter.", desc)
	case emptyLoadError:
		if errText == "" {
			return nothingLoadedHint
		}

		return fmt.Sprintf("Could not reload: %s\n\n%s", uitext.Sanitize(errText), nothingLoadedHint)
	}

	// Every named emptyReason is handled above (the exhaustive linter checks
	// it); this only guards a value outside that closed set, mirroring
	// activeIndex's own fallback in app.go.
	return createHint
}
