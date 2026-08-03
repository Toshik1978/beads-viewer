package tui_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/boardview"
	"github.com/Toshik1978/beads-viewer/internal/tui/depsview"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
)

type helpTestSuite struct {
	suite.Suite
}

func (s *helpTestSuite) TestFitsIn80x24() {
	out := tui.RenderHelpForTest(tui.DefaultKeyMap(), theme.New(config.ThemeDark, theme.BackgroundDark), 80, 24)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	s.LessOrEqual(len(lines), 24, "the overlay must not scroll off an 80x24 terminal")
	for _, line := range lines {
		s.LessOrEqual(lipgloss.Width(line), 80, "line: %q", line)
	}
}

func (s *helpTestSuite) TestDocumentsEveryBoundKey() {
	// A key that works but is undocumented is a key nobody finds. Boundary-
	// aware keyIsDocumented, not a plain Contains, is required here for the
	// same reason TestDocumentsTreeAndBoardKeys' own comment explains: a
	// bare Contains(out, "1") or Contains(out, "y") is satisfied by any
	// digit anywhere in the overlay, or by "y" as a substring of "key
	// bindings", regardless of whether that key is actually bound.
	out := ansi.Strip(
		tui.RenderHelpForTest(tui.DefaultKeyMap(), theme.New(config.ThemeDark, theme.BackgroundDark), 100, 40),
	)

	for _, binding := range tui.DefaultKeyMap().All() {
		for _, k := range binding.Keys() {
			s.True(keyIsDocumented(out, k), "key %q is bound but undocumented", k)
		}
	}
}

func (s *helpTestSuite) TestMentionsTheThemeOverride() {
	out := tui.RenderHelpForTest(tui.DefaultKeyMap(), theme.New(config.ThemeDark, theme.BackgroundDark), 100, 40)
	s.Contains(out, "BV_THEME")
}

func (s *helpTestSuite) TestDegenerateSizes() {
	for _, size := range [][2]int{{0, 0}, {10, 3}, {40, 10}} {
		s.Run("", func() {
			s.NotPanics(func() {
				_ = tui.RenderHelpForTest(tui.DefaultKeyMap(),
					theme.New(config.ThemeDark, theme.BackgroundDark), size[0], size[1])
			})
		})
	}
}

// TestFitsAtTheHeightsHelpOverlayActuallyPasses closes I1 and I2 together.
// The overlay's un-placed body is exactly 20 lines at width 80 with today's
// binding set — confirmed by probing renderHelp directly: height 19 already
// drops the footnote, height 20 does not. Model's helpOverlay (app.go) sizes
// its call from BodyHeight, which lands at 22 or 20 depending on whether the
// background note is folded in, so those are the two heights this pins,
// independently of Model — a plain LessOrEqual(len(lines), height) check, as
// TestFitsIn80x24 above does, is satisfied by a body that silently dropped
// content just as easily as by one that did not; only checking *which*
// content survives catches that.
//
// This is what would have caught both named mutants: a 16th Navigation
// binding grows the taller column by one line, and forcing balanceColumns
// into a single column inflates it far more — both push the un-placed body
// past 20 lines, and placeHelp (help.go) clips silently from the tail rather
// than growing the frame.
//
// bv-7pt.6.1 added the dependency view's two bindings (ViewDeps, Back) to
// Views, which would have pushed the un-placed body to 22 lines had Filtering
// stayed its own column — helpGroups folded Filtering into Global instead
// (see that function's own comment), landing both columns at 16 lines and the
// un-placed body back at exactly 20. The pinned heights below did not move:
// they are BodyHeight's own real values, not derived from content size.
func (s *helpTestSuite) TestFitsAtTheHeightsHelpOverlayActuallyPasses() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)

	for _, height := range []int{22, 20} {
		s.Run("", func() {
			out := ansi.Strip(tui.RenderHelpForTest(tui.DefaultKeyMap(), th, 80, height))

			for _, binding := range tui.DefaultKeyMap().All() {
				for _, k := range binding.Keys() {
					s.True(keyIsDocumented(out, k), "key %q dropped at height %d", k, height)
				}
			}
			s.Contains(out, "BV_THEME", "the footnote must survive at height %d", height)
		})
	}
}

