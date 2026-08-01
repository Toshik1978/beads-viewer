package listview_test

import (
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/listview"
)

// Pin: *listview.Model must keep satisfying tui.View. Same drift risk as
// treeview's own pin (internal/tui/treeview/view_test.go): silently losing a
// method here would not be caught until app.go's construction site failed to
// compile, which points at the wrong package's diff.
var _ tui.View = (*listview.Model)(nil)
