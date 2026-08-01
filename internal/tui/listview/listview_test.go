package listview_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/listview"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

func TestListView(t *testing.T) {
	suite.Run(t, new(truncateTestSuite))
	suite.Run(t, new(listViewTestSuite))
}

type truncateTestSuite struct {
	suite.Suite
}

// AMENDMENT — the upper bound alone is not a test.
//
// The original form of this suite asserted only
// `s.LessOrEqual(lipgloss.Width(got), tc.want)`. A `Truncate` that returned
// the empty string for every input would have passed all eight cases, so the
// test could not fail for the reason it exists. Every case now pins both
// bounds: a string that fits must come back byte-identical, and a string that
// is cut must still fill the space it was given.
//
// The lower bound is `width-1`, not `width`, and that single cell of slack is
// load-bearing rather than sloppy: a two-cell grapheme cannot be halved, so
// truncating "日本語のタイトル" to 8 cells yields "日本語…" at 7 — the next
// glyph would overflow. Verified against ansi.Truncate directly; see the
// table in Step 3.
func (s *truncateTestSuite) TestTruncateMeasuresDisplayWidth() {
	// gosmopolitan flags Han-script literals as likely hard-coded locale
	// text; these two exist to exercise CJK display-width counting, not to
	// label anything user-facing, so they are pulled into named constants
	// with the suppression once rather than repeated per table row.
	const (
		cjkTitle = "日本語のタイトル" //nolint:gosmopolitan // CJK width fixture, not locale text
		cjkWord  = "日本語"      //nolint:gosmopolitan // CJK width fixture, not locale text
	)

	cases := []struct {
		name  string
		in    string
		width int
		fits  bool // true when the input already fits and must survive verbatim
	}{
		{"short ascii is untouched", "hello", 10, true},
		{"exact fit is untouched", "hello", 5, true},
		{"ascii is cut with an ellipsis", "hello world", 8, false},
		// Each CJK glyph is two cells wide. A rune-based truncation would keep
		// six of these and render twelve cells into an eight-cell pane, and the
		// overflow wraps and corrupts every row below it.
		{
			"cjk counts two cells each",
			cjkTitle, // exercising CJK width, not locale text
			8,
			false,
		},
		{"emoji count two cells", "🚀🚀🚀🚀🚀", 6, false},
		{
			"mixed width",
			"fix " + cjkWord + " bug", // exercising CJK width, not locale text
			10,
			false,
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			got := uitext.Truncate(tc.in, tc.width)

			if tc.fits {
				s.Equal(tc.in, got, "a string that fits must not be altered")

				return
			}

			s.NotEqual(tc.in, got, "an over-long string must actually be cut")
			s.LessOrEqual(lipgloss.Width(got), tc.width,
				"result must never exceed the requested width")
			s.GreaterOrEqual(lipgloss.Width(got), tc.width-1,
				"result must fill the width it was given, less at most one "+
					"cell that a two-cell grapheme cannot occupy")
			s.Contains(got, "…", "a cut string must be marked as cut")
		})
	}
}

func (s *truncateTestSuite) TestTruncateRejectsNonPositiveWidths() {
	for _, width := range []int{0, -1, -5} {
		s.Run(strconv.Itoa(width), func() {
			s.Empty(uitext.Truncate("hello", width))
		})
	}
}

func (s *truncateTestSuite) TestTruncateNeverSplitsAGrapheme() {
	// Cutting mid-grapheme emits a replacement character and shifts the row.
	// NotEmpty is not decoration: without it this whole assertion holds
	// vacuously for a function that returns "".
	got := uitext.Truncate("日本語", 3) //nolint:gosmopolitan // exercising CJK width, not locale text
	s.NotEmpty(got)
	s.NotContains(got, "�")
	s.LessOrEqual(lipgloss.Width(got), 3)
}

type listViewTestSuite struct {
	suite.Suite
}

func (s *listViewTestSuite) newModel(issues []beads.Issue, w, h int) *listview.Model {
	m := listview.New(theme.New(config.ThemeDark, theme.BackgroundDark))
	m.SetSize(w, h)
	m.SetSnapshot(beads.NewSnapshot(issues))

	return m
}

