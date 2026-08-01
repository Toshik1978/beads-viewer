package treeview_test

import (
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
)

// Pin: *treeview.Model must keep satisfying tui.View. This lives in the
// external test package (treeview_test), which sits outside every package's
// import graph, so treeview_test -> tui -> treeview is not a cycle even
// though tui does not import treeview until Task 5.3 wires it in.
var _ tui.View = (*treeview.Model)(nil)
