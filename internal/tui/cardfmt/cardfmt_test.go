package cardfmt_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/cardfmt"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

func TestCardfmt(t *testing.T) {
	suite.Run(t, new(cardfmtTestSuite))
}

type cardfmtTestSuite struct {
	suite.Suite
}

func (s *cardfmtTestSuite) TestRenderNeverExceedsItsWidth() {
	issue := &beads.Issue{
		ID:       "bv-a-very-long-identifier-indeed",
		Title:    "a title far longer than any sensible column could hold without truncating it",
		Status:   beads.StatusOpen,
		Priority: beads.PriorityHigh,
	}
	snap := beads.NewSnapshot(nil)

	for _, width := range []int{0, 1, 2, 3, 18, 40, 120} {
		for _, expanded := range []bool{false, true} {
			s.Run("", func() {
				out := cardfmt.Render(s.th(), snap, issue, width, false, expanded)
				for _, line := range splitLines(ansi.Strip(out)) {
					s.LessOrEqual(len([]rune(line)), max(width, 0), "width %d, line %q", width, line)
				}
			})
		}
	}
}

func (s *cardfmtTestSuite) TestSelectedRendersDifferentlyFromUnselected() {
	// Styling asserted separately from content, per the house rule: the
	// ANSI-stripped text is the same, the escape-carrying output is not.
	issue := &beads.Issue{ID: "bv-1", Title: "t", Status: beads.StatusOpen}
	snap := beads.NewSnapshot(nil)

	plain := cardfmt.Render(s.th(), snap, issue, 30, false, false)
	picked := cardfmt.Render(s.th(), snap, issue, 30, true, false)

	s.NotEqual(plain, picked, "a selected card must be visibly distinguishable")
	s.Equal(ansi.Strip(plain), ansi.Strip(picked), "selection is styling, not content")
}

func (s *cardfmtTestSuite) TestHeightMatchesTheRenderedRowCount() {
	issue := &beads.Issue{ID: "bv-1", Title: "t", Status: beads.StatusOpen}
	snap := beads.NewSnapshot(nil)

	for _, expanded := range []bool{false, true} {
		s.Run("", func() {
			out := cardfmt.Render(s.th(), snap, issue, 30, false, expanded)
			s.Len(splitLines(ansi.Strip(out)), cardfmt.Height(expanded),
				"Height is what the board budgets rows with; a mismatch overflows the column")
		})
	}
}

func (s *cardfmtTestSuite) TestNilSnapshotDoesNotPanic() {
	issue := &beads.Issue{ID: "bv-1", Title: "t", Status: beads.StatusOpen}

	s.NotPanics(func() { _ = cardfmt.Render(s.th(), nil, issue, 30, false, true) })
}

func (s *cardfmtTestSuite) th() theme.Theme {
	return theme.New(config.ThemeDark, theme.BackgroundDark)
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}
