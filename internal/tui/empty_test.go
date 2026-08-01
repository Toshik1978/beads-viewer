package tui_test

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

type emptyTestSuite struct {
	suite.Suite
}

// TestEachReasonSaysSomethingDifferent guards against the conflation this
// task exists to fix: an empty workspace, a filter matching nothing and a
// failed reload all render zero rows, and a test that only checked "the
// message is non-empty" would not notice all three collapsing back into one
// shared "No issues" string. Every pair is checked, not just workspace vs
// filtered, so a load-error message that happened to match one of the other
// two would still be caught.
func (s *emptyTestSuite) TestEachReasonSaysSomethingDifferent() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	workspace := tui.RenderEmptyForTest(tui.EmptyWorkspace, beads.Filter{}, "", th, 80, 20)
	filtered := tui.RenderEmptyForTest(tui.EmptyFilter,
		beads.Filter{Text: "zzz"}, "", th, 80, 20)
	loadError := tui.RenderEmptyForTest(tui.EmptyLoadError, beads.Filter{}, "decode: line 7", th, 80, 20)

	s.Contains(workspace, "br create")
	s.Contains(filtered, "zzz", "the message must quote the filter that matched nothing")
	s.Contains(filtered, "esc")
	s.Contains(loadError, "decode: line 7", "the actual decode error must reach the screen")
	s.Contains(loadError, "Nothing has loaded yet")
	s.NotEqual(workspace, filtered,
		"an empty workspace and an over-narrow filter are different problems")
	s.NotEqual(workspace, loadError,
		"an empty workspace and a failed reload are different problems")
	s.NotEqual(filtered, loadError,
		"an over-narrow filter and a failed reload are different problems")
}

// TestLoadErrorWithNoErrTextStillSaysSomethingTrue pins emptyLoadError's
// fallback wording for the test-seam case with no error text supplied
// (unreachable through the composed app — body() only reaches this branch
// when m.status.IsError, which is only ever set alongside a message) so
// renderEmpty still has something true to say rather than an empty line.
func (s *emptyTestSuite) TestLoadErrorWithNoErrTextStillSaysSomethingTrue() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)
	out := tui.RenderEmptyForTest(tui.EmptyLoadError, beads.Filter{}, "", th, 80, 20)

	s.Contains(out, "Nothing has loaded yet")
}

// TestLoadErrorDoesNotRunIntoTheHint is a regression guard found while
// verifying Important 3 live: emptyMessage originally spliced errText and
// nothingLoadedHint together with a literal "\n\n" separator, and renderEmpty
// then ran uitext.Sanitize over the WHOLE composed string — which strips
// every rune below 0x20, '\n' included, silently eating both newlines and
// gluing the two together with no space at all ("...beginning of
// valueNothing has loaded yet."), reproduced with RenderEmptyForTest before
// this fix. Sanitizing errText on its own, before it is spliced in, is what
// keeps the literal separator intact.
func (s *emptyTestSuite) TestLoadErrorDoesNotRunIntoTheHint() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)
	out := ansi.Strip(tui.RenderEmptyForTest(
		tui.EmptyLoadError, beads.Filter{}, "decode: unexpected end of value", th, 100, 24))

	s.NotContains(out, "valueNothing", "the error text must not run directly into the hint")
	s.Regexp(`value\s+Nothing has loaded yet`, out,
		"the hint must be visibly separated from the error text")
}

// TestNeverBlank pins that renderEmpty always has something to say at any
// positive size, down to a terminal too small to hold much of anything —
// and, at 0x0, that it degrades to nothing wider than the (zero) space it
// was given rather than panicking. Subtests are named by reason and size —
// s.Run("", ...) previously reported failures as bare "#00"…"#11", naming
// neither — and a line-count assertion sits beside the existing per-line
// width one: a renderEmpty that silently clipped the wrong number of lines
// (the same class of bug the wrap-vs-truncate switch in Important 3 exists
// to avoid) would satisfy the width check alone just as easily as one that
// clipped correctly.
func (s *emptyTestSuite) TestNeverBlank() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)
	reasons := []struct {
		name   string
		reason tui.EmptyReason
	}{
		{"workspace", tui.EmptyWorkspace},
		{"filter", tui.EmptyFilter},
		{"load-error", tui.EmptyLoadError},
	}
	for _, rc := range reasons {
		for _, size := range [][2]int{{80, 20}, {40, 10}, {10, 3}, {0, 0}} {
			s.Run(fmt.Sprintf("%s/%dx%d", rc.name, size[0], size[1]), func() {
				out := tui.RenderEmptyForTest(rc.reason, beads.Filter{}, "", th, size[0], size[1])
				lines := strings.Split(out, "\n")

				if size[0] > 0 && size[1] > 0 {
					s.NotEmpty(strings.TrimSpace(out), "must render something at %dx%d", size[0], size[1])
					s.LessOrEqual(len(lines), size[1], "must not exceed the given height")
				}
				for _, line := range lines {
					s.LessOrEqual(lipgloss.Width(line), max(size[0], 0), "must not exceed the given width")
				}
			})
		}
	}
}
