package tui_test

import (
	"bytes"
	"errors"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
	"github.com/Toshik1978/beads-viewer/internal/tui/treeview"
)

type appTestSuite struct {
	suite.Suite
}

// transitiveFieldCount counts t's fields, and for an embedded (anonymous)
// struct field, counts that struct's own fields rather than the field itself.
// A plain reflect.Type.NumField() call stops at the top level, so replacing
// several named fields with one embedded struct would silently pass the
// tripwire in TestModelStaysSmall; counting transitively closes that gap.
func transitiveFieldCount(t reflect.Type) int {
	count := 0
	for field := range t.Fields() {
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			count += transitiveFieldCount(field.Type)

			continue
		}
		count++
	}

	return count
}

func (s *appTestSuite) newModel(issues []beads.Issue) *tui.Model {
	return s.newModelWithLog(slog.New(slog.DiscardHandler), issues)
}

// newModelWithLog is newModel with the logger broken out, for I7's evidence
// test below — every other caller in this suite only needs a discard
// handler, since none of them inspect what bv would have logged.
func (s *appTestSuite) newModelWithLog(log *slog.Logger, issues []beads.Issue) *tui.Model {
	m, err := tui.NewModel(tui.Deps{
		Log:      log,
		Cfg:      config.Config{Theme: config.ThemeDark, View: config.ViewList},
		Theme:    theme.New(config.ThemeDark, theme.BackgroundDark),
		Snapshot: beads.NewSnapshot(issues),
		Reload:   func() tea.Msg { return nil },
	})
	s.Require().NoError(err)

	return m
}

// newTreeModel builds a Model already switched to the tree view and sized,
// the shared setup I1's two evidence tests below and TestLoadTreeState-
// SyncsTheDetailPane's own local closure all need.
func (s *appTestSuite) newTreeModel(issues []beads.Issue) *tui.Model {
	m, err := tui.NewModel(tui.Deps{
		Log:      slog.New(slog.DiscardHandler),
		Cfg:      config.Config{Theme: config.ThemeDark, View: config.ViewTree},
		Theme:    theme.New(config.ThemeDark, theme.BackgroundDark),
		Snapshot: beads.NewSnapshot(issues),
		Reload:   func() tea.Msg { return nil },
	})
	s.Require().NoError(err)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	return m
}

// sample is this suite's shared fixture: enough issues, of varied type and
// status, to fill a pane and exercise selection.
func (s *appTestSuite) sample() []beads.Issue {
	return []beads.Issue{
		{ID: "bv-1", Title: "first", IssueType: beads.TypeBug, Status: beads.StatusOpen},
		{ID: "bv-2", Title: "second", IssueType: beads.TypeTask, Status: beads.StatusInProgress},
		{ID: "bv-3", Title: "third", IssueType: beads.TypeEpic, Status: beads.StatusClosed},
	}
}

// longIssue is an issue whose description cannot fit a 40-row detail pane, so
// scrolling it actually changes what is rendered.
func longIssue() beads.Issue {
	body := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 200)

	return beads.Issue{
		ID:          "bv-1",
		Title:       "long",
		Status:      beads.StatusOpen,
		Description: body,
	}
}

func (s *appTestSuite) TestModelStaysSmall() {
	// The rewrite exists because the previous Model had ~100 fields. This test
	// is the tripwire; if it fails, move state into a view rather than raising
	// the bound. It counts transitively — see transitiveFieldCount — so
	// folding several fields into one embedded struct cannot evade the cap;
	// that is exactly the shape a god-object regression would take.
	s.LessOrEqual(transitiveFieldCount(reflect.TypeFor[tui.Model]()), 12,
		"new state belongs inside a view, not on the root model")
}

func (s *appTestSuite) TestTransitiveFieldCountSeesThroughAnEmbeddedStruct() {
	// Pins transitiveFieldCount's own behaviour, since TestModelStaysSmall
	// cannot demonstrate the regression it guards against while Model itself
	// has no embedded fields: a naive reflect.Type.NumField() would report 2
	// for outer below (the embedded inner plus D), silently undercounting the
	// 4 real fields — the exact evasion an embedded struct would offer a
	// future god-object regression.
	type inner struct{ A, B, C int }
	type outer struct {
		inner

		D int
	}

	s.Equal(4, transitiveFieldCount(reflect.TypeFor[outer]()),
		"an embedded struct must be counted by its own fields, not as a single field")
}

func (s *appTestSuite) TestSwitchingViewsPreservesSelection() {
	m := s.newModel([]beads.Issue{
		{ID: "bv-1", Title: "One", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "Two", Status: beads.StatusOpen},
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: 'j'})

	before := m.SelectedID()
	m.Update(tea.KeyPressMsg{Code: '2'}) // switch to tree
	m.Update(tea.KeyPressMsg{Code: '1'}) // and back
	s.Equal(before, m.SelectedID(), "selection is by id, not row index")
}

// mkIssue builds an open issue with id (and title, for readability in a
// failure message), leaving every opt to layer on whatever else a test needs.
func mkIssue(id string, opts ...func(*beads.Issue)) beads.Issue {
	i := beads.Issue{ID: id, Title: id, Status: beads.StatusOpen}
	for _, opt := range opts {
		opt(&i)
	}

	return i
}

// withParent records a parent-child dependency on the issue being built, so
// the tree view has an ancestor to collapse and later expand via Reveal.
func withParent(parentID string) func(*beads.Issue) {
	return func(i *beads.Issue) {
		i.Dependencies = append(i.Dependencies, beads.Dependency{
			IssueID: i.ID, DependsOnID: parentID, Type: beads.DepParentChild,
		})
	}
}

// withDep records a dependency of type t from the issue being built onto
// target, so the dependency view has a column to populate.
func withDep(t beads.DepType, target string) func(*beads.Issue) {
	return func(i *beads.Issue) {
		i.Dependencies = append(i.Dependencies, beads.Dependency{
			IssueID: i.ID, DependsOnID: target, Type: t,
		})
	}
}

// keyPress builds the single-character key press s's binding expects. Text is
// set alongside Code because handleFilterKey (and the filter-box tests that
// exercise it) read msg.Text, not msg.Code, to find out what was typed.
func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// TestSwitchingViewsCarriesTheSelection is Task 3's guard: each view keeps
// its own cursor, so without carrySelection a switch lands wherever that
// view was last left rather than on the issue the user was reading.
func (s *appTestSuite) TestSwitchingViewsCarriesTheSelection() {
	for _, tc := range []struct {
		name string
		key  string
		want config.ViewKind
	}{
		{name: "list to tree", key: "2", want: config.ViewTree},
		{name: "list to board", key: "3", want: config.ViewBoard},
	} {
		s.Run(tc.name, func() {
			// Three levels, not two: treeview.buildNode only defaults a
			// depth-0 node to Expanded (tree.go), so a direct child of a
			// root already has a visible row on the very first SetSnapshot
			// — Reveal would have nothing to do and a bare SelectByID would
			// pass this test identically. "mid" sits at depth 1 and
			// defaults collapsed, which genuinely hides "kid"'s row until
			// Reveal expands it.
			m := s.newModel([]beads.Issue{
				mkIssue("root"),
				mkIssue("mid", withParent("root")),
				mkIssue("kid", withParent("mid")),
				mkIssue("other"),
			})
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			s.Require().True(m.SelectID("kid"), "precondition: the list can select kid")
			s.Require().Equal("kid", m.SelectedID())

			m.Update(keyPress(tc.key))

			s.Equal(tc.want, tui.ViewKindForTest(m))
			s.Equal("kid", m.SelectedID(), "the incoming view must open on the issue the outgoing view had")
		})
	}
}

