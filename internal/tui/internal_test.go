package tui

import (
	"image/color"
	"log/slog"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// WhiteBoxSuite holds tests that need access to unexported fields — checks
// that verify a load-bearing decision directly rather than inferring it
// through the View interface, which does not expose enough to observe it
// from outside the package.
type WhiteBoxSuite struct {
	suite.Suite
}

// spyView records every SetSize call it receives. It stands in for a
// concrete view so a test can prove every pane — not only the active one —
// is resized; a real view's own View() output does not expose its width or
// height directly, so none of listview, treeview or boardview could
// demonstrate this on their own without also asserting on their internal
// layout arithmetic.
type spyView struct {
	width, height int
	resized       bool
}

func (v *spyView) SetSize(width, height int) {
	v.width, v.height = width, height
	v.resized = true
}

func (v *spyView) SetSnapshot(*beads.Snapshot) {}

func (v *spyView) SetTheme(theme.Theme) {}

func (v *spyView) Update(tea.Msg) tea.Cmd { return nil }

func (v *spyView) View() string { return "" }

func (v *spyView) Selected() *beads.Issue { return nil }

func (v *spyView) Reveal(string) bool { return false }

// TestInitRequestsTheBackgroundColour is I7's guard: Init is the single
// entry point for the whole background-detection feature — nothing else
// ever asks the terminal for its colour — and nothing pinned that it does,
// so `return tea.RequestBackgroundColor` could become `return nil` with the
// rest of this suite staying green (no BackgroundColorMsg ever arrives in a
// test that never sends one, and every test that does send one constructs
// it directly rather than deriving it from what Init returned).
func (s *WhiteBoxSuite) TestInitRequestsTheBackgroundColour() {
	m := &Model{}

	cmd := m.Init()
	s.Require().NotNil(cmd, "Init must return a command, not nil")

	got := reflect.ValueOf(cmd).Pointer()
	want := reflect.ValueOf(tea.Cmd(tea.RequestBackgroundColor)).Pointer()
	s.Equal(want, got, "Init must ask the terminal for its background colour specifically")
}

// TestBackgroundDetectionReachesEveryView is C2's guard, reproducing the
// reviewer's own probe: listview, treeview and boardview.New each capture a
// theme.Theme BY VALUE at construction, and the View interface had no
// SetTheme, so applyBackground's theme rebuild reached m.theme and the
// detail pane but never the three panes actually on screen — every
// terminal that answers the OSC 11 query (the common local case) spent the
// whole session on whichever palette NewModel happened to resolve first.
// This reads the active view's own View() output directly (only WhiteBoxSuite
// can — Model.views is unexported), before and after a light
// tea.BackgroundColorMsg lands on a Model built agnostic (auto +
// BackgroundUnknown), and requires the bytes to differ. Before View gained
// SetTheme and applyBackground started calling it, this failed with
// before == after: the row was byte-identical.
func (s *WhiteBoxSuite) TestBackgroundDetectionReachesEveryView() {
	m, err := NewModel(Deps{
		Log:      slog.New(slog.DiscardHandler),
		Cfg:      config.Config{Theme: config.ThemeAuto, View: config.ViewList},
		Theme:    theme.New(config.ThemeAuto, theme.BackgroundUnknown),
		Snapshot: beads.NewSnapshot([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}}),
		Reload:   func() tea.Msg { return nil },
	})
	s.Require().NoError(err)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	before := m.views[m.active].View()

	m.Update(tea.BackgroundColorMsg{Color: color.White})

	after := m.views[m.active].View()
	s.NotEqual(before, after,
		"the active view must restyle once the terminal's background is actually detected")
}

