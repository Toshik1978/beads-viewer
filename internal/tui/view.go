package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// View is one of the interchangeable panes.
//
// Keeping the views behind this interface is the structural reason the root
// model stays small: each view owns its own state instead of contributing
// fields to it. Adding the dependency view, the fourth, bore this out: it
// cost one package and one enum arm, not a change to Model's own fields.
//
// SetTheme is what lets a detected background reach the panes: each New
// captures a theme.Theme BY VALUE, so without this applyBackground's rebuild
// (app.go) never reached a view already on screen.
//
// Reveal is what makes a view switch land on the issue the user was already
// looking at. It is deliberately not named SelectByID, which listview and
// boardview both already export with live callers: revealing may have to
// *change what is visible* before it can select — the tree expands the
// ancestors hiding the row, and the dependency view re-roots outright —
// whereas SelectByID moves a cursor among rows that already exist.
type View interface {
	SetSize(width, height int)
	SetSnapshot(snap *beads.Snapshot)
	SetTheme(th theme.Theme)
	Update(msg tea.Msg) tea.Cmd
	View() string
	Selected() *beads.Issue
	Reveal(id string) bool
}

// viewKinds pairs each Model.views slot with the config.ViewKind it
// represents. activeIndex and viewKindAt read it in opposite directions
// instead of each maintaining its own switch, which is what kept the two from
// drifting out of sync when board's slot last moved.
//
// It lives beside the View interface rather than on the root model: it is
// view-slot bookkeeping, and app.go is the file this project's 500-line cap
// actually binds on.
func viewKinds() [viewCount]config.ViewKind {
	return [viewCount]config.ViewKind{config.ViewList, config.ViewTree, config.ViewBoard, config.ViewDeps}
}

// activeIndex maps the configured starting view to its slot in Model.views.
// An unrecognised kind — config.Load validates it, but activeIndex must still
// be total — falls back to the list, the same as ViewList itself.
func activeIndex(kind config.ViewKind) int {
	for i, k := range viewKinds() {
		if k == kind {
			return i
		}
	}

	return 0
}

// viewKindAt is activeIndex's inverse, for the status bar.
func viewKindAt(i int) config.ViewKind {
	if kinds := viewKinds(); i >= 0 && i < len(kinds) {
		return kinds[i]
	}

	return config.ViewList
}