// TestSwitchingBackCarriesTheSelectionToo proves the carry works in both
// directions, not just outward from the list — the list's own Reveal is a
// bare SelectByID delegate, so this is what actually exercises it.
func (s *appTestSuite) TestSwitchingBackCarriesTheSelectionToo() {
	m := s.newModel([]beads.Issue{
		mkIssue("root"),
		mkIssue("kid", withParent("root")),
		mkIssue("other"),
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(keyPress("2")) // tree
	s.Require().True(m.SelectID("other"))
	m.Update(keyPress("1")) // back to the list

	s.Equal(config.ViewList, tui.ViewKindForTest(m))
	s.Equal("other", m.SelectedID())
}

// TestSwitchingWithNothingSelectedStillSwitches pins carrySelection's silent
// failure path: Selected() is nil on an empty snapshot, and that must not
// block the switch itself.
func (s *appTestSuite) TestSwitchingWithNothingSelectedStillSwitches() {
	m := s.newModel(nil)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(keyPress("2"))

	s.Equal(config.ViewTree, tui.ViewKindForTest(m))
}

// TestFourOpensTheDependencyViewOnTheSelection is Task 10's guard: pressing 4
// must switch to the dependency view and carry the active selection in as its
// focus, the same way switching to the tree or board does.
func (s *appTestSuite) TestFourOpensTheDependencyViewOnTheSelection() {
	m := s.newModel([]beads.Issue{
		mkIssue("focus"),
		mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	s.Require().True(m.SelectID("focus"))

	m.Update(keyPress("4"))

	s.Equal(config.ViewDeps, tui.ViewKindForTest(m))
	s.Contains(ansi.Strip(tui.ActivePaneForTest(m)), "blocked by")
	s.Contains(ansi.Strip(tui.ActivePaneForTest(m)), "waiter")
}

// TestLeavingTheDependencyViewCarriesItsFocus proves carrySelection reaches
// the dependency view's own Reveal, which re-roots rather than moving a
// cursor: a walk through it that ends on "waiter" must land the list there
// too, not back on "focus".
func (s *appTestSuite) TestLeavingTheDependencyViewCarriesItsFocus() {
	m := s.newModel([]beads.Issue{
		mkIssue("focus"),
		mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	s.Require().True(m.SelectID("focus"))

	m.Update(keyPress("4"))
	m.Update(keyPress("l")) // onto the blocks column
	m.Update(keyPress("l"))
	s.Require().Equal("waiter", m.SelectedID())

	m.Update(keyPress("1"))

	s.Equal(config.ViewList, tui.ViewKindForTest(m))
	s.Equal("waiter", m.SelectedID(), "a walk in the dependency view must land the list where it ended")
}

// TestTheDependencyViewTakesTheWholeBody pins showsDetail's exclusion of the
// dependency view: four columns squeezed into 42% of the terminal would be
// under the board's own minimum column width.
func (s *appTestSuite) TestTheDependencyViewTakesTheWholeBody() {
	m := s.newModel([]beads.Issue{mkIssue("a")})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(keyPress("4"))

	s.Equal(0, tui.LayoutForTest(m).DetailWidth)
}

// TestOpeningDirectlyOnTheDependencyViewSeedsAFocus is Important 1's guard.
// carrySelection only ever reveals an id on a view *switch* (keys.go), so a
// Model that opens straight onto the dependency view — `bv --view deps`,
// `view: deps` and BV_VIEW=deps all construct one this way, through
// NewModel's own applyFilter call — never had a switch to run Reveal from.
// Before the fix this rendered "blocked by (0) | focused (0) | blocks (0) |
// related (0)" with SelectedID() == "", confirmed empirically against this
// exact fixture: two issues and a WindowSizeMsg, nothing else.
func (s *appTestSuite) TestOpeningDirectlyOnTheDependencyViewSeedsAFocus() {
	m, err := tui.NewModel(tui.Deps{
		Log:   slog.New(slog.DiscardHandler),
		Cfg:   config.Config{Theme: config.ThemeDark, View: config.ViewDeps},
		Theme: theme.New(config.ThemeDark, theme.BackgroundDark),
		Snapshot: beads.NewSnapshot([]beads.Issue{
			mkIssue("focus"),
			mkIssue("waiter", withDep(beads.DepBlocks, "focus")),
		}),
		Reload: func() tea.Msg { return nil },
	})
	s.Require().NoError(err)

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	s.Equal(config.ViewDeps, tui.ViewKindForTest(m))
	s.NotEmpty(m.SelectedID(), "the view must adopt a focus on its own, not wait for a switch that never comes")
	s.Contains(ansi.Strip(tui.ActivePaneForTest(m)), "focused (1)",
		"the middle column must actually hold the subject, not read as an empty pane naming nothing")
}

func (s *appTestSuite) TestReloadPreservesSelectionByID() {
	m := s.newModel([]beads.Issue{
		{ID: "bv-1", Title: "One", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "Two", Status: beads.StatusOpen},
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: 'j'})
	selected := m.SelectedID()

	// A new issue sorting above the selection would shift every row index.
	m.Update(tui.SnapshotMsg{Snapshot: beads.NewSnapshot([]beads.Issue{
		{ID: "bv-0", Title: "Zero", Status: beads.StatusOpen, Priority: beads.PriorityCritical},
		{ID: "bv-1", Title: "One", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "Two", Status: beads.StatusOpen},
	})})

	s.Equal(selected, m.SelectedID())
}

func (s *appTestSuite) TestReloadErrorKeepsThePreviousSnapshot() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(tui.SnapshotMsg{Err: errors.New("malformed jsonl: line 42")})

	s.Contains(m.View(), "bv-1", "a bad write must never blank the UI")
	s.Contains(m.View(), "line 42", "and the error must be visible")
}

func (s *appTestSuite) TestFilterAppliesToEveryView() {
	issues := []beads.Issue{
		{ID: "bv-1", Title: "keep me", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "drop me", Status: beads.StatusOpen},
	}
	for _, view := range []rune{'1', '2', '3'} {
		s.Run(string(view), func() {
			m := s.newModel(issues)
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			m.Update(tea.KeyPressMsg{Code: view})
			m.SetFilter(beads.Filter{Text: "keep"})

			s.Contains(m.View(), "keep me")
			s.NotContains(m.View(), "drop me")
		})
	}
}

func (s *appTestSuite) TestNoPanicAtDegenerateSizes() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One"}})
	for _, size := range []tea.WindowSizeMsg{{Width: 0, Height: 0}, {Width: 1, Height: 1}} {
		s.NotPanics(func() {
			m.Update(size)
			_ = m.View()
		})
	}
}

func (s *appTestSuite) TestEmptySnapshotRendersAnExplanation() {
	m := s.newModel(nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	out := m.View()
	s.NotEmpty(strings.TrimSpace(out))
	// "br create", not the looser "No issues": that substring is also in
	// emptyFilter's own message ("No issues match ..."), so it cannot tell an
	// empty workspace apart from an empty filter result — it only proves the
	// body said something, not which something.
	s.Contains(out, "br create", "a blank screen is never an acceptable state")
}

// TestHelpOverlayOpensAndAnyKeyClosesRatherThanQuitting checks for the help
// overlay's own title rather than the substring "help": Task 7.1 gave the
// status bar a persistent "? help" hint that is on screen whether or not the
// overlay is open, which would satisfy a plain Contains(out, "help") either
// way and prove nothing about whether the overlay itself actually closed.
func (s *appTestSuite) TestHelpOverlayOpensAndAnyKeyClosesRatherThanQuitting() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(tea.KeyPressMsg{Code: '?'})
	s.Contains(m.View(), "bv — key bindings", "opening help must be visible")

	// "?" then "q" must close the overlay rather than quit — the routing
	// order (overlay swallows keys ahead of the global bindings) this task
	// exists to pin down.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	s.Nil(cmd, "q must close the open overlay, not quit, while help is showing")
	s.NotContains(m.View(), "bv — key bindings", "the overlay must be gone once closed")
}

func (s *appTestSuite) TestFilterKeyOpensEditingAndSwallowsAGlobalBinding() {
	m := s.newModel([]beads.Issue{
		{ID: "bv-1", Title: "One", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "Two", Status: beads.StatusOpen},
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(tea.KeyPressMsg{Code: '/'})
	s.Contains(m.View(), "filter:", "opening the filter box must be visible")

	// "2" is globally bound to switch to the tree view; while the filter box
	// is focused it must be treated as typed text instead, not routed to the
	// global bindings.
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	s.Contains(m.View(), "filter: 2", "the typed character must reach the filter buffer")
	s.Equal("bv-1", m.SelectedID(), "the key must not have switched views")
}

func (s *appTestSuite) TestQDoesNotQuitWhileTheFilterBoxIsFocused() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '/'})

	// The bug fixed upstream as #176: typing "q" into the filter box quit the
	// app instead of being entered as text, because the global quit binding
	// was checked before the in-progress filter edit. bv-f7b.6.1 made every
	// typed character schedule a debounced apply (keys.go's
	// scheduleFilterApply), so cmd is no longer nil for an ordinary
	// character; what must still never happen is cmd carrying tea.Quit's
	// signal.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})

	if cmd != nil {
		_, isQuit := cmd().(tea.QuitMsg)
		s.False(isQuit, "q must not quit while the filter box is focused")
	}
	s.Contains(m.View(), "filter: q", "q must be entered as text instead")
}

// TestCtrlCQuitsWhileTheFilterBoxIsFocused is I4's fix pinned. Unlike "q",
// ctrl+c carries no msg.Text for handleFilterKey's default branch to
// swallow it into, so it used to be silently dropped — the one key the help
// overlay advertises, one keypress away, as always quitting ("q/ctrl+c
// quit").
func (s *appTestSuite) TestCtrlCQuitsWhileTheFilterBoxIsFocused() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '/'})

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	s.NotNil(cmd, "ctrl+c must quit even while the filter box is focused")
}

