package tui_test

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

type statusTestSuite struct {
	suite.Suite
}

// TestShowsCounts checks the whole "N issues · N open · N ready · N blocked
// · N closed" substring, not each digit individually: five separate
// Contains checks pass even if Ready and Blocked (or any other pair) were
// swapped in the Sprintf, since every digit still appears somewhere in the
// line — confirmed by swapping them and watching a first draft of this test
// stay green.
func (s *statusTestSuite) TestShowsCounts() {
	out := tui.RenderStatusForTest(tui.StatusState{
		Counts: beads.Counts{Total: 60, Open: 3, Ready: 2, Blocked: 1, Closed: 56},
		View:   config.ViewList,
	}, theme.New(config.ThemeDark, theme.BackgroundDark), 120)

	s.Contains(out, "60 issues · 3 open · 2 ready · 1 blocked · 56 closed")
	s.Contains(out, "list")
}

func (s *statusTestSuite) TestFitsEveryWidth() {
	st := tui.StatusState{
		Counts:  beads.Counts{Total: 682, Open: 40, Ready: 12, Blocked: 8, Closed: 642},
		Filter:  beads.Filter{Text: "a fairly long filter term"},
		View:    config.ViewBoard,
		Message: "reload failed: malformed jsonl: line 412",
		IsError: true,
	}
	for _, width := range []int{20, 40, 60, 80, 120, 200} {
		s.Run("", func() {
			out := tui.RenderStatusForTest(st, theme.New(config.ThemeDark, theme.BackgroundDark), width)
			s.LessOrEqual(lipgloss.Width(out), width)
			s.NotContains(out, "\n", "the status bar is exactly one line")
		})
	}
}

// TestNeverBlankBetween30And51Columns is I1's fix pinned. counts — by far
// the widest segment, 52 cells here — was the last one renderStatus dropped,
// so a width it alone could not fit (30-51) fell straight through to a
// wholly blank bar, taking the view name, the active filter and the help
// hint down with it even though those three together fit in as little as 20
// cells. Measured blank at width 40 before this fix.
func (s *statusTestSuite) TestNeverBlankBetween30And51Columns() {
	st := tui.StatusState{
		Counts: beads.Counts{Total: 682, Open: 40, Ready: 12, Blocked: 8, Closed: 642},
		View:   config.ViewList,
	}
	out := tui.RenderStatusForTest(st, theme.New(config.ThemeDark, theme.BackgroundDark), 40)

	s.NotEmpty(strings.TrimSpace(ansi.Strip(out)),
		"the bar must not go blank just because counts alone does not fit")
	s.Contains(out, "list", "the view name fits easily once counts is dropped")
}

// TestBarIsPaddedToTheTerminalWidth is a MINOR fix pinned: StatusBar carries
// a background in both hue schemes and Reverse(true) in the agnostic one, so
// an unpadded bar shorter than the terminal used to render as a coloured
// fragment with bare terminal to its right — checked with and without the
// trailing message styled red, since renderLine pads each branch separately.
func (s *statusTestSuite) TestBarIsPaddedToTheTerminalWidth() {
	th := theme.New(config.ThemeAuto, theme.BackgroundUnknown)

	plain := tui.RenderStatusForTest(tui.StatusState{
		Counts: beads.Counts{Total: 1}, View: config.ViewList,
	}, th, 100)
	s.Equal(100, lipgloss.Width(plain), "the bar must fill the terminal width even with no error styled")

	withError := tui.RenderStatusForTest(tui.StatusState{
		Counts: beads.Counts{Total: 1}, View: config.ViewList, Message: "boom", IsError: true,
	}, th, 100)
	s.Equal(100, lipgloss.Width(withError), "padding after a styled error message must not be skipped")
}

func (s *statusTestSuite) TestErrorSurvivesNarrowing() {
	// Counts and the filter can be dropped to fit; the error never can, or a
	// bad reload becomes invisible on a small terminal.
	st := tui.StatusState{
		Counts:  beads.Counts{Total: 60},
		Filter:  beads.Filter{Text: "term"},
		Message: "line 412",
		IsError: true,
	}
	out := tui.RenderStatusForTest(st, theme.New(config.ThemeDark, theme.BackgroundDark), 30)
	s.Contains(out, "412")
}

// TestDropOrderMatchesThePriority pins renderStatus's documented drop
// order — the hint first, then the filter, then the view name, then the
// count breakdown, with the message surviving every width — which nothing
// pinned before this: reordering the optional slice (for instance, so the
// filter dropped before the hint) left every other test in this suite
// green, since they only ever check that *something* still fits, not which
// segment specifically survived at a given width.
//
// The five widths below straddle this fixture's own exact boundaries (full
// line 146 cells; without the hint, 137; without the filter too, 107;
// counts-only, 99; message-only, 40), so each step down drops exactly one
// more segment rather than an arbitrary number.
func (s *statusTestSuite) TestDropOrderMatchesThePriority() {
	st := tui.StatusState{
		Counts:  beads.Counts{Total: 682, Open: 40, Ready: 12, Blocked: 8, Closed: 642},
		Filter:  beads.Filter{Text: "a fairly long filter term"},
		View:    config.ViewBoard,
		Message: "reload failed: malformed jsonl: line 412",
	}
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	full := tui.RenderStatusForTest(st, th, 150)
	for _, want := range []string{"682", "board", "a fairly long filter term", "? help", "412"} {
		s.Contains(full, want, "everything must fit at a generous width")
	}

	noHint := tui.RenderStatusForTest(st, th, 140)
	s.NotContains(noHint, "? help", "the hint must be the first segment dropped")
	s.Contains(noHint, "a fairly long filter term")
	s.Contains(noHint, "board")
	s.Contains(noHint, "682")

	noFilter := tui.RenderStatusForTest(st, th, 110)
	s.NotContains(noFilter, "a fairly long filter term", "the filter must drop next")
	s.Contains(noFilter, "board")
	s.Contains(noFilter, "682")

	noView := tui.RenderStatusForTest(st, th, 100)
	s.NotContains(noView, "board", "the view name must drop next")
	s.Contains(noView, "682")

	noCounts := tui.RenderStatusForTest(st, th, 50)
	s.NotContains(noCounts, "682", "the count breakdown drops last, before only the message remains")
	s.Contains(noCounts, "412", "the message must survive every narrowing")
}

// TestNeverShowsTheLaneOutsideTheBoard pins that the swimlane indicator (the
// 6.2 addendum this task closes out) only ever appears once it is actually
// set — an empty Lane must not print a bare "()" that reads as a broken
// segment on every other view.
func (s *statusTestSuite) TestNeverShowsTheLaneOutsideTheBoard() {
	out := tui.RenderStatusForTest(tui.StatusState{
		View: config.ViewList,
	}, theme.New(config.ThemeDark, theme.BackgroundDark), 120)

	s.NotContains(out, "(")
}

// TestShowsTheLaneOnTheBoard pins the other half: once Lane is set, it must
// actually reach the rendered line, or boardview.SwimLane.Name stays dead API
// with a caller that never uses its result.
func (s *statusTestSuite) TestShowsTheLaneOnTheBoard() {
	out := tui.RenderStatusForTest(tui.StatusState{
		View: config.ViewBoard,
		Lane: "Priority",
	}, theme.New(config.ThemeDark, theme.BackgroundDark), 120)

	s.Contains(out, "Priority")
}