// TestResizeReachesEveryView is the regression guard for a resize that only
// reaches the active pane. resize's loop over m.views cannot be observed
// through the placeholder view (nothing it renders depends on its size), so
// this constructs Model directly with spies in every slot: a view that first
// learns its width on activation paints its first frame at the wrong size,
// and that would stay invisible until a real view landed in a later task.
func (s *WhiteBoxSuite) TestResizeReachesEveryView() {
	spies := [viewCount]View{&spyView{}, &spyView{}, &spyView{}, &spyView{}}
	// detail is deliberately left nil: this test isolates resize's loop over
	// m.views from the real detail pane, and resize (see app.go) guards the
	// nil case for exactly this kind of direct construction.
	m := &Model{keys: DefaultKeyMap(), views: spies, active: 0}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// true, matching resize's own call: Task 4.2 always reserves detail
	// space now that the pane exists, so this must ask Compute the same
	// question resize does, not the pre-detail answer.
	want := Compute(120, 40, true)
	// paneHeights, not want.BodyHeight directly: a bordered layout (as this
	// one is) deducts its own frame from the list pane's share, the same
	// deduction applyLayout applies before calling SetSize.
	wantHeight, _ := want.paneHeights()
	for i := range m.views {
		spy, ok := m.views[i].(*spyView)
		s.Require().True(ok)
		s.True(spy.resized, "view %d never received SetSize", i)
		s.Equal(want.ListWidth, spy.width, "view %d got the wrong width", i)
		s.Equal(wantHeight, spy.height, "view %d got the wrong height", i)
	}
}

// TestPgDownDoesNotPanicWithoutADetailPane pins keys.go's nil guard on
// m.detail in the pgup/pgdown routing branch of handleKey — the one place
// that used to check only m.layout.DetailWidth > 0, unlike joinPanes, resize
// and syncDetail, which all guard m.detail directly and document why: a
// Model built by hand for a test (as this one is, mirroring
// TestResizeReachesEveryView) rather than through NewModel has a nil detail
// pane. Proven to panic without the guard: a directly-built Model, given a
// WindowSizeMsg wide enough to set layout.DetailWidth > 0, then a pgdown
// key press, dereferences the nil m.detail in m.detail.Update(msg).
func (s *WhiteBoxSuite) TestPgDownDoesNotPanicWithoutADetailPane() {
	spies := [viewCount]View{&spyView{}, &spyView{}, &spyView{}, &spyView{}}
	m := &Model{keys: DefaultKeyMap(), views: spies, active: 0}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	s.NotPanics(func() {
		m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	})
}

// TestTabIsANoOpWithNoDetailPaneOnScreen pins toggleFocus's detailOnScreen
// guard. A Model built by hand rather than through NewModel has a nil detail
// pane — the same construction TestPgDownDoesNotPanicWithoutADetailPane uses,
// and the only way to reach this state, since Compute gives even a 1-column
// terminal a stacked detail pane of the full width.
func (s *WhiteBoxSuite) TestTabIsANoOpWithNoDetailPaneOnScreen() {
	spies := [viewCount]View{&spyView{}, &spyView{}, &spyView{}, &spyView{}}
	m := &Model{keys: DefaultKeyMap(), views: spies, active: 0}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	s.Equal(focusView, m.overlay.focus,
		"with nothing to focus, Tab must leave focus where it was")
}

// TestBoardGetsTheWholeBodyWidth pins that the board is laid out with no
// detail pane beside it. A spy is what makes this observable: a real board's
// View() does not report the width it was handed, and inferring it from the
// rendered frame cannot tell "full width" apart from "split, but the detail
// pane happened to render nothing".
func (s *WhiteBoxSuite) TestBoardGetsTheWholeBodyWidth() {
	spies := [viewCount]View{&spyView{}, &spyView{}, &spyView{}, &spyView{}}
	m := &Model{keys: DefaultKeyMap(), views: spies, active: boardSlot}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	want := Compute(120, 40, false)
	s.Zero(want.DetailWidth, "fixture assumption: no detail pane without one")
	board, ok := m.views[boardSlot].(*spyView)
	s.Require().True(ok)
	s.Equal(want.ListWidth, board.width)
}