func (s *listViewTestSuite) sample() []beads.Issue {
	return []beads.Issue{
		{
			ID: "bv-1", Title: "Add tree view", Status: beads.StatusOpen,
			Priority: beads.PriorityHigh, IssueType: beads.TypeFeature, Labels: []string{"ui"},
		},
		{
			ID: "bv-2", Title: "Fix decoder panic", Status: beads.StatusClosed,
			Priority: beads.PriorityCritical, IssueType: beads.TypeBug,
		},
		{
			//nolint:gosmopolitan // exercising CJK width, not locale text
			ID: "bv-3", Title: "日本語のタイトルです", Status: beads.StatusOpen,
			Priority: beads.PriorityMedium, IssueType: beads.IssueType("spike"),
		},
	}
}

// manyIssues builds n plain issues, for tests that need enough rows to force
// bubbles/v2/list into more than one page — the pagination footer only
// renders once TotalPages > 1.
func (s *listViewTestSuite) manyIssues(n int) []beads.Issue {
	issues := make([]beads.Issue, n)
	for i := range issues {
		issues[i] = beads.Issue{
			ID:        fmt.Sprintf("bv-%03d", i),
			Title:     fmt.Sprintf("Issue number %d", i),
			Status:    beads.StatusOpen,
			Priority:  beads.PriorityMedium,
			IssueType: beads.TypeFeature,
		}
	}

	return issues
}

func (s *listViewTestSuite) TestRendersEveryIssue() {
	out := s.newModel(s.sample(), 100, 20).View()

	s.Contains(out, "bv-1")
	s.Contains(out, "Add tree view")
	s.Contains(out, "bv-3", "an unknown issue type must not drop the row")
}

// TestNewlineInTitleDoesNotEmitAnExtraRow pins the sanitisation that keeps a
// delegate documented to emit one line per row honest: ansi.StringWidth
// treats "\n" as zero-width and ansi.Truncate never removes one, so an
// un-sanitised title containing a newline would slip through Truncate intact
// and render as a second physical row, shifting every row below it — the
// same corruption CJK truncation exists to prevent, through a different door.
// bv renders rather than validates hand-edited JSONL, so this is defensive
// rather than reachable through either fixture today.
func (s *listViewTestSuite) TestNewlineInTitleDoesNotEmitAnExtraRow() {
	issues := []beads.Issue{
		{ID: "bv-1", Title: "line one\nline two", Status: beads.StatusOpen, Priority: beads.PriorityHigh},
		{ID: "bv-2", Title: "after", Status: beads.StatusOpen, Priority: beads.PriorityLow},
	}
	m := s.newModel(issues, 100, 20)

	var rendered []string
	for line := range strings.SplitSeq(m.View(), "\n") {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			rendered = append(rendered, ansi.Strip(line))
		}
	}
	// The row count is the real proof: if the newline survived, "line one"
	// and "line two" render as two physical rows and bv-2's row is pushed
	// down, so len(rendered) comes out 3, not 2.
	s.Len(rendered, len(issues), "a newline in the title must not add a physical row")
	s.Require().NotEmpty(rendered)
	s.Contains(rendered[0], "line one", "the text around the newline must survive")
	s.Contains(rendered[0], "line two", "the text around the newline must survive, joined")
}

// TestEscapeInTitleDoesNotBreakTheRowsStyle pins the other half of the same
// sanitisation: a raw ESC (0x1b) reaching the terminal mid-line can terminate
// the row's own style sequence early. theme.Selected.Render("x") is a fixed
// number of escape bytes regardless of the text it wraps (no width or
// padding is set on it), so a selected row that still carries exactly that
// overhead proves the ESC never reached the terminal as a literal byte.
func (s *listViewTestSuite) TestEscapeInTitleDoesNotBreakTheRowsStyle() {
	th := theme.New(config.ThemeDark, theme.BackgroundDark)
	overhead := len(th.Selected.Render("x")) - len("x")

	issues := []beads.Issue{
		{ID: "bv-1", Title: "click \x1bhere", Status: beads.StatusOpen, Priority: beads.PriorityHigh},
	}
	m := listview.New(th)
	m.SetSize(100, 20)
	m.SetSnapshot(beads.NewSnapshot(issues))
	s.Require().True(m.SelectByID("bv-1"))

	var line string
	for candidate := range strings.SplitSeq(m.View(), "\n") {
		if strings.Contains(ansi.Strip(candidate), "bv-1") {
			line = candidate
		}
	}
	s.Require().NotEmpty(line)

	stripped := ansi.Strip(line)
	for _, r := range stripped {
		s.False(r < 0x20 || r == 0x7f, "control character leaked into rendered output: %q", r)
	}
	s.Contains(stripped, "click here", "the text around the escape must survive, joined")
	s.Equal(overhead, len(line)-len(stripped),
		"the row's style overhead must match a clean render exactly — a stray "+
			"reset from the raw escape would shrink or grow it")
}

