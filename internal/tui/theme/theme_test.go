package theme_test

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

func TestTheme(t *testing.T) {
	suite.Run(t, new(themeTestSuite))
}

type themeTestSuite struct {
	suite.Suite
}

func (s *themeTestSuite) TestResolve() {
	cases := []struct {
		name     string
		pref     config.ThemePreference
		detected theme.DetectedBackground
		want     theme.Scheme
	}{
		{"explicit dark wins over a light terminal", config.ThemeDark, theme.BackgroundLight, theme.SchemeDark},
		{"explicit light wins over a dark terminal", config.ThemeLight, theme.BackgroundDark, theme.SchemeLight},
		{"explicit light wins over no answer", config.ThemeLight, theme.BackgroundUnknown, theme.SchemeLight},
		{"auto follows a dark terminal", config.ThemeAuto, theme.BackgroundDark, theme.SchemeDark},
		{"auto follows a light terminal", config.ThemeAuto, theme.BackgroundLight, theme.SchemeLight},
		// The regression guard, and the point of the whole task. Over SSH and
		// inside tmux the terminal never answers. Resolving that to dark, even
		// under a named branch, hands a light-terminal user the dark-tuned
		// palette: near-white text on white. Assuming neither background is
		// the only choice that is safe on both.
		{
			"auto with no answer is background-agnostic, not dark",
			config.ThemeAuto, theme.BackgroundUnknown, theme.SchemeAgnostic,
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, theme.Resolve(tc.pref, tc.detected))
		})
	}
}

func (s *themeTestSuite) TestExplicitPreferenceIgnoresDetectionEntirely() {
	for _, detected := range []theme.DetectedBackground{
		theme.BackgroundUnknown, theme.BackgroundLight, theme.BackgroundDark,
	} {
		s.Equal(theme.SchemeDark, theme.Resolve(config.ThemeDark, detected))
		s.Equal(theme.SchemeLight, theme.Resolve(config.ThemeLight, detected))
	}
}

func (s *themeTestSuite) TestGlamourStyleTracksTheResolvedScheme() {
	// glamour v2 removed WithAutoStyle, so the style name must be supplied.
	// Deriving it from the resolved scheme keeps markdown and chrome in step.
	// Agnostic gets its own name: glamour's "dark" and "light" stylesheets
	// both bake in a background assumption this scheme declines to make.
	s.Equal("dark", theme.New(config.ThemeDark, theme.BackgroundLight).GlamourStyle())
	s.Equal("light", theme.New(config.ThemeLight, theme.BackgroundDark).GlamourStyle())
	s.Equal("notty", theme.New(config.ThemeAuto, theme.BackgroundUnknown).GlamourStyle())
}

func (s *themeTestSuite) TestAgnosticBaseStyleAssumesNothing() {
	// Pin the profile implicitly by never calling anything that queries a
	// terminal: New never does, so Render below is deterministic regardless
	// of whether this runs under CI, tmux, or a local TTY.
	th := theme.New(config.ThemeAuto, theme.BackgroundUnknown)

	s.Equal(lipgloss.NoColor{}, th.Base.GetForeground(),
		"agnostic base must not set a foreground: text must inherit the terminal's own colour")
	s.Equal(lipgloss.NoColor{}, th.Base.GetBackground(),
		"agnostic base must not set a background: it must not paint over the terminal's own")
}

func (s *themeTestSuite) TestAgnosticColoursAreANSINotHex() {
	th := theme.New(config.ThemeAuto, theme.BackgroundUnknown)

	// A fixed hex colour renders as a truecolor escape ("\x1b[38;2;r;g;bm");
	// an ANSI 0-15 slot renders as a plain SGR code. Asserting on the actual
	// rendered bytes, rather than merely "differs from the dark palette", is
	// what pins this down: a palette of different hex guesses would pass a
	// looser check while still baking in a background assumption.
	rendered := th.Accent.Render("x")
	s.NotContains(rendered, "38;2;", "agnostic colours must be ANSI slots, not fixed hex")
	s.NotEmpty(rendered)
}

