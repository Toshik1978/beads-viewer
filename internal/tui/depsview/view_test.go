package depsview_test

import (
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/depsview"
)

// Pin: *depsview.Model must keep satisfying tui.View. Same drift risk as
// listview's, treeview's and boardview's own pins — silently losing a method
// here would not surface until app.go's construction site failed to compile,
// which points at the wrong package's diff.
var _ tui.View = (*depsview.Model)(nil)