func (s *listViewTestSuite) TestNoLineExceedsTheWidth() {
	// The invariant that makes CJK truncation matter: an over-wide line wraps
	// and pushes every subsequent row out of place.
	cases := []struct {
		name   string
		issues []beads.Issue
		width  int
		height int
	}{
		{"40 cols", s.sample(), 40, 20},
		{"60 cols", s.sample(), 60, 20},
		{"100 cols", s.sample(), 100, 20},
		// 3 issues at height 20 never fills a page, so TotalPages == 1 and
		// bubbles/v2/list.paginationView renders nothing at all — a broken
		// pagination footer cannot surface unless there is more than one
		// page. This case is what actually exercises it: 50 issues in a
		// 10x6 pane forces enough pages that the footer's dots need the
		// arabic fallback, which is where the width overflow used to show.
		{"multiple pages forces the pagination footer to render", s.manyIssues(50), 10, 6},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			out := s.newModel(tc.issues, tc.width, tc.height).View()

			nonEmpty := false
			for line := range strings.SplitSeq(out, "\n") {
				s.LessOrEqual(lipgloss.Width(line), tc.width, "line: %q", line)
				if strings.TrimSpace(ansi.Strip(line)) != "" {
					nonEmpty = true
				}
			}
			// Without a lower bound, View() returning "" would satisfy every
			// width assertion above vacuously.
			s.True(nonEmpty, "at least one rendered line must be non-empty")
		})
	}
}

func (s *listViewTestSuite) TestSelectByID() {
	m := s.newModel(s.sample(), 100, 20)

	s.True(m.SelectByID("bv-3"))
	s.Equal("bv-3", m.SelectedID())
	s.Equal("bv-3", m.Selected().ID)

	s.False(m.SelectByID("missing"))
	s.Equal("bv-3", m.SelectedID(), "a failed selection must not move the cursor")
}

// TestEscapeDoesNotQuitTheListView pins the fix for a real bug: bubbles/v2/
// list's DefaultKeyMap binds "esc" (alongside "q") to Quit, and
// list.Model.Update returns tea.Quit straight from its own handleBrowsing —
// nothing in this package used to stop that. keys.go's handleGlobalKey
// intercepts "q" before any view ever sees it, but "esc" reached this list
// unfiltered, so pressing it from bv's default view silently ended the
// program. Without listview.New's DisableQuitKeybindings call, the cmd
// returned here is tea.Quit and this test goes red.
func (s *listViewTestSuite) TestEscapeDoesNotQuitTheListView() {
	m := s.newModel(s.sample(), 100, 20)

	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if cmd == nil {
		return
	}
	msg := cmd()
	_, isQuit := msg.(tea.QuitMsg)
	s.False(isQuit, "esc must not quit the list view; got %#v", msg)
}

// TestPagingLettersAreDisabledInTheListView pins the related minor: bubbles/
// v2/list's PrevPage/NextPage bind pgup/b/u and pgdown/f/d/left/h/right/l, of
// which only pgup/pgdown are documented (routed to the detail pane by
// keys.go) — the rest paged this view on keys neither the README nor the
// help overlay mention. listview.New unbinds PrevPage/NextPage entirely, so
// none of these should move the cursor or change the rendered page.
func (s *listViewTestSuite) TestPagingLettersAreDisabledInTheListView() {
	m := s.newModel(s.manyIssues(50), 10, 6)
	before := m.View()
	beforeSelection := m.SelectedID()

	for _, key := range []string{"b", "u", "f", "d", "left", "h", "right", "l", "pgup", "pgdown"} {
		m.Update(tea.KeyPressMsg{Text: key, Code: keyCode(key)})
	}

	s.Equal(before, m.View(), "none of the disabled paging keys may change the rendered page")
	s.Equal(beforeSelection, m.SelectedID(), "none of the disabled paging keys may move the cursor")
}