// twoIssues is this suite's fixture for the live-filter tests below: two
// issues whose titles need to be distinct and known so narrowing by a single
// typed letter can be asserted precisely. "widget" rather than "beta": beads.
// Filter.matchesText is a substring match, and "beta" itself contains "a",
// which would leave TestTypingNarrowsTheViewsWithoutEnter unable to exclude
// it by typing "a" no matter how correct the debounce plumbing is.
func (s *appTestSuite) twoIssues() []beads.Issue {
	return []beads.Issue{
		{ID: "bv-1", Title: "alpha", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "widget", Status: beads.StatusOpen},
	}
}

func (s *appTestSuite) TestTypingNarrowsTheViewsWithoutEnter() {
	m := s.newModel(s.twoIssues())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

	s.Require().NotNil(cmd, "a keystroke must schedule a debounced apply")
	// Run the scheduled command for the tick it would have delivered rather
	// than sleeping: tea.Tick's own timer is not this test's concern.
	m.Update(cmd())

	out := ansi.Strip(m.View())
	s.Contains(out, "bv-1", "alpha matches")
	s.NotContains(out, "bv-2", "widget does not")
}

func (s *appTestSuite) TestAStaleTickAppliesNothing() {
	m := s.newModel(s.twoIssues())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})

	// Type "b", hold on to its tick, then type "e" — the first tick is now a
	// keystroke stale and must not narrow anything to "b" alone. staleMsg is
	// built only after "e" lands: at runtime the tick fires 150ms later, by
	// which time more characters have arrived, so calling stale() early would
	// let a fire-time read of the token (the exact bug this test exists to
	// catch) pass by accident.
	_, stale := m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	s.Require().NotNil(stale)
	_, fresh := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	s.Require().NotNil(fresh)
	staleMsg := stale()

	m.Update(staleMsg)
	s.Empty(m.FilterForTest().Text, "a stale tick must apply nothing at all")

	m.Update(fresh())
	s.Equal("be", m.FilterForTest().Text)
}

func (s *appTestSuite) TestEnterStillCommitsAndClosesTheOverlay() {
	m := s.newModel(s.twoIssues())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	s.Equal("a", m.FilterForTest().Text)
	s.NotContains(ansi.Strip(m.View()), "filter: ", "the overlay is closed")
}

func (s *appTestSuite) TestEscapeRestoresTheFilterAsItWasWhenTheOverlayOpened() {
	m := s.newModel(s.twoIssues())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Open on an already-active filter — the case a plain "clear on escape"
	// gets wrong, and the reason this is a restore rather than a reset.
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.Require().Equal("a", m.FilterForTest().Text)

	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	m.Update(cmd())
	s.Require().Equal("ab", m.FilterForTest().Text, "typing applied live")

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	s.Equal("a", m.FilterForTest().Text,
		"escape restores what was there, it does not clear")
	s.Contains(ansi.Strip(m.View()), "bv-1")
}

func (s *appTestSuite) TestATickArrivingAfterEscapeAppliesNothing() {
	m := s.newModel(s.twoIssues())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Open on an already-active filter, so the guard actually has something
	// to discriminate: closeFilterOverlay clears overlay.buffer to "" too, so
	// starting from an empty filter would leave "the guard fired" and "the
	// buffer emptied on its own" looking identical. Only a restored, non-
	// empty filter proves the token's kind check — not just its token check,
	// which this tick still satisfies — is what stopped it.
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	s.Require().Equal("a", m.FilterForTest().Text)

	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	s.Require().NotNil(cmd)
	inFlight := cmd()

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(inFlight)

	s.Equal("a", m.FilterForTest().Text,
		"a tick that outlived the overlay must not overwrite the restored filter")
}

// TestEscapeClearsANonMatchingFilterFromTheActiveView is C1's fix pinned.
// empty.go's emptyFilter message tells the user "Press esc to clear the
// filter", but that screen only shows once the filter box is already
// CLOSED (Enter applied it, or it matched nothing to begin with) — and
// handleFilterKey's own Escape case only fires while the box is open.
// Reproduced live at 100x14: typing a non-matching filter and pressing
// Escape three times over left the frame byte-identical each time, because
// esc fell through to the active view (listview has nothing bound to it).
// Global esc (handleGlobalKey) is what makes the message's promise true.
func (s *appTestSuite) TestEscapeClearsANonMatchingFilterFromTheActiveView() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "keep me", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	m.Update(tea.KeyPressMsg{Code: '/'})
	m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"}) // matches nothing
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	s.Contains(m.View(), "No issues match", "fixture assumption: the filter must not match anything")

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	s.Contains(m.View(), "keep me", "esc must clear the filter and restore the view")
}

// TestEscapeClearsTheQueryButNotHideClosed records a decision, not a bug
// report: esc discards the query the user typed — Text, Status and Labels —
// and leaves every other dimension of the filter exactly as it was.
//
// esc used to assign beads.Filter{}, a whole-struct zero. HideClosed lives in
// that struct, so a session started with --hide-closed or hide_closed: true
// silently un-hid every closed issue the first time the user pressed esc to
// drop a search, discarding a setting they had never typed. c is already
// hide-closed's own toggle and is the only key that should move it; esc is
// the only way to discard a query, and that is now the whole of what it does.
// ShowTombstones is left standing for the same reason, and because nothing in
// this MVP can turn it on to begin with.
func (s *appTestSuite) TestEscapeClearsTheQueryButNotHideClosed() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.SetFilter(beads.Filter{
		Text: "second", Status: beads.StatusInProgress, Labels: []string{"ui"}, HideClosed: true,
	})

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	s.Equal(beads.Filter{HideClosed: true}, m.FilterForTest(),
		"esc clears the query and nothing else")
	s.NotContains(ansi.Strip(tui.ActivePaneForTest(m)), "third",
		"the closed issue must stay hidden on screen, not only in the filter struct")
}

// TestDetailPaneRendersBesideOrBelowTheList pins that joinPanes actually
// shows the detail pane's own content, in both the side-by-side (>=
// splitThreshold) and stacked (below it) layouts. Nothing else in this
// suite would notice a regression that left DetailWidth at 0 in one of the
// two branches — width invariants alone hold vacuously for a pane that never
// renders at all, so this checks for the detail pane's metadata line
// specifically, which only it produces ("bv-1 · Open", the id-and-status
// join renderMeta builds; the list pane's own row format is "bv-1  Only
// issue", never a match for this substring).
func (s *appTestSuite) TestDetailPaneRendersBesideOrBelowTheList() {
	for _, width := range []int{120, 60} {
		s.Run(strconv.Itoa(width), func() {
			m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "Only issue", Status: beads.StatusOpen}})
			m.Update(tea.WindowSizeMsg{Width: width, Height: 40})

			out := m.View()
			s.Contains(out, "Only issue", "the list pane must render")
			s.Contains(out, "bv-1 · Open", "the detail pane's own metadata line must also render")
		})
	}
}

// TestJoinedFrameNeverExceedsTheTerminalWidth is the join-level counterpart
// to detail's own TestNoLineExceedsTheWidth: that test proves the pane's own
// content never overflows its allotted width, but it cannot see whether
// joinPanes hands the panes the wrong widths or glues them together with an
// extra column. A title and description long enough to need wrapping and
// truncation exercise both the side-by-side (120, splitThreshold-and-above)
// and the stacked (60, below it) layouts, plus a width narrow enough
// (20) that both panes are near or below their own placeholder thresholds.
//
// 99, 100 and 101 straddle splitThreshold specifically for the Task 7.1
// gutter fix: 99 stays stacked (no gutter), 100 and 101 cross into the
// side-by-side branch where Compute now reserves gutterWidth out of
// ListWidth's own share — this is what would catch a version that added the
// gutter back on top of the width budget instead of carving it out, which
// is exactly the "99 more column" overflow this fix exists to avoid.
func (s *appTestSuite) TestJoinedFrameNeverExceedsTheTerminalWidth() {
	issue := beads.Issue{
		ID: "bv-1", Title: strings.Repeat("wide title ", 20),
		Description: strings.Repeat("word ", 100),
	}
	for _, width := range []int{120, 101, 100, 99, 60, 20} {
		s.Run(strconv.Itoa(width), func() {
			m := s.newModel([]beads.Issue{issue})
			m.Update(tea.WindowSizeMsg{Width: width, Height: 40})

			for line := range strings.SplitSeq(m.View(), "\n") {
				s.LessOrEqual(lipgloss.Width(line), width, "line: %q", line)
			}
		})
	}
}