func (s *themeTestSuite) TestPriorityStylesAreDistinctAndTotal() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	seen := map[string]bool{}
	for p := beads.PriorityCritical; p <= beads.PriorityBacklog; p++ {
		rendered := th.Priority(p).Render(p.Label())
		s.NotEmpty(rendered)
		seen[rendered] = true
	}
	s.Len(seen, 5, "each priority must be visually distinguishable")

	// Out-of-range values must not panic — Clamp is what makes this total.
	s.NotPanics(func() { th.Priority(beads.Priority(99)) })
	s.NotPanics(func() { th.Priority(beads.Priority(-1)) })
}

func (s *themeTestSuite) TestTypeGlyphHandlesUnknownValues() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	s.NotEmpty(th.TypeGlyph(beads.TypeBug))
	s.NotEmpty(th.TypeGlyph(beads.IssueType("spike")),
		"an unknown type still needs a glyph, or rows misalign")
}

func (s *themeTestSuite) TestFrameDistinguishesFocus() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	unfocused := th.Frame(false).Render("x")
	focused := th.Frame(true).Render("x")

	s.NotEqual(unfocused, focused,
		"the frame colour is the only indicator of which pane has focus")
	s.Contains(unfocused, "╭", "the frame is a rounded border")
	s.Contains(focused, "╭")
}

func (s *themeTestSuite) TestFrameStaysBackgroundAgnostic() {
	th := theme.New(config.ThemeAuto, theme.BackgroundUnknown)

	s.Equal(theme.SchemeAgnostic, th.Scheme)
	// The agnostic palette sets Border and Accent from ANSI 0-15 slots, which
	// every terminal maps to its own theme — so the frame still has a colour
	// here, it just carries no assumption about the background behind it.
	s.NotEqual(th.Frame(false).Render("x"), th.Frame(true).Render("x"))
}

func (s *themeTestSuite) TestTypeStylesDistinguishTheCommonTypes() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	bug := th.Type(beads.TypeBug).Render("x")
	epic := th.Type(beads.TypeEpic).Render("x")
	task := th.Type(beads.TypeTask).Render("x")

	s.NotEqual(bug, epic)
	s.NotEqual(bug, task)
	s.NotEqual(epic, task)
}

func (s *themeTestSuite) TestUnknownTypeFallsBackToBase() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	// br models issue_type as an open set and bv renders rather than
	// validates, so a type nobody here has heard of must still get a style.
	s.Equal(th.Base.Render("x"), th.Type(beads.IssueType("wibble")).Render("x"))
}

func (s *themeTestSuite) TestStatusStylesDistinguishProgressFromBlocked() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	s.NotEqual(
		th.Status(beads.StatusInProgress).Render("x"),
		th.Status(beads.StatusBlocked).Render("x"),
	)
	s.NotEqual(
		th.Status(beads.StatusOpen).Render("x"),
		th.Status(beads.StatusClosed).Render("x"),
	)
}

func (s *themeTestSuite) TestUnknownStatusFallsBackToBase() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	s.Equal(th.Base.Render("x"), th.Status(beads.Status("triaging")).Render("x"))
}

func (s *themeTestSuite) TestTypeAndStatusStayBackgroundAgnostic() {
	th := theme.New(config.ThemeAuto, theme.BackgroundUnknown)
	s.Require().Equal(theme.SchemeAgnostic, th.Scheme)

	// Base sets no attributes at all under this scheme — that is the one pair
	// guaranteed legible when nothing about the background is known — so the
	// fallback must not have acquired a colour on the way through.
	s.Equal("x", th.Type(beads.IssueType("wibble")).Render("x"))
	s.Equal("x", th.Status(beads.Status("triaging")).Render("x"))
	s.NotEqual(th.Status(beads.StatusOpen).Render("x"), th.Status(beads.StatusBlocked).Render("x"))
}