// keyCode maps a key spelling to the tea.KeyPressMsg.Code a real keypress
// would carry: single-rune keys arrive as the rune itself, named keys as
// their own tea.Key* constant. tea.KeyPressMsg.String() (what key.Matches
// compares against) is derived from Code, not Text, so a test that only set
// Text would silently match nothing and pass for the wrong reason.
func keyCode(k string) rune {
	switch k {
	case "left":
		return tea.KeyLeft
	case "right":
		return tea.KeyRight
	case "pgup":
		return tea.KeyPgUp
	case "pgdown":
		return tea.KeyPgDown
	default:
		return rune(k[0])
	}
}

func (s *listViewTestSuite) TestSelectedIsNilWhenEmpty() {
	m := s.newModel(nil, 100, 20)

	s.Nil(m.Selected())
	s.Empty(m.SelectedID())
	s.NotPanics(func() { _ = m.View() })
}

// TestUpdateMovesTheCursor pins that Update actually reaches list.Model's own
// navigation rather than being a no-op: a Model.Update that discarded its
// argument and returned nil would keep every other test in this suite green,
// since none of them sent it a key press before reading SelectedID back.
func (s *listViewTestSuite) TestUpdateMovesTheCursor() {
	m := s.newModel(s.sample(), 100, 20)
	// sortIssues orders by priority then id: bv-2 (P0), bv-1 (P1), bv-3 (P2).
	s.Require().Equal("bv-2", m.SelectedID(), "the default cursor is the highest-priority row")

	m.Update(tea.KeyPressMsg{Code: 'j'})
	s.Equal("bv-1", m.SelectedID(), "j must move the cursor down one row")

	m.Update(tea.KeyPressMsg{Code: 'k'})
	s.Equal("bv-2", m.SelectedID(), "k must move the cursor back up")
}

// TestUpdateClampsAtBothEnds pins that the cursor stops rather than wraps.
func (s *listViewTestSuite) TestUpdateClampsAtBothEnds() {
	m := s.newModel(s.sample(), 100, 20)
	s.Require().Equal("bv-2", m.SelectedID())

	m.Update(tea.KeyPressMsg{Code: 'k'})
	s.Equal("bv-2", m.SelectedID(), "up from the first row must not wrap to the last")

	for range s.sample() {
		m.Update(tea.KeyPressMsg{Code: 'j'})
	}
	s.Equal("bv-3", m.SelectedID(), "down past the last row must not wrap to the first")
}

// TestTerminalStatusRendersMuted covers item.go's muted branch for a closed
// issue that is NOT the cursor row. sample's only closed issue, bv-2, is P0
// and therefore always the selected row, so its muted style is masked by the
// selected style — this exercises the branch the selected row cannot.
func (s *listViewTestSuite) TestTerminalStatusRendersMuted() {
	issues := append(s.sample(), beads.Issue{
		ID: "bv-4", Title: "Old bug", Status: beads.StatusClosed,
		Priority: beads.PriorityBacklog, IssueType: beads.TypeBug,
	})
	m := s.newModel(issues, 100, 20)
	s.Require().NotEqual("bv-4", m.SelectedID(), "bv-4 (P4) must sort after the default cursor row")

	var closed, open string
	for line := range strings.SplitSeq(m.View(), "\n") {
		switch {
		case strings.Contains(ansi.Strip(line), "bv-4"):
			closed = line
		case strings.Contains(ansi.Strip(line), "bv-1"):
			open = line
		}
	}
	s.Require().NotEmpty(closed)
	s.Require().NotEmpty(open)

	// stylePrefix (a byte count) is not discriminating enough here: theme.Base
	// and theme.Muted are both a single Foreground call, and the two colours
	// this theme picks happen to render as same-length SGR codes, so a length
	// comparison ties even though the codes differ. styleCode compares the
	// actual leading escape sequence instead.
	s.NotEqual(styleCode(closed), styleCode(open),
		"a terminal-status row must be styled differently from an open row")
}