// TestJoinedFrameNeverExceedsTheTerminalHeight is the height-level
// counterpart to TestJoinedFrameNeverExceedsTheTerminalWidth, above.
// resize's stacked-layout split (app.go: listHeight = BodyHeight/2,
// detailHeight = BodyHeight - listHeight) had no coverage at all — deleting
// it leaves both panes claiming the full BodyHeight, so the stacked frame
// (list + detail joined vertically) comes out roughly twice the terminal's
// actual height, and nothing in this suite or detail's own would have
// noticed. 60 is the same stacked width TestJoinedFrameNeverExceedsTheWidth
// already uses; 120 is the side-by-side case, where both panes already share
// the full BodyHeight and this holds for a different, simpler reason.
func (s *appTestSuite) TestJoinedFrameNeverExceedsTheTerminalHeight() {
	issue := beads.Issue{
		ID: "bv-1", Title: strings.Repeat("wide title ", 20),
		Description: strings.Repeat("word ", 100),
	}
	sizes := []struct{ width, height int }{
		{120, 40}, // side-by-side
		{60, 40},  // stacked — the case that actually exercises the height split
		{20, 24},  // narrow on both axes
	}
	for _, size := range sizes {
		s.Run(strconv.Itoa(size.width)+"x"+strconv.Itoa(size.height), func() {
			m := s.newModel([]beads.Issue{issue})
			m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})

			lines := strings.Split(m.View(), "\n")
			s.LessOrEqual(len(lines), size.height,
				"the frame (list pane plus detail pane, stacked or not) must fit inside the terminal height")
		})
	}
}

// TestJoinedFrameNeverExceedsTheTerminalHeightWithFilterOpen is C3's fix
// pinned. View (app.go) appends the filter-edit line on top of a body
// already sized to BodyHeight = height - 1 (the status bar's own row), so
// the joined frame came out height + 1 rows the instant the filter box
// opened — probed at 5,664 of ~5,700 size combinations. Live at 80x24 this
// pushed the status bar (the counts and the active-filter indicator both)
// off the bottom of the terminal while the filter was being edited.
func (s *appTestSuite) TestJoinedFrameNeverExceedsTheTerminalHeightWithFilterOpen() {
	issue := beads.Issue{ID: "bv-1", Title: "One", Status: beads.StatusOpen}
	sizes := []struct{ width, height int }{
		{120, 40}, // side-by-side
		{80, 24},  // the size the finding's own live capture used
		{60, 40},  // stacked
		{20, 24},  // narrow on both axes
	}
	for _, size := range sizes {
		s.Run(strconv.Itoa(size.width)+"x"+strconv.Itoa(size.height), func() {
			m := s.newModel([]beads.Issue{issue})
			m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
			m.Update(tea.KeyPressMsg{Code: '/'})

			lines := strings.Split(m.View(), "\n")
			s.LessOrEqual(len(lines), size.height,
				"the frame must not exceed the terminal height while the filter overlay is open")
		})
	}
}

func (s *appTestSuite) TestTabMovesFocusAndTheFrameShowsIt() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	before := m.View()
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	after := m.View()

	s.NotEqual(before, after,
		"the frame colour is the only signal that focus moved")
	s.Equal(ansi.Strip(before), ansi.Strip(after),
		"focus changes styling only — no content moves")
}

func (s *appTestSuite) TestSwitchingViewsReturnsFocusToTheView() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // focus the detail pane

	focusedDetail := m.View()
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"}) // switch to the tree

	s.NotEqual(focusedDetail, m.View())
	// Switching views with focus stranded on the detail pane would leave the
	// new view's own cursor unreachable until the user pressed Tab again.
	//
	// The assertion has to be that the cursor actually MOVED. A closing
	// NotEmpty(SelectedID()) held whether or not the key arrived, because the
	// tree already has a row-0 selection either way — so the test passed with
	// handleViewSwitchKey mutated to leave focus on the detail pane, which is
	// precisely the bug the paragraph above describes.
	s.Require().Equal("bv-1", m.SelectedID(), "fixture assumption: the tree opens on its first row")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	s.Equal("bv-2", m.SelectedID(), "down must reach the tree, not the detail pane it was focused on")
}

func (s *appTestSuite) TestFramedPanesDrawABorder() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	frame := ansi.Strip(m.View())

	s.Contains(frame, "╭", "each pane draws a rounded frame")
	s.Contains(frame, "╯")
	for line := range strings.SplitSeq(frame, "\n") {
		s.LessOrEqual(ansi.StringWidth(line), 120,
			"the frame must not push any row past the terminal edge")
	}
}

func (s *appTestSuite) TestTerminalsTooSmallToFrameDrawNoBorder() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 9, Height: 40})

	frame := ansi.Strip(m.View())

	s.NotContains(frame, "╭",
		"a terminal this narrow must spend every column on content")
	for line := range strings.SplitSeq(frame, "\n") {
		s.LessOrEqual(ansi.StringWidth(line), 9)
	}
}

// TestPgDownScrollsTheDetailPaneRatherThanTheList pins keys.go's routing of
// pgup/pgdown to the detail pane instead of the active view. Deleting that
// routing block leaves every other test in this suite green, since
// bubbles/v2/list's own default keymap binds only left/h/b/u and
// right/l/f/d for paging, not pgup/pgdown — so an unrouted pgdown is simply
// ignored by the list rather than visibly breaking anything, which is
// exactly why this needs its own assertion rather than relying on some other
// test to notice the regression.
func (s *appTestSuite) TestPgDownScrollsTheDetailPaneRatherThanTheList() {
	issues := []beads.Issue{
		// 2000, not the original 400: M-e's chromeHeight fix (layout.go) gave
		// every pane one more line of height, and 400 words fit inside that
		// without needing to scroll at all — this must stay comfortably
		// larger than any single frame the detail pane could plausibly get.
		{ID: "bv-1", Title: "One", Status: beads.StatusOpen, Description: strings.Repeat("word ", 2000)},
		{ID: "bv-2", Title: "Two", Status: beads.StatusOpen},
	}
	m := s.newModel(issues)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	before := m.View()
	beforeSelection := m.SelectedID()

	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})

	s.NotEqual(before, m.View(),
		"pgdown must scroll the detail pane, which a description this long has room to scroll")
	s.Equal(beforeSelection, m.SelectedID(), "pgdown must not move the list's own cursor")
}

// TestLoadTreeStateSyncsTheDetailPane pins a bug found while verifying the
// tree's persistence end to end: LoadTreeState correctly restores the
// tree view's own cursor onto the saved id — a terminal capture confirmed
// the highlighted row was right — but applyFilter had already synced the
// detail pane once before LoadTreeState ever ran, against whatever row
// SetSnapshot's own default picked. Without a resync afterward, the detail
// pane stayed one issue behind the tree's own highlighted row until the
// user's first keypress, which is exactly the kind of mismatch nothing else
// in this suite exercises: every other test drives selection through
// keypresses, which already call syncDetail via handleKey.
//
// The assertion checks for "child · Open" — detail's own id-and-status join
// (renderMeta), the same distinguishing format
// TestDetailPaneRendersBesideOrBelowTheList uses below — rather than the
// child issue's title. The title alone would pass whether or not this
// worked: the tree pane always shows both rows at once (root starts
// expanded), so "Child issue" is already sitting in the tree's own listing
// regardless of which row is selected or synced to the detail pane.
func (s *appTestSuite) TestLoadTreeStateSyncsTheDetailPane() {
	beadsDir := filepath.Join(s.T().TempDir(), ".beads")
	s.T().Setenv("XDG_STATE_HOME", s.T().TempDir())

	issues := []beads.Issue{
		{ID: "root", Title: "Root issue", Status: beads.StatusOpen},
		{
			ID: "child", Title: "Child issue", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "child", DependsOnID: "root", Type: beads.DepParentChild},
			},
		},
	}
	newTreeModel := func() *tui.Model {
		m, err := tui.NewModel(tui.Deps{
			Log:      slog.New(slog.DiscardHandler),
			Cfg:      config.Config{Theme: config.ThemeDark, View: config.ViewTree},
			Theme:    theme.New(config.ThemeDark, theme.BackgroundDark),
			Snapshot: beads.NewSnapshot(issues),
			Reload:   func() tea.Msg { return nil },
		})
		s.Require().NoError(err)
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

		return m
	}

	saver := newTreeModel()
	saver.Update(tea.KeyPressMsg{Code: 'j'}) // move from root onto child
	saver.SaveTreeState(beadsDir)

	loader := newTreeModel()
	loader.LoadTreeState(beadsDir)

	s.Contains(loader.View(), "child · Open",
		"the detail pane must reflect the restored selection immediately, not only after the next keypress")
}

