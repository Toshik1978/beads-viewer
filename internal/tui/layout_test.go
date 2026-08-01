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
	s.Equal(60, got.ListWidth)
}

func (s *layoutTestSuite) TestHidingDetailGivesTheListEverything() {
	got := tui.Compute(120, 40, false)

	s.Equal(120, got.ListWidth)
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