// TestSetSnapshotPreservesSelectedID exercises SetSnapshot's id-preservation
// in this package directly, rather than relying only on internal/tui's
// higher-level coverage of the same behaviour.
func (s *listViewTestSuite) TestSetSnapshotPreservesSelectedID() {
	m := s.newModel(s.sample(), 100, 20)
	s.Require().True(m.SelectByID("bv-3"))

	// bv-0 (P0, newer) sorts ahead of every sample issue, so the swap shifts
	// every row index — preservation must go by id, not position.
	m.SetSnapshot(beads.NewSnapshot(append([]beads.Issue{
		{ID: "bv-0", Title: "Zero", Status: beads.StatusOpen, Priority: beads.PriorityCritical},
	}, s.sample()...)))

	s.Equal("bv-3", m.SelectedID())
}

// TestSetSnapshotFallsBackWhenSelectedIDIsGone pins the other half of the
// same preservation logic: when the id no longer exists, the cursor must
// fall back to the first row instead of leaving a stale, invisible selection.
func (s *listViewTestSuite) TestSetSnapshotFallsBackWhenSelectedIDIsGone() {
	m := s.newModel(s.sample(), 100, 20)
	s.Require().True(m.SelectByID("bv-3"))

	m.SetSnapshot(beads.NewSnapshot([]beads.Issue{
		{ID: "bv-9", Title: "New", Status: beads.StatusOpen},
	}))

	s.Equal("bv-9", m.SelectedID(), "a vanished selection must fall back to the first row")
}

// TestIDColumnPaddingLinesUpTitles pins that the id column is padded to the
// widest id in the snapshot. Every sample id is 4 cells wide, so a delegate
// that stopped padding entirely (id := issue.ID) would still line every
// title up by coincidence; the longer id here is what makes the padding
// observable.
func (s *listViewTestSuite) TestIDColumnPaddingLinesUpTitles() {
	issues := append(s.sample(), beads.Issue{
		ID: "bv-1234", Title: "Long id issue", Status: beads.StatusOpen, Priority: beads.PriorityLow,
	})
	lines := strings.Split(ansi.Strip(s.newModel(issues, 100, 20).View()), "\n")

	col := func(needle string) int {
		for _, line := range lines {
			if before, _, ok := strings.Cut(line, needle); ok {
				// ansi.StringWidth of the prefix, not its byte length: the
				// unknown-type glyph "·" is 2 bytes but 1 cell, so a
				// byte-offset comparison reports a false mismatch on any row
				// that uses it.
				return ansi.StringWidth(before)
			}
		}
		s.Fail("needle not found in any rendered line", needle)

		return -1
	}

	s.Equal(col("Add tree view"), col("Long id issue"),
		"every row's title column must start at the same offset")
}

func (s *listViewTestSuite) TestDegenerateSizes() {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {5, 2}} {
		s.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func() {
			var out string
			s.NotPanics(func() {
				out = s.newModel(s.sample(), size[0], size[1]).View()
			})

			// {0,0} and {1,1} are exactly the sizes at which the pagination
			// footer's PaddingLeft(2) used to escape m.width's own check:
			// JoinVertical then pads every row up to that oversized footer,
			// so this must assert the width invariant, not merely survive.
			for line := range strings.SplitSeq(out, "\n") {
				s.LessOrEqual(lipgloss.Width(line), size[0], "line: %q", line)
			}
		})
	}
}

// AMENDMENT — do NOT call lipgloss.SetColorProfile; it does not exist.
//
// `lipgloss.SetColorProfile` and `lipgloss.Ascii` were v1 API. Neither symbol
// is in lipgloss v2.0.5 (`go doc charm.land/lipgloss/v2 SetColorProfile` →
// "no symbol"). v2 removed the global renderer entirely: `Style.Render` always
// emits the full escape sequence, and downsampling to the terminal's real
// capability happens later, at the `colorprofile.Writer` bubbletea wraps
// stdout in.
//
// Verified by running the same styled render under TERM=xterm-256color,
// TERM=dumb, NO_COLOR=1 and COLORTERM=truecolor: all four produce the
// identical "\x1b[1;38;5;205mhello\x1b[m". So golden output is *already*
// deterministic across CI, tmux and a local TTY, and there is nothing to pin.
//
// This supersedes the "pin the colour profile" line in Global Constraints,
// which was written against v1 semantics. The rule it was protecting against
// is real; the mechanism it named is not.
//
// The golden file therefore stores the ANSI-**stripped** render. That is a
// deliberate choice, not a shortcut: escape-laden goldens are unreviewable, so
// a wrong one gets regenerated rather than read, and the file stops being
// evidence. Stripping keeps the golden diffable and still pins what actually
// regresses — column order, padding, and truncation. Styling is asserted
// separately below, so dropping colour from the golden does not leave it
// untested.
func (s *listViewTestSuite) TestGoldenRendering() {
	m := s.newModel(s.sample(), 80, 10)
	// bv-2 (P0) is already the cursor row before any selection is made; that
	// makes SelectByID here a no-op that would pass even if selection were
	// broken entirely. bv-3 actually exercises the move.
	s.Require().True(m.SelectByID("bv-3"))

	actual := ansi.Strip(m.View())
	s.Equal(golden(s.T(), "list_80x10.golden", actual), actual)
}