// hierarchicalIssues is the fixture I1's two tests below share: a root with
// one child, so the child's row being visible is a direct proxy for the root
// having rendered expanded.
func hierarchicalIssues() []beads.Issue {
	return []beads.Issue{
		{ID: "root", Title: "Root issue", Status: beads.StatusOpen},
		{
			ID: "child", Title: "Child issue", Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{
				{IssueID: "child", DependsOnID: "root", Type: beads.DepParentChild},
			},
		},
	}
}

// TestFirstRunWithNoSavedStateOpensExpanded is I1's first evidence test: a
// beadsDir nothing was ever saved for must leave Build's own depth == 0
// default alone, not collapse to a single row. Before the fix, LoadState
// folded a missing file into a zero State the same way it folds a genuinely
// all-collapsed one, and LoadTreeState called ApplyState on it
// unconditionally — proven by building a variant with only the LoadTreeState
// call removed: with it, a single collapsed root row; without it, the root
// expanded with its child visible.
//
// The assertion checks for "▾", root's own expansion marker (treeview/
// prefix.go's Prefix), rather than the child's title text. The detail pane
// beside the tree renders a "Children" section naming every child of
// whichever issue is selected — root, here, either way — regardless of
// whether the tree itself is expanded or collapsed, so a text assertion on
// "Child issue" would pass even with the LoadTreeState fix reverted; this was
// caught by deliberately reintroducing the bug and watching a first draft of
// this test stay green.
func (s *appTestSuite) TestFirstRunWithNoSavedStateOpensExpanded() {
	s.T().Setenv("XDG_STATE_HOME", s.T().TempDir())
	beadsDir := filepath.Join(s.T().TempDir(), ".beads") // nothing ever saved here.

	m := s.newTreeModel(hierarchicalIssues())
	m.LoadTreeState(beadsDir)

	s.Contains(m.View(), "▾",
		"with nothing ever saved, the tree must keep Build's own expanded-root default")
}

// TestAGenuinelyAllCollapsedSavedStateIsHonoured is I1's second evidence
// test, the other half of the found/not-found distinction: a state that was
// actually saved with nothing expanded must still be applied, not treated as
// though nothing were there.
//
// As above, this checks root's own marker directly ("▸", collapsed) rather
// than the child's title, since the detail pane's "Children" section would
// otherwise leak "Child issue" into the view regardless of the tree's own
// expand state.
func (s *appTestSuite) TestAGenuinelyAllCollapsedSavedStateIsHonoured() {
	s.T().Setenv("XDG_STATE_HOME", s.T().TempDir())
	beadsDir := filepath.Join(s.T().TempDir(), ".beads")
	s.Require().NoError(treeview.State{Selected: "root"}.Save(beadsDir))

	m := s.newTreeModel(hierarchicalIssues())
	m.LoadTreeState(beadsDir)

	out := m.View()
	s.Contains(out, "▸", "a genuinely all-collapsed saved state must be honoured")
	s.NotContains(out, "▾", "no node should be left expanded once an all-collapsed state is applied")
}

// TestSaveTreeStateLogsAFailure is I7's fix pinned, and the only one of the
// three swallows that call named as "one line each" is actually reachable
// from a test: applyBackground's and detail's rebuildRenderer's own failures
// are both documented as unreachable in practice, since theme.New only ever
// produces a glamour style that already succeeded once. SaveTreeState's own
// comment already says failures never stop the exit — that stays true here,
// nothing panics or returns an error — but a full session with BV_LOG set
// used to produce a 0-byte file (Model.log had zero readers before this
// task) even when a save like this one silently failed. os.MkdirAll fails
// because a plain file sits where the "bv" state directory component needs
// to be — chosen over touching real file permissions, which behave
// differently across platforms.
func (s *appTestSuite) TestSaveTreeStateLogsAFailure() {
	dir := s.T().TempDir()
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "bv"), []byte("x"), 0o600))
	s.T().Setenv("XDG_STATE_HOME", dir)

	var buf bytes.Buffer
	m := s.newModelWithLog(slog.New(slog.NewTextHandler(&buf, nil)), nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.SaveTreeState(filepath.Join(dir, ".beads"))

	s.Contains(buf.String(), "save tree state", "the swallowed failure must still reach the log")
}

// TestTreeViewWiringRendersATreeSpecificArtefact closes M2: the previous
// implementation report claimed TestFilterAppliesToEveryView was "genuinely
// strengthened" by this task's views[1] wiring, but replacing
// treeview.Model.View() with `return ""` leaves the entire internal/tui suite
// green, because that test's shared assertion (Contains(m.View(), "keep
// me")) is satisfied by the detail pane's own rendering of the selected
// issue's title regardless of what the tree itself emits. This drives a real
// parent/child fixture through the tree view and asserts a glyph only
// treeview/prefix.go's Prefix can produce — an expansion marker a blank pane
// could never satisfy.
func (s *appTestSuite) TestTreeViewWiringRendersATreeSpecificArtefact() {
	m := s.newTreeModel(hierarchicalIssues())

	s.Contains(m.View(), "▾", "the tree view must render its own expansion marker, not a blank pane")
}

// TestBoardViewWiringRendersABoardSpecificArtefact is F4, the same M2 gap as
// TestTreeViewWiringRendersATreeSpecificArtefact above but for views[2]:
// replacing boardview.Model.View() with `return ""` leaves `go test
// ./internal/tui/` green, because TestFilterAppliesToEveryView's '3'
// subtest is satisfied by the detail pane's own rendering of the selected
// issue's title, not anything the board itself drew. A card's bordered box
// ("┌", lipgloss.NormalBorder in boardview/card.go) is drawn nowhere else
// in this package tree, so a blank board pane cannot produce it.
func (s *appTestSuite) TestBoardViewWiringRendersABoardSpecificArtefact() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '3'}) // switch to board

	s.Contains(m.View(), "┌", "the board view must render its own card border, not a blank pane")
}

// TestBoardDrawsNoDetailPaneAndNoSecondFrame pins that the board, once
// active, gets the whole body: one frame per row, and no line wider than the
// terminal — not a two-pane split with the detail pane's own border folded
// in beside it.
func (s *appTestSuite) TestBoardDrawsNoDetailPaneAndNoSecondFrame() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})

	for line := range strings.SplitSeq(ansi.Strip(m.View()), "\n") {
		s.LessOrEqual(ansi.StringWidth(line), 120)
		s.LessOrEqual(strings.Count(line, "╭"), 1,
			"a full-screen board has one frame, not two")
	}
}

// TestTabDoesNothingOnTheBoard pins that Tab, which toggles focus between the
// active view and the detail pane, is a no-op once the board has no detail
// pane to focus.
func (s *appTestSuite) TestTabDoesNothingOnTheBoard() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})

	before := m.View()
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	// There is no detail pane to focus, and s — not tab — cycles swimlanes.
	s.Equal(before, m.View())
}

// TestBoardLaneOnlyShowsOnTheBoard is I5's Model-level guard for
// boardLaneName's active-view check. status_test.go's
// TestNeverShowsTheLaneOutsideTheBoard calls renderStatus directly with
// Lane already forced to "", which cannot catch a dropped
// `|| m.active != boardSlot` guard inside boardLaneName itself — only a
// Model actually switched to the board, then away from it, can.
//
// This drives the lane change with "s" rather than asserting the default
// lane's own name: bv-f7b.2.1 moved tab to the app-level focus key, which
// briefly left this test unable to reach the board's own CycleSwimLane at
// all, but bv-f7b.2.3 rebinds that to "s" — a key the global bindings do not
// claim, so it still reaches the active view via handleKey's fallthrough.
// Cycling to "Priority" and back to nothing is strictly stronger than
// checking the unchanged default: it also proves "s" actually routes through
// Model.Update down to the board, not just that boardLaneName's active-view
// guard reads Lane correctly.
func (s *appTestSuite) TestBoardLaneOnlyShowsOnTheBoard() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(tea.KeyPressMsg{Code: '3'})            // switch to board
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"}) // cycle swimlane: Status -> Priority
	s.Contains(m.View(), "Priority", "the swimlane indicator must show while the board is active")

	m.Update(tea.KeyPressMsg{Code: '1'}) // switch to list
	s.NotContains(m.View(), "Priority", "and disappear once another view is active")
}

