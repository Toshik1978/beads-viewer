package tui

// frame draws pane's border, or returns pane untouched when Compute decided
// the terminal could not afford one. width and height are the pane's content
// box, already net of the frame (deductBorders and paneHeights) — but
// lipgloss/v2's Style.Width/Height size the block the border is drawn
// around, not the content area inside it, so borderWidth/borderHeight are
// added back here. Passing the content size straight through would wrap
// content that already fit and push the block a row taller than budgeted.
func (m *Model) frame(pane string, width, height int, focused bool) string {
	if !m.layout.Bordered {
		return pane
	}

	return m.theme.Frame(focused).Width(width + borderWidth).Height(height + borderHeight).Render(pane)
}

// separator is the blank gutter column between side-by-side panes. A framed
// layout has none — the adjacent borders already separate the panes, and a
// gutter on top would leave a visible three-cell channel. Layout.separator-
// Width (layout.go) makes the same decision for the geometry; this is its
// rendering half.
func (m *Model) separator() string {
	if m.layout.Bordered {
		return ""
	}

	return gutter(m.layout.BodyHeight)
}
