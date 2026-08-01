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
	// Bordered reports whether the terminal can afford a frame around each
	// pane. Every other dimension here is already net of that frame, so a
	// renderer reads this only to decide whether to draw one — never to
	// re-derive a width.
	Bordered bool
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
	// ellipsis never sits flush against the detail pane. It applies only to
	// the unbordered fallback: a framed layout separates the panes with their
	// own adjacent borders instead (separatorWidth below). It comes out of
	// ListWidth's own share below, which is what keeps ListWidth +
	// gutterWidth + DetailWidth from ever exceeding width.
	gutterWidth = 1
	// borderWidth and borderHeight are the columns and rows a framed pane
	// spends on its own border — one on each side. They are deducted from the
	// pane's content geometry before it reaches View.SetSize; a view handed
	// its frame's outer size renders borderWidth cells wider than the frame
	// drawn around it, which lipgloss then clips silently.
	borderWidth  = 2
	borderHeight = 2
	// minBorderedPaneWidth and minBorderedBodyHeight are the smallest content
	// area worth framing. Below either, panes render unframed: at that size
	// the frame costs a larger share of the pane than the content it would
	// contain. minBorderedBodyHeight is 6 rather than 3 because a stacked
	// layout halves the body first, so each half needs its own two frame rows
	// plus a content row.
	minBorderedPaneWidth  = 8
	minBorderedBodyHeight = 6
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

	l := Layout{
		Width:      width,
		Height:     height,
		BodyHeight: max(height-chromeHeight, 0),
	}
	l.Bordered = fitsBorders(width, l.BodyHeight)

	switch {
	case !showDetail:
		l.ListWidth = width
	case width < splitThreshold:
		// Stacked panes both run the full terminal width, one above the
		// other, so there is no shared row for a gutter to protect.
		l.Stacked = true
		l.ListWidth, l.DetailWidth = width, width
	default:
		sep := l.separatorWidth()
		l.ListWidth = max(int(float64(width)*listShare)-sep, 0)
		l.DetailWidth = max(width-l.ListWidth-sep, 0)
	}

	return l.deductBorders()
}

// fitsBorders reports whether width x bodyHeight can afford a frame around
// each pane. Below either floor the panes render unframed rather than
// surrendering most of a small terminal to decoration.
func fitsBorders(width, bodyHeight int) bool {
	return width >= minBorderedPaneWidth+borderWidth &&
		bodyHeight >= minBorderedBodyHeight
}

// separatorWidth is the blank column between side-by-side panes. A framed
// layout has none: the two adjacent borders already separate the panes, and
// keeping the gutter as well would leave a visible three-cell channel.
func (l Layout) separatorWidth() int {
	if l.Bordered {
		return 0
	}

	return gutterWidth
}

// deductBorders takes each pane's frame out of its own content width.
//
// Heights are deducted in paneHeights instead, not here: BodyHeight is also
// the height the empty screen (empty.go) and the help overlay (help.go)
// render into, and neither of those is framed. Deducting rows here would
// shrink both of them by two for a border they never draw.
func (l Layout) deductBorders() Layout {
	if !l.Bordered {
		return l
	}

	l.ListWidth = max(l.ListWidth-borderWidth, 0)
	if l.DetailWidth > 0 {
		l.DetailWidth = max(l.DetailWidth-borderWidth, 0)
	}

	return l
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
// shares when stacked, or the full height for both when side by side. When
// Bordered, each pane's own frame is then deducted from its share.
// Model.applyLayout and applyBackground (app.go, rebuilding the detail pane
// after a scheme change) both need this.
func (l Layout) paneHeights() (list, detail int) {
	list, detail = l.BodyHeight, l.BodyHeight
	if l.Stacked {
		list = l.BodyHeight / 2
		detail = l.BodyHeight - list
	}
	if l.Bordered {
		list = max(list-borderHeight, 0)
		detail = max(detail-borderHeight, 0)
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