// TestHiddenCountShowsOnEveryView replaces TestTreeHiddenCountOnlyShowsOn-
// TheTree, which pinned the opposite: the count used to come from the tree's
// own local toggle, so it read 0 on the list and the board no matter what was
// hidden. Hide-closed is one filter shared by all three views now, so the
// indicator has to follow it everywhere — a count that vanished on a view
// switch would now be reporting the wrong thing, not scoping itself.
//
// The two tombstones in the fixture are what keep the count honest. They are
// terminal, so beads.Counts folds them into Closed, but they are hidden with
// the toggle off as well — a status bar reading Counts.Closed straight off
// says "3 hidden" here when pressing 'c' hid exactly one issue.
func (s *appTestSuite) TestHiddenCountShowsOnEveryView() {
	issues := []beads.Issue{
		{ID: "bv-1", Title: "open one", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "closed one", Status: beads.StatusClosed},
		{ID: "bv-3", Title: "deleted one", Status: beads.StatusTombstone},
		{ID: "bv-4", Title: "deleted two", Status: beads.StatusTombstone},
	}
	m := s.newModel(issues)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	s.NotContains(m.View(), "hidden", "nothing is hidden until 'c' is pressed")

	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})

	for _, viewKey := range []rune{'1', '2', '3'} {
		s.Run(string(viewKey), func() {
			m.Update(tea.KeyPressMsg{Code: viewKey, Text: string(viewKey)})
			s.Contains(ansi.Strip(m.View()), "1 hidden",
				"the shared filter's hidden count must show on every view")
		})
	}
}

// closedParentIssues is the case the promotion changes: a closed epic with
// an open child. Under the tree's old local prune the epic stayed as
// scaffolding; under a global filter it is gone and the child re-roots.
func closedParentIssues() []beads.Issue {
	return []beads.Issue{
		{ID: "bv-1", Title: "closed epic", IssueType: beads.TypeEpic, Status: beads.StatusClosed},
		{
			ID: "bv-2", Title: "open child", IssueType: beads.TypeTask, Status: beads.StatusOpen,
			Dependencies: []beads.Dependency{{DependsOnID: "bv-1", Type: beads.DepParentChild}},
		},
	}
}

// TestHideClosedNarrowsEveryView asserts on the active pane rather than the
// whole frame (ActivePaneForTest, export_test.go): the detail pane beside it
// is handed the unfiltered snapshot on purpose, so it still names bv-1 as
// bv-2's parent once the filter has removed it — deliberate, and not what
// this test is about.
func (s *appTestSuite) TestHideClosedNarrowsEveryView() {
	m := s.newModel(closedParentIssues())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	for _, viewKey := range []rune{'1', '2', '3'} {
		s.Run(string(viewKey), func() {
			m.Update(tea.KeyPressMsg{Code: viewKey, Text: string(viewKey)})
			s.Contains(ansi.Strip(tui.ActivePaneForTest(m)), "bv-1")
		})
	}

	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})

	for _, viewKey := range []rune{'1', '2', '3'} {
		s.Run("hidden/"+string(viewKey), func() {
			m.Update(tea.KeyPressMsg{Code: viewKey, Text: string(viewKey)})
			out := ansi.Strip(tui.ActivePaneForTest(m))
			s.NotContains(out, "bv-1", "the closed epic must be gone from every view")
			s.Contains(out, "bv-2", "its open child must survive, re-rooted")
		})
	}
}

func (s *appTestSuite) TestHideClosedTogglesBackOn() {
	m := s.newModel(closedParentIssues())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})

	// The pane, not the frame, for the reason the sibling test above gives:
	// the detail pane names bv-1 either way, so a frame-level assertion here
	// would pass with the second press deleted.
	s.Contains(ansi.Strip(tui.ActivePaneForTest(m)), "bv-1")
}

func (s *appTestSuite) TestHideClosedShowsOnTheStatusBar() {
	m := s.newModel(closedParentIssues())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})

	// Filter.Describe already renders this — it has since before the
	// promotion, and its comment says the wording matches the key's label.
	s.Contains(ansi.Strip(m.View()), "hide closed")
}

// TestGutterSeparatesTruncatedTitleFromDetailPane is I6's guard: the gutter
// column joinPanes renders between the list and detail panes (layout.go's
// gutter) had no assertion of its own, only a width-budget test proving the
// joined frame does not overflow — which holds equally well for an empty
// column or one that renders something else entirely. A title long enough
// to truncate makes the list row extend right up to the pane boundary, so
// the character immediately after it (at column ListWidth) is what proves
// the gutter is actually blank rather than glued to the detail pane's own
// first character.
//
// This now targets a short terminal (bodyHeight below minBorderedBodyHeight)
// rather than a tall one: at 120x40 both panes are framed (Task 1.1), and a
// framed layout has no gutter at all — separatorWidth returns 0 and the two
// borders separate the panes instead. 120x6 keeps the same side-by-side,
// wide-enough-to-truncate shape this test was written for while staying
// below the height floor Layout.Bordered checks, so the gutter fallback this
// test guards is still the path actually exercised.
func (s *appTestSuite) TestGutterSeparatesTruncatedTitleFromDetailPane() {
	m := s.newModel([]beads.Issue{{
		ID: "bv-1", Title: strings.Repeat("wide title ", 20), Status: beads.StatusOpen,
	}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 6})

	layout := tui.Compute(120, 6, true)
	s.Require().False(layout.Bordered, "fixture assumption: this size must fall back to the gutter")

	var row string
	for line := range strings.SplitSeq(ansi.Strip(m.View()), "\n") {
		if strings.Contains(line, "…") {
			row = line

			break
		}
	}
	s.Require().NotEmpty(row, "fixture assumption: the title must actually truncate")

	runes := []rune(row)
	s.Require().Greater(len(runes), layout.ListWidth, "the row must reach into the gutter column")
	s.Equal(' ', runes[layout.ListWidth],
		"the gutter column must be blank, not glued to the detail pane")
}

// TestFrameSeparatesTitleFromDetailPaneWhenBordered is this suite's sibling
// assertion for the bordered case: at 120x40 there is no gutter column (the
// test above's own comment explains why), so what actually keeps a truncated
// title from touching the detail pane's own text is the list pane's own
// right-hand border character, immediately followed by the detail pane's
// left-hand border character.
func (s *appTestSuite) TestFrameSeparatesTitleFromDetailPaneWhenBordered() {
	m := s.newModel([]beads.Issue{{
		ID: "bv-1", Title: strings.Repeat("wide title ", 20), Status: beads.StatusOpen,
	}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	layout := tui.Compute(120, 40, true)
	s.Require().True(layout.Bordered, "fixture assumption: this size must be framed")

	var row string
	for line := range strings.SplitSeq(ansi.Strip(m.View()), "\n") {
		if strings.Contains(line, "…") {
			row = line

			break
		}
	}
	s.Require().NotEmpty(row, "fixture assumption: the title must actually truncate")

	runes := []rune(row)
	// The row starts with the list pane's own left border, which shifts its
	// content one column past ListWidth; its right border follows content at
	// ListWidth+1, and the detail pane's left border immediately after that.
	s.Require().Greater(len(runes), layout.ListWidth+2, "the row must reach into the frame")
	s.Equal('│', runes[layout.ListWidth+1], "the list pane's own right border must follow its content")
	s.Equal('│', runes[layout.ListWidth+2], "the detail pane's own left border must follow immediately")
}

// TestOverlaySwallowsKeys pins the routing handleKey documents: any key
// closes the help overlay, including the one that would otherwise quit.
func (s *appTestSuite) TestOverlaySwallowsKeys() {
	// ? then q must close the overlay, not quit the program. The equivalent
	// defect for the filter box — typing q quitting mid-search — was a real
	// upstream regression (#176).
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.Update(tea.KeyPressMsg{Code: '?'})
	s.Contains(m.View(), "BV_THEME", "the overlay is open")

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	s.Nil(cmd, "q must not quit while an overlay is open")
	s.NotContains(m.View(), "BV_THEME", "it closes the overlay instead")
}

// TestFilterBoxSwallowsPrintableKeys is the same routing guarantee for the
// filter overlay's own edit buffer.
func (s *appTestSuite) TestFilterBoxSwallowsPrintableKeys() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "quick", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.Update(tea.KeyPressMsg{Code: '/'})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	s.Nil(cmd, "typing q into the filter box must not quit")
}

// TestUnknownBackgroundIsNotSilentlyDark pins the SSH/tmux case: no
// tea.BackgroundColorMsg ever arrives, and the help overlay must say so
// rather than rendering as if a background had actually been confirmed.
func (s *appTestSuite) TestUnknownBackgroundIsNotSilentlyDark() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One"}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// No BackgroundColorMsg ever arrives — the SSH and tmux case.
	m.Update(tea.KeyPressMsg{Code: '?'})

	s.Contains(m.View(), "not detected",
		"the guess must be visible so BV_THEME is discoverable")
}

