package tui_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/tui"
)

func TestTUI(t *testing.T) {
	suite.Run(t, new(layoutTestSuite))
	suite.Run(t, new(appTestSuite))
	suite.Run(t, new(tui.WhiteBoxSuite))
	suite.Run(t, new(statusTestSuite))
	suite.Run(t, new(helpTestSuite))
	suite.Run(t, new(emptyTestSuite))
}

type layoutTestSuite struct {
	suite.Suite
}

func (s *layoutTestSuite) TestSplitsWideTerminals() {
	got := tui.Compute(120, 40, true)

	s.False(got.Stacked)
	s.Positive(got.ListWidth)
	s.Positive(got.DetailWidth)
	s.LessOrEqual(got.ListWidth+got.DetailWidth, 120,
		"panes plus borders must never exceed the terminal width")
}

func (s *layoutTestSuite) TestStacksNarrowTerminals() {
	got := tui.Compute(60, 40, true)

	s.True(got.Stacked)
	// 60 at 40 rows affords a frame, and a stacked pane is bordered on its
	// own width too even though it already runs full width — two columns
	// go to its own frame just as they would side by side.
	s.True(got.Bordered)
	s.Equal(58, got.ListWidth)
}

func (s *layoutTestSuite) TestHidingDetailGivesTheListEverything() {
	got := tui.Compute(120, 40, false)

	// The list still spends its own two columns on a frame when bordered;
	// "everything" means the whole terminal minus that frame, not minus a
	// detail pane that never existed.
	s.True(got.Bordered)
	s.Equal(118, got.ListWidth)
	s.Zero(got.DetailWidth)
}

func (s *layoutTestSuite) TestDegenerateSizesStayNonNegative() {
	// A terminal can report 0x0 during startup and while being resized. Every
	// dimension feeds a slice bound somewhere downstream, so none may go
	// negative — that is the negative-viewport-offset panic, one layer up.
	cases := [][2]int{{0, 0}, {1, 1}, {3, 2}, {-5, -5}, {20, 1}}
	for _, tc := range cases {
		s.Run("", func() {
			got := tui.Compute(tc[0], tc[1], true)
			s.GreaterOrEqual(got.ListWidth, 0)
			s.GreaterOrEqual(got.DetailWidth, 0)
			s.GreaterOrEqual(got.BodyHeight, 0)
		})
	}
}

func (s *layoutTestSuite) TestBorderedPanesDeductTheirOwnFrame() {
	got := tui.Compute(120, 40, true)

	s.True(got.Bordered)
	// Each pane spends two columns on its own frame, and a bordered layout
	// has no gutter — adjacent frames separate the panes themselves.
	s.Equal(120, got.ListWidth+got.DetailWidth+2*2,
		"content widths plus both frames must account for every column")
}

func (s *layoutTestSuite) TestBorderedPaneHeightsDeductTheirOwnFrame() {
	got := tui.Compute(120, 40, true)
	list, detail := tui.ExportPaneHeights(got)

	s.True(got.Bordered)
	s.Equal(got.BodyHeight-2, list)
	s.Equal(got.BodyHeight-2, detail)
}

func (s *layoutTestSuite) TestStackedBorderedPanesSplitTheBodyThenDeduct() {
	got := tui.Compute(60, 40, true)
	list, detail := tui.ExportPaneHeights(got)

	s.True(got.Stacked)
	s.True(got.Bordered)
	s.Equal(got.BodyHeight/2-2, list)
	s.Equal(got.BodyHeight-got.BodyHeight/2-2, detail)
}

func (s *layoutTestSuite) TestTerminalsTooSmallToFrameGoUnbordered() {
	cases := []struct {
		name          string
		width, height int
	}{
		{"too narrow", 9, 40},
		{"too short", 120, 5},
		{"degenerate", 0, 0},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			got := tui.Compute(tc.width, tc.height, true)

			s.False(got.Bordered,
				"a terminal this size must not spend its cells on a frame")
			s.GreaterOrEqual(got.ListWidth, 0)
			s.GreaterOrEqual(got.DetailWidth, 0)
		})
	}
}

func (s *layoutTestSuite) TestUnborderedLayoutsKeepTheGutter() {
	// Wide enough to split side by side (>= splitThreshold) but short enough
	// that BodyHeight falls under minBorderedBodyHeight, so this is the
	// unbordered fallback for a side-by-side layout, not the stacked one — a
	// narrow width alone (e.g. 9) forces Stacked before Bordered ever enters
	// the decision, which would not exercise the gutter this test is for.
	got := tui.Compute(100, 5, true)

	s.False(got.Stacked)
	s.False(got.Bordered)
	// The blank gutter column is the unbordered fallback's only separation
	// between the panes, so it must survive when the frame does not.
	//
	// The assertion is on the gutter's exact width, not on the panes fitting.
	// A LessOrEqual(..., 100) was satisfied by 42 + 58 == 100 — no gutter at
	// all — so it pinned !Bordered a second time and left separatorWidth free
	// to return 0.
	s.Equal(100-1, got.ListWidth+got.DetailWidth,
		"exactly one column stays blank between the panes")
}

func (s *layoutTestSuite) TestPaneHeightsNeverGoNegative() {
	// BodyHeight can legitimately be smaller than a frame mid-resize.
	for _, h := range []int{0, 1, 2, 3, 7} {
		s.Run("", func() {
			list, detail := tui.ExportPaneHeights(tui.Compute(120, h, true))
			s.GreaterOrEqual(list, 0)
			s.GreaterOrEqual(detail, 0)
		})
	}
}
