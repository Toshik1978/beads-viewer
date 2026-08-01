package boardview_test

import (
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/boardview"
)

// Pin: *boardview.Model must keep satisfying tui.View. Same drift risk as
// listview's and treeview's own pins: silently losing a method here would
// not be caught until app.go's construction site failed to compile, which
// points at the wrong package's diff.
var _ tui.View = (*boardview.Model)(nil)