// TestKnownBackgroundIsNotFlaggedAsUnknown is the mutation guard for the test
// above: it would also pass if helpOverlay printed the "not detected" note
// unconditionally, which is not what BackgroundUnknown being a distinct state
// is for. Once a real BackgroundColorMsg arrives, the note must disappear.
func (s *appTestSuite) TestKnownBackgroundIsNotFlaggedAsUnknown() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One"}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	m.Update(tea.KeyPressMsg{Code: '?'})

	s.NotContains(m.View(), "not detected",
		"a confirmed background must not still read as undetected")
}

// TestUnknownBackgroundDescribesTheAgnosticPaletteAccurately is C1's fix
// pinned: the footer used to claim bv was "assuming dark" whenever
// detection failed, which is the exact bug this rewrite exists to avoid —
// theme.Resolve maps auto + BackgroundUnknown to SchemeAgnostic, never
// SchemeDark (theme.go) — and it named only BV_THEME=light as the escape
// hatch, when the agnostic palette assumes neither background and dark is
// just as relevant an override. TestUnknownBackgroundIsNotSilentlyDark above
// only checks for the substring "not detected", which the old, wrong wording
// also contained; mutating the rest of the string to something else entirely
// left it, and every other test in this suite, green.
func (s *appTestSuite) TestUnknownBackgroundDescribesTheAgnosticPaletteAccurately() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One"}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(tea.KeyPressMsg{Code: '?'})

	out := m.View()
	s.NotContains(out, "assuming dark", "detection failing must never be described as assuming dark")
	s.Contains(out, "background-agnostic", "the note must name the palette bv actually falls back to")
	s.Contains(out, "BV_THEME=light")
	s.Contains(out, "BV_THEME=dark", "both overrides are equally relevant under an agnostic palette")
}

// TestHelpOverlayFitsAt80x24WithTheBackgroundNote is this review's C2 fix
// pinned. backgroundUnknownNote's first line alone is 94 cells — reached
// renderHelpBody unwrapped, it overflowed every terminal narrower than
// that, including 80x24: the size a brand-new user's terminal most commonly
// is, and '?' the first key they are likely to press. help_test.go's own
// TestFitsIn80x24 could not have caught this: it exercises renderHelp
// directly, which never carries the note at all — only Model.helpOverlay
// (app.go) adds it, and only once background detection has failed (see
// TestUnknownBackgroundIsNotSilentlyDark above, on which this borrows the
// same no-BackgroundColorMsg setup).
func (s *appTestSuite) TestHelpOverlayFitsAt80x24WithTheBackgroundNote() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyPressMsg{Code: '?'})

	out := m.View()
	s.Contains(out, "not detected", "fixture assumption: the background note must actually be showing")
	for line := range strings.SplitSeq(out, "\n") {
		s.LessOrEqual(lipgloss.Width(line), 80, "line: %q", line)
	}
}

// TestHelpOverlayDocumentsYank guards against helpGroups (help.go) losing
// the Yank binding silently. help_test.go's own TestDocumentsEveryBoundKey
// cannot catch that: it checks the overlay's text against
// DefaultKeyMap().All(), but All() (export_test.go) is itself derived by
// flattening helpGroups, so removing Yank from helpGroups also removes it
// from All() and that check keeps passing — confirmed by deleting keys.Yank
// from helpGroups's "Global" line and watching TestDocumentsEveryBoundKey
// stay green. This checks the overlay's rendered text against Yank's own
// label directly, independently of helpGroups, so it does not share that
// blind spot.
func (s *appTestSuite) TestHelpOverlayDocumentsYank() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(tea.KeyPressMsg{Code: '?'})

	s.Contains(m.View(), "yank", "the help overlay must document the yank binding")
}

// TestYankIssuesAClipboardCommand pins that "y" both produces the OSC52
// clipboard command carrying the selected id and shows a transient
// confirmation — OSC52 gives no acknowledgement of its own, so the message
// is the only proof, from inside the model, that anything happened.
//
// The confirmation is checked as the literal phrase "copied bv-124", not
// merely Contains(out, "bv-124"): the selected issue's own id is already on
// screen, in the list row and the detail pane's metadata line, whether or
// not yank ever sets a status message — confirmed by dropping the
// setStatus call from yank entirely and watching a first draft of this
// test, checking only "bv-124", stay green.
func (s *appTestSuite) TestYankIssuesAClipboardCommand() {
	m := s.newModel([]beads.Issue{{ID: "bv-124", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y'})
	s.Require().NotNil(cmd, "y must produce a clipboard command")

	// tea.SetClipboard returns a Cmd producing the OSC52 message; running it
	// is how we assert the id, not the escape sequence, is what gets copied.
	msg := cmd()
	s.NotNil(msg)
	s.Contains(m.View(), "copied bv-124", "a transient confirmation is required — OSC52 is silent")
}

// TestYankWithNothingSelectedIsANoOp goes beyond the brief's own NotPanics
// check — a test that only proves no panic would also pass if yank silently
// copied "" or set a confirmation for nothing selected, which is exactly the
// defect class this task's own notes warn about. cmd and the view's text are
// both checked so a regression that started copying an empty string, or
// showing "copied " with nothing after it, would fail here too.
func (s *appTestSuite) TestYankWithNothingSelectedIsANoOp() {
	m := s.newModel(nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	var cmd tea.Cmd
	s.NotPanics(func() { _, cmd = m.Update(tea.KeyPressMsg{Code: 'y'}) })
	s.Nil(cmd, "nothing selected must not produce a clipboard command")
	s.NotContains(m.View(), "copied", "nothing selected must not set a copy confirmation either")
}

// TestTokenGuardOnlyClearsAMatchingGenerationTimer is Important 4's guard
// for clearMessageAfter's generation counter (keys.go's clearStatusMsg,
// status.go's setStatus): the mechanism exists specifically so a stale timer
// from an earlier message — yank's own 3-second "copied bv-1" clear, here —
// cannot blank out a newer, unrelated message that landed before the timer
// fired. Before this test, nothing exercised it: replacing the `if
// msg.token == m.status.token` guard in Update's clearStatusMsg case
// (app.go) with an unconditional clear left every other test in this
// package, and every other package, green — nothing else waits out
// clearMessageAfter's real delay, and NewClearStatusMsgForTest/
// StatusTokenForTest (export_test.go) are what let this test deliver the
// timer message and read the generation by hand instead.
//
// Both halves matter: the first half alone would also pass for a guard that
// was `true` unconditionally — commented out entirely — never actually
// clearing anything, since nothing would ever assert the message eventually
// disappears; the second half proves a matching-generation timer still
// does its job.
func (s *appTestSuite) TestTokenGuardOnlyClearsAMatchingGenerationTimer() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.Update(tea.KeyPressMsg{Code: 'y'}) // yank sets "copied bv-1" and schedules its own clear.
	staleToken := m.StatusTokenForTest()

	// A newer message lands — a reload error, say — before the stale yank
	// timer ever fires. This bumps the generation past staleToken.
	m.Update(tui.SnapshotMsg{Err: errors.New("newer reload error")})

	m.Update(tui.NewClearStatusMsgForTest(staleToken))
	s.Contains(m.View(), "newer reload error",
		"a stale timer must not clear a newer message that arrived after it was scheduled")

	// A timer that DOES match the message currently showing must still work —
	// the guard's other half, not exercised by the assertion above.
	m.Update(tui.NewClearStatusMsgForTest(m.StatusTokenForTest()))
	s.NotContains(m.View(), "newer reload error",
		"a timer matching the current generation must still clear the message")
}

// TestEmptyFilterResultIsDistinguishedFromEmptyWorkspace pins the
// consolidated empty-state wording end to end, through Model.View rather
// than renderEmpty directly: a workspace that has issues, narrowed by a
// filter to none of them, must not suggest `br create` — that hint is for an
// empty workspace, a different problem the user cannot fix by pressing esc.
//
// "no-such-thing" alone is not enough to prove body's own emptyFilter branch
// ran: the status line already renders the active filter's own Describe()
// every frame (statusLine, app.go), so that substring would still appear
// even with the emptyFilter case deleted from body() entirely and the list
// pane falling back to bubbles' own "No item." placeholder — confirmed by
// deleting that case and watching a first draft of this test, without the
// "esc" assertion below, stay green. "esc" comes only from emptyFilter's own
// message and nowhere else on this frame, so it is what actually catches
// that mutation.
func (s *appTestSuite) TestEmptyFilterResultIsDistinguishedFromEmptyWorkspace() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "something", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetFilter(beads.Filter{Text: "no-such-thing"})

	out := m.View()
	s.Contains(out, "no-such-thing")
	s.Contains(out, "esc", "the empty-filter message itself, not just the status line, must render")
	s.NotContains(out, "br create",
		"the workspace is not empty — only the filter is too narrow")
}

// TestEmptyLoadErrorIsDistinguishedFromEmptyWorkspace is the third leg of
// the same guard: a reload that fails before anything was ever loaded
// successfully leaves the snapshot empty exactly like a genuinely empty
// workspace would, but it is not the same problem — br create would tell the
// user to do something that already happened; the actual fix is elsewhere,
// in whatever produced the decode error the status line already shows.
func (s *appTestSuite) TestEmptyLoadErrorIsDistinguishedFromEmptyWorkspace() {
	m := s.newModel(nil) // nothing ever loaded — an empty workspace, so far.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	s.Contains(m.View(), "br create", "fixture assumption: this starts out reading as an empty workspace")

	m.Update(tui.SnapshotMsg{Err: errors.New("malformed jsonl: line 7")})
	out := m.View()

	s.Contains(out, "line 7", "the decode error must still reach the screen")
	s.Contains(out, "Nothing has loaded yet")
	s.NotContains(out, "br create",
		"a failed reload is not the same problem as an empty workspace, even though both show 0 issues")
}

// TestBackgroundNoteJoinsTheCentredOverlay is M-d's fix pinned: the note
// used to be appended after renderHelp had already centred its own body via
// lipgloss.Place, which left it sitting flush left at column 0, several
// blank rows below the centred block, on any terminal wide enough to leave
// margin around the overlay. It must instead render on the same line range
// as the centred body's own left margin — checked here by requiring the
// note's own first line to start with at least one leading space at a width
// much wider than the overlay's own content, which only holds if it was
// centred together with the rest rather than placed on its own afterward.
func (s *appTestSuite) TestBackgroundNoteJoinsTheCentredOverlay() {
	m := s.newModel([]beads.Issue{{ID: "bv-1", Title: "One"}})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '?'})

	var noteLine string
	for line := range strings.SplitSeq(ansi.Strip(m.View()), "\n") {
		if strings.Contains(line, "not detected") {
			noteLine = line

			break
		}
	}
	s.Require().NotEmpty(noteLine, "fixture assumption: the note must actually render")
	s.NotEqual(strings.TrimLeft(noteLine, " "), noteLine,
		"the note must be centred alongside the rest of the overlay, not flush against column 0")
}

