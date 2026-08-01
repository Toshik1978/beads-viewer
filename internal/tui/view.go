package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// View is one of the interchangeable panes.
//
// Keeping the views behind this interface is the structural reason the root
// model stays small: the list, tree and board own their own state instead of
// contributing fields to it. Adding a fourth view means adding a package and
// one enum arm.
//
// SetTheme is what lets a detected background reach the panes: each New
// captures a theme.Theme BY VALUE, so without this applyBackground's rebuild
// (app.go) never reached a view already on screen.
type View interface {
	SetSize(width, height int)
	SetSnapshot(snap *beads.Snapshot)
	SetTheme(th theme.Theme)
	Update(msg tea.Msg) tea.Cmd
	View() string
	Selected() *beads.Issue
}