// TestSwitchingToTheBoardRelaysOutWithoutAResize is the trap: applyLayout
// used to run only on WindowSizeMsg, so a view switch left the board sized
// for a split it no longer has until the terminal happened to be resized.
func (s *WhiteBoxSuite) TestSwitchingToTheBoardRelaysOutWithoutAResize() {
	spies := [viewCount]View{&spyView{}, &spyView{}, &spyView{}, &spyView{}}
	m := &Model{keys: DefaultKeyMap(), views: spies, active: 0}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})

	board, ok := m.views[boardSlot].(*spyView)
	s.Require().True(ok)
	s.Equal(Compute(120, 40, false).ListWidth, board.width)

	// And back again, or leaving the board would strand the two-pane layout.
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	list, ok := m.views[0].(*spyView)
	s.Require().True(ok)
	s.Equal(Compute(120, 40, true).ListWidth, list.width)
}

// TestResizeGivesTheDetailPaneItsOwnWidth pins that resize hands the detail
// pane layout.DetailWidth specifically, not some other value such as
// ListWidth by copy-paste accident.
//
// Reading m.detail.View() directly, rather than the joined View() an
// external test is limited to, is what makes this discriminating:
// lipgloss.JoinHorizontal concatenates the list pane's own (much narrower,
// unpadded) content onto every row, so a detail pane mistakenly given the
// list's width still produces a *joined* line wider than ListWidth thanks to
// that unrelated contribution — measured directly, a first version of this
// test kept passing under exactly that mutation. Reading the detail pane's
// own output in isolation removes the ambiguity.
func (s *WhiteBoxSuite) TestResizeGivesTheDetailPaneItsOwnWidth() {
	m, err := NewModel(Deps{
		Log:   slog.New(slog.DiscardHandler),
		Cfg:   config.Config{Theme: config.ThemeDark, View: config.ViewList},
		Theme: theme.New(config.ThemeDark, theme.BackgroundDark),
		Snapshot: beads.NewSnapshot([]beads.Issue{{
			ID: "bv-1", Title: "T", Status: beads.StatusOpen,
			Description: strings.Repeat("word ", 200),
		}}),
		Reload: func() tea.Msg { return nil },
	})
	s.Require().NoError(err)

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	layout := Compute(120, 40, true)
	s.Require().Greater(layout.DetailWidth, layout.ListWidth,
		"fixture assumption: this width must actually split unevenly")

	widest := 0
	for line := range strings.SplitSeq(m.detail.View(), "\n") {
		widest = max(widest, lipgloss.Width(line))
	}

	s.Greater(widest, layout.ListWidth,
		"the detail pane must have been given its own, wider width — not the list's")
	s.LessOrEqual(widest, layout.DetailWidth)
}

// TestEveryViewKindRoundTripsThroughItsSlot is bv-vz2.4's drift guard:
// viewCount, the views literal in NewModel and viewKinds are three places
// that must agree, and ireturn rules out collapsing them into one kind-keyed
// constructor (View is an interface). This pins the agreement instead.
//
// A length check would not: viewKinds returns a [viewCount]config.ViewKind,
// so its length is viewCount by construction. The real failure is a kind
// that exists in config but is missing from viewKinds — activeIndex then
// falls through to its "unrecognised kind" return 0, and BV_VIEW=deps
// silently opens the list instead.
func (s *WhiteBoxSuite) TestEveryViewKindRoundTripsThroughItsSlot() {
	for _, kind := range []config.ViewKind{
		config.ViewList, config.ViewTree, config.ViewBoard, config.ViewDeps,
	} {
		s.Equal(kind, viewKindAt(activeIndex(kind)), "%s must round-trip through its slot", kind)
	}

	m, err := NewModel(Deps{
		Cfg:   config.Config{},
		Theme: theme.New(config.ThemeDark, theme.BackgroundDark),
	})
	s.Require().NoError(err)
	for i, v := range m.views {
		s.NotNil(v, "slot %d (%s) was never constructed", i, viewKinds()[i])
	}
}