// TestFocusedDetailPaneTakesTheNavigationKeys pins keys.go's handleKey: once
// Tab has moved focus onto the detail pane, every key the global bindings did
// not consume — j included, not just pgup/pgdown — goes to the detail pane's
// own viewport rather than the active view.
func (s *appTestSuite) TestFocusedDetailPaneTakesTheNavigationKeys() {
	m := s.newModel([]beads.Issue{longIssue(), {ID: "bv-2", Title: "second", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	selected := m.SelectedID()
	before := ansi.Strip(m.View())
	for range 5 {
		m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}

	s.Equal(selected, m.SelectedID(),
		"the view's cursor must not move while the detail pane holds focus")
	s.NotEqual(before, ansi.Strip(m.View()), "the detail pane must have scrolled")
}

// TestUnfocusedDetailPaneLeavesTheKeysToTheView is the mirror of
// TestFocusedDetailPaneTakesTheNavigationKeys: with focus left on the view
// (the zero value), j still moves the view's own cursor rather than
// scrolling a detail pane the user has not focused.
func (s *appTestSuite) TestUnfocusedDetailPaneLeavesTheKeysToTheView() {
	m := s.newModel([]beads.Issue{longIssue(), {ID: "bv-2", Title: "second", Status: beads.StatusOpen}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	s.Equal("bv-1", m.SelectedID())
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})

	s.Equal("bv-2", m.SelectedID(),
		"with the view focused, j moves the view's own cursor")
}

// TestScrollKeysReachTheDetailPaneRegardlessOfFocus pins the other half of
// handleKey's rule: pgup/pgdown are bound to the detail pane specifically
// (see ScrollUp/ScrollDown's comment in DefaultKeyMap), so they must keep
// reaching it even while focus sits on the view — unlike j, which only
// reaches the detail pane once focus has moved there.
func (s *appTestSuite) TestScrollKeysReachTheDetailPaneRegardlessOfFocus() {
	m := s.newModel([]beads.Issue{longIssue()})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// View focused — pgdown is bound to the detail pane specifically.
	before := ansi.Strip(m.View())
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})

	s.NotEqual(before, ansi.Strip(m.View()))
}

// TestEnterOnACardOpensItInTheList pins keys.go's openSelectedInList: the
// full-screen board has no detail pane, so Enter is how a card reaches one.
func (s *appTestSuite) TestEnterOnACardOpensItInTheList() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})

	// Move off the first card so the assertion cannot pass by coincidence —
	// the list's own default selection is its first row.
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	want := m.SelectedID()
	s.Require().NotEmpty(want)

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	s.Equal(config.ViewList, tui.ViewKindForTest(m),
		"Enter must land on the list, not merely select on the board")
	s.Equal(want, m.SelectedID())
}

// TestEnterOnAnEmptyColumnDoesNothing pins that Enter is a silent no-op when
// the board's cursor sits on a column with no issue under it.
//
// Walking there with 'l' is no longer possible (bv-llf.2.1: MoveLeft/
// MoveRight skip empty columns), so this reaches the same state the way
// clamp still allows: a regrouping that empties the column the cursor
// already sits on. Hiding closed issues does exactly that — bv-2 is the only
// issue in the Closed column, so filtering it out leaves the cursor on a
// column with nothing under it, which is what clamp resolves to rather than
// picking a new one — see boardview's clamp.
func (s *appTestSuite) TestEnterOnAnEmptyColumnDoesNothing() {
	m := s.newModel([]beads.Issue{
		{ID: "bv-1", Title: "open", Status: beads.StatusOpen},
		{ID: "bv-2", Title: "closed", Status: beads.StatusClosed},
	})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})

	m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}) // skips the empty columns between Open and Closed
	s.Require().Equal("bv-2", m.SelectedID(), "fixture assumption: cursor lands directly on Closed")

	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"}) // hide closed: regroups with bv-2 filtered out
	s.Require().Empty(m.SelectedID(), "fixture assumption: cursor is on an empty column")

	before := m.View()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	s.Equal(before, m.View())
}

// TestEnterOutsideTheBoardChangesNothing pins that Enter is scoped to the
// board: pressed from the list or tree view it does nothing.
//
// The cursor is moved off row 0 first, and that is the whole of what makes
// this load-bearing. Pressed from row 0, Enter with openSelectedInList's
// board guard deleted still lands on row 0 — the board's own cursor starts
// there too — so the frame comes back identical and the test passed with the
// guard removed. From row 1 the two disagree, and a fall-through drags the
// list back to bv-1.
func (s *appTestSuite) TestEnterOutsideTheBoardChangesNothing() {
	m := s.newModel(s.sample())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	s.Require().Equal("bv-2", m.SelectedID(), "fixture assumption: the cursor is off the board's own row 0")

	before := m.View()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	s.Equal(before, m.View())
	s.Equal("bv-2", m.SelectedID(), "enter outside the board must not move the list's cursor")
}