// TestGoldenRenderingNarrowWidthTruncates pins truncation itself: the 80-wide
// golden's widest row is 42 cells, so nothing in it is actually cut despite
// its comment implying otherwise. 40 columns is narrow enough that bv-3's
// CJK title (20 cells) no longer fits the remaining width and must be cut.
func (s *listViewTestSuite) TestGoldenRenderingNarrowWidthTruncates() {
	m := s.newModel(s.sample(), 40, 10)
	s.Require().True(m.SelectByID("bv-3"))

	actual := ansi.Strip(m.View())
	s.Contains(actual, "…", "at this width the cjk title must actually be cut")
	s.Equal(golden(s.T(), "list_40x10.golden", actual), actual)
}

// TestSelectedRowIsStyled covers what the stripped golden cannot: that the
// cursor row is visually distinguishable at all. Without this, deleting every
// call to theme.Selected would keep the golden green.
func (s *listViewTestSuite) TestSelectedRowIsStyled() {
	m := s.newModel(s.sample(), 80, 10)
	// bv-2 is the default cursor row; selecting it would test nothing about
	// SelectByID actually moving the cursor. bv-3 does.
	s.Require().True(m.SelectByID("bv-3"))

	out := m.View()
	var selected, other string
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.Contains(ansi.Strip(line), "bv-3"):
			selected = line
		case strings.Contains(ansi.Strip(line), "bv-1"):
			other = line
		}
	}
	s.Require().NotEmpty(selected)
	s.Require().NotEmpty(other)

	s.NotEqual(ansi.Strip(selected), selected,
		"the selected row must carry styling")
	s.NotEqual(stylePrefix(selected), stylePrefix(other),
		"the selected row must be styled differently from an unselected one")
}

// stylePrefix summarises how much escape-sequence styling a line carries, so
// two lines can be compared for differing styling without depending on the
// exact codes lipgloss happens to emit. The byte count freed by stripping is
// a stand-in for the leading escape sequence itself — simpler, and just as
// sufficient for a "these two differ" assertion.
func stylePrefix(line string) string {
	return strconv.Itoa(len(line) - len(ansi.Strip(line)))
}

// leadingSGR matches a leading "Select Graphic Rendition" escape sequence —
// the \x1b[...m codes lipgloss.Style.Render emits for colour, bold, reverse
// and the like.
var leadingSGR = regexp.MustCompile(`^\x1b\[[0-9;]*m`)

// styleCode returns the actual leading escape sequence of line, rather than
// stylePrefix's byte count. Two single-attribute styles (theme.Base and
// theme.Muted are both a lone Foreground call) can render to the
// same-length code with different colours, which ties under stylePrefix; this
// distinguishes them by content instead.
func styleCode(line string) string {
	return leadingSGR.FindString(line)
}

// golden reads a golden file from testdata, or writes actual to it when the
// UPDATE_GOLDEN environment variable is set.
//
// A flag.Bool wired up the usual way needs a package-level *bool to hold it,
// and gochecknoglobals is enabled with no exemption for _test.go files; an
// environment variable gets the same "-update"-style workflow
// (UPDATE_GOLDEN=1 go test ./...) without a global.
//
// Kept local to this package: it is the only view with a golden test so far,
// and the brief is explicit that the second caller — not this one — justifies
// promoting it into a shared tuitest package.
func golden(t *testing.T, name, actual string) string {
	t.Helper()

	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(actual), 0o600); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}

		return actual
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}

	return string(b)
}