// TestDocumentsTreeBoardAndDepsKeys is I8's drift guard: helpGroups (help.go)
// keeps its own literal copy of treeview's, boardview's and depsview's key
// bindings rather than importing keyActions directly (see that function's own
// comment on why), which means nothing caught it automatically if one of
// those packages added or renamed a key without updating this table. All
// three packages' HelpKeys expose exactly the strings their real keyActions
// binds, so this closes that gap: a key present there and absent here now
// fails a test instead of shipping an overlay that omits it.
//
// A plain Contains(out, k) is not enough for a single-character key: "c" is
// a substring of half the descriptions in the overlay regardless of whether
// "c" is actually bound — confirmed by dropping the "c" binding from
// helpGroups outright and watching a first draft of this test, using plain
// Contains, stay green. renderGroup (help.go) always renders a binding's
// keys as "key1/key2  description", so keyIsDocumented requires the actual
// delimiters either side (start-of-token "^", "/" or whitespace on the
// left; "/" or the two-space gap on the right) rather than a bare substring
// match — which a second draft, checking only k+"  ", also failed: "p" is
// still a bare substring of "up/k", so a left boundary is needed too, not
// only a right one.
func (s *helpTestSuite) TestDocumentsTreeBoardAndDepsKeys() {
	out := ansi.Strip(
		tui.RenderHelpForTest(tui.DefaultKeyMap(), theme.New(config.ThemeDark, theme.BackgroundDark), 100, 40),
	)

	for _, k := range treeview.HelpKeys() {
		s.True(keyIsDocumented(out, k), "treeview key %q is bound but undocumented", k)
	}
	for _, k := range boardview.HelpKeys() {
		s.True(keyIsDocumented(out, k), "boardview key %q is bound but undocumented", k)
	}
	for _, k := range depsview.HelpKeys() {
		s.True(keyIsDocumented(out, k), "depsview key %q is bound but undocumented", k)
	}
}

// TestReadmeDocumentsExactlyTheBoundKeys is F2's drift guard. README.md's
// Keybindings section claims to be grouped "exactly as they appear" in the
// overlay, and nothing checked that until now: four rows had drifted — tab
// still documented as the swimlane key it stopped being, tab and enter absent
// from the tables they had moved to, and esc bound but named nowhere. The
// overlay is guarded against KeyMap by TestDocumentsEveryBoundKey above; this
// closes the same loop for the prose that claims to mirror it.
//
// It compares the two sets in both directions on purpose. A one-way "every
// bound key appears in README.md" check would have caught the three missing
// rows but not the stale tab row, since tab is still bound — just not to the
// action the README gave it.
//
// It compares keys, not descriptions: a wording difference between a table
// cell and a Binding.Help().Desc is editorial, and a test that failed on it
// would be reverted rather than obeyed.
func (s *helpTestSuite) TestReadmeDocumentsExactlyTheBoundKeys() {
	documented := s.readmeKeybindings()

	bound := map[string]bool{}
	for _, binding := range tui.DefaultKeyMap().All() {
		for _, k := range binding.Keys() {
			bound[k] = true
		}
	}
	for _, k := range slices.Concat(treeview.HelpKeys(), boardview.HelpKeys()) {
		bound[k] = true
	}

	for k := range bound {
		s.Contains(documented, k, "%q is bound but missing from README.md's key tables", k)
	}
	for k := range documented {
		s.Contains(bound, k, "README.md documents %q, which nothing binds", k)
	}
}

// readmeKeybindings collects every key named in the first column of a table
// row inside README.md's Keybindings section.
//
// It reads the published file rather than an embedded copy, so what is
// checked is what a reader actually sees. The section is bounded by its own
// "## " headings so that prose elsewhere in the file cannot satisfy the
// comparison by accident, and only backticked spans of a row's first cell
// count — which is what keeps "`bv`" in the section's own opening sentence,
// and every description cell, out of the set.
func (s *helpTestSuite) readmeKeybindings() map[string]bool {
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	s.Require().NoError(err)

	keys := map[string]bool{}
	code := regexp.MustCompile("`([^`]+)`")

	inSection := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.HasPrefix(line, "## ") {
			inSection = line == "## Keybindings"

			continue
		}
		cells := strings.Split(line, "|")
		if !inSection || !strings.HasPrefix(line, "|") || len(cells) < 3 {
			continue
		}
		for _, match := range code.FindAllStringSubmatch(cells[1], -1) {
			keys[match[1]] = true
		}
	}
	s.Require().NotEmpty(keys, "README.md's Keybindings section must still be findable")

	return keys
}

// keyIsDocumented reports whether k appears in out (already ANSI-stripped —
// each row is individually styled, so raw ANSI codes would otherwise sit
// between a key and its delimiter) as one of renderGroup's own key tokens,
// not merely as a substring of surrounding text.
func keyIsDocumented(out, k string) bool {
	pattern := `(^|/|\s)` + regexp.QuoteMeta(k) + `(/|  )`

	return regexp.MustCompile(pattern).MatchString(out)
}
