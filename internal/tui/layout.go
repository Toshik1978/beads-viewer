package tui

import "strings"

// Layout is the computed geometry for one frame.
type Layout struct {
	// Width and Height are the terminal's own clamped dimensions, not a
	// pane's — the help overlay and status bar size and centre against the
	// whole frame, not a single pane.
	Width       int
	Height      int
	ListWidth   int
	DetailWidth int
	BodyHeight  int
	Stacked     bool
}

const (
	// splitThreshold is the narrowest terminal that still gets side-by-side
	// panes. Below it the detail pane stacks under the list instead.
	splitThreshold = 100
	// listShare is the fraction of width the list takes when split.
	listShare = 0.42
	// chromeHeight is the status bar's own line. Previously 2, reserving room
	// for a header View() (app.go) never drew, leaving bv one row short.
	chromeHeight = 1
	// gutterWidth is the blank column joinPanes (app.go) renders between the
	// list and detail panes when side by side, so a truncated title's
	// ellipsis never sits flush against the detail pane. It comes out of
	// ListWidth's own share below, which is what keeps ListWidth +
	// gutterWidth + DetailWidth from ever exceeding width.
	gutterWidth = 1
)

// Compute derives pane geometry from the terminal size.
//
// Every returned dimension is clamped at zero. A terminal legitimately reports
// 0x0 during startup and mid-resize, and each of these values ends up as a
// slice bound in a view — a negative one is a panic several layers away from
// its cause.
func Compute(width, height int, showDetail bool) Layout {
	width = max(width, 0)
	height = max(height, 0)

	layout := Layout{Width: width, Height: height, BodyHeight: max(height-chromeHeight, 0)}

	switch {
	case !showDetail:
		layout.ListWidth = width
	case width < splitThreshold:
		// Stacked panes both run the full terminal width, one above the
		// other, so there is no shared row for a gutter to protect.
		layout.Stacked = true
		layout.ListWidth = width
		layout.DetailWidth = width
	default:
		layout.ListWidth = max(int(float64(width)*listShare)-gutterWidth, 0)
		layout.DetailWidth = max(width-layout.ListWidth-gutterWidth, 0)
	}

	return layout
}

// gutter renders the blank column joinPanes places between the list and
// detail panes when they sit side by side.
func gutter(height int) string {
	if height <= 0 {
		return ""
	}

	column := strings.Repeat(" ", gutterWidth)

	return strings.Repeat(column+"\n", height-1) + column
}

// paneHeights splits l's BodyHeight between the list and detail panes: equal
// shares when stacked, or the full height for both when side by side.
// Model.applyLayout and applyBackground (app.go, rebuilding the detail pane
// after a scheme change) both need this.
func (l Layout) paneHeights() (list, detail int) {
	list, detail = l.BodyHeight, l.BodyHeight
	if l.Stacked {
		list = l.BodyHeight / 2
		detail = l.BodyHeight - list
	}

	return list, detail
}

// applyLayout recomputes pane geometry for width x height and propagates it
// to every view, not only the active one, and the detail pane. Called on
// every WindowSizeMsg, and from handleGlobalKey/handleFilterKey (keys.go)
// whenever the filter overlay opens or closes without a resize — View
// (app.go) then appends a row chromeHeight above never reserves (C3).
func (m *Model) applyLayout(width, height int) {
	m.layout = Compute(width, height, true)
	if m.overlay.kind == overlayFilter {
		m.layout.BodyHeight = max(m.layout.BodyHeight-1, 0)
	}

	listHeight, detailHeight := m.layout.paneHeights()
	for _, v := range m.views {
		v.SetSize(m.layout.ListWidth, listHeight)
	}
	if m.detail != nil {
		m.detail.SetSize(m.layout.DetailWidth, detailHeight)
	}
}
