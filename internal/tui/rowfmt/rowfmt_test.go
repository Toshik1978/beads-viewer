package rowfmt_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/rowfmt"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

func TestRowfmt(t *testing.T) {
	suite.Run(t, new(columnsTestSuite))
}

type columnsTestSuite struct {
	suite.Suite
}

func (s *columnsTestSuite) cols() rowfmt.Columns {
	return rowfmt.Columns{
		Glyph: "! ", ID: "bv-1 ", Priority: "P1 ", Status: "Open        ",
		Title: "a title", Age: "   2h",
	}
}

func (s *columnsTestSuite) issue() *beads.Issue {
	return &beads.Issue{
		ID: "bv-1", Title: "a title", IssueType: beads.TypeBug,
		Status: beads.StatusOpen, Priority: beads.PriorityHigh,
	}
}

func (s *columnsTestSuite) TestPlainAndStyledLayOutIdenticalText() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	for width := 1; width <= 120; width++ {
		s.Run("", func() {
			plain := s.cols().Plain(width)
			styled := ansi.Strip(s.cols().Styled(th, s.issue(), width))

			s.Equal(plain, styled,
				"a row must not shift when selection changes which path renders it")
			s.LessOrEqual(ansi.StringWidth(plain), width)
			s.LessOrEqual(ansi.StringWidth(styled), width)
		})
	}
}

func (s *columnsTestSuite) TestStyledColoursEachColumnSeparately() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	out := s.cols().Styled(th, s.issue(), 80)

	// The mutation this exists to catch is collapsing Styled to one Render
	// over the whole row. Comparing the glyph's and status's own sequences
	// against the body's is what makes that fail — a count of escape codes
	// does not, since one Render plus the age already emits several.
	s.Contains(out, th.Type(beads.TypeBug).Render("! "))
	s.Contains(out, th.Status(beads.StatusOpen).Render("Open        "))
	s.NotEqual(th.Base.Render("! "), th.Type(beads.TypeBug).Render("! "),
		"fixture assumption: a bug's glyph style differs from the body style")
}

func (s *columnsTestSuite) TestPlainKeepsTheAgeFlushRight() {
	line := strings.TrimRight(s.cols().Plain(80), " ")

	s.Equal(80, ansi.StringWidth(s.cols().Plain(80)))
	s.True(strings.HasSuffix(line, "2h"), "line: %q", line)
}
