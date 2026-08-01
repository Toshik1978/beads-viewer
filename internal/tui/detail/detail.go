// Package detail is bv's right-hand (or lower) pane: everything about the
// selected issue, with its prose fields rendered as markdown via glamour.
package detail

import (
	"fmt"
	"log/slog"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

const (
	// defaultWrap seeds the renderer New builds before the first SetSize call
	// supplies the pane's real width.
	defaultWrap = 80

	// minWrap floors the width handed to glamour.WithWordWrap. Measured
	// against glamour v2.0.1: WithWordWrap wraps correctly for n >= 5 and
	// only degenerates to one unwrapped line at n <= 4 — WithWordWrap(4),
	// (3), (1), (0) and (-1) all emit their paragraph unwrapped, with no
	// error and no signal. 5 is therefore the floor that actually protects
	// anything; a higher floor (20 was tried first) forces glamour to wrap
	// wider than panes narrower than that floor actually are, which does the
	// same damage from the other direction — measured: at paneWidth 16 a
	// floor of 20 lost 15 of 60 words to truncateLines' backstop, and at 13
	// and 12 it lost 30 of 60. Compute legitimately returns a DetailWidth
	// below this during startup and mid-resize, so the floor alone is not
	// sufficient either way; truncateLines in render.go is the other half of
	// the defence.
	minWrap = 5

	// minPaneWidth is the narrowest width markdown is worth attempting at.
	// Below it there is no room left for a single wrapped word once the
	// glyphs and indentation every section adds are accounted for, so the
	// pane states its own narrowness instead of rendering an illegible
	// fragment.
	minPaneWidth = 12

	// noIssuePlaceholder is shown before anything is selected.
	noIssuePlaceholder = "Select an issue to see its details."

	// narrowPanePlaceholder is shown below minPaneWidth.
	narrowPanePlaceholder = "pane too narrow"

	// danglingBlockerNote explains the case where IsBlocked is true but
	// Blockers is empty: the blocking id is not present in this workspace, so
	// there is no issue to name. Rendering nothing here would read as a bug —
	// the issue is plainly blocked and the pane would say nothing about why.
	danglingBlockerNote = "blocked by an issue not present in this workspace"
)

// markdownRenderer is the fallback seam behind *glamour.TermRenderer. A test
// cannot make glamour itself fail — v2.0.1 does not error on malformed
// markdown, it degrades instead — so this interface lets an internal test
// inject a renderer that always does, which is the only way to reach
// renderOrRaw's fallback branch.
type markdownRenderer interface {
	Render(src string) (string, error)
}

// Model is the detail pane.
//
// It deliberately does NOT satisfy tui.View, despite sharing most of that
// shape. An earlier version of this comment claimed it did, which was false:
// the pane takes SetIssue rather than SetSnapshot, and has no Selected — it
// displays whatever the active view has selected instead of owning a cursor
// of its own. That is the whole distinction. tui.View is the contract for the
// three interchangeable panes the user switches between with the view keys;
// the detail pane sits beside whichever of them is active, so it is a field on
// the root model rather than one of its views.
type Model struct {
	theme    theme.Theme
	log      *slog.Logger
	renderer markdownRenderer
	viewport viewport.Model
	wrap     int
	width    int
	issue    *beads.Issue
	snapshot *beads.Snapshot
}

// New builds the pane and its markdown renderer.
//
// The renderer is constructed once and kept, and rebuilt only on resize,
// where word wrap has to change. This is not a performance requirement:
// measured against glamour v2.0.1, building one — parsing the stylesheet and
// assembling Chroma's lexer set — costs about 24 microseconds, far too fast
// to be "visible while scrolling" the way an earlier version of this comment
// claimed. The real reason is that a per-frame rebuild would be pure wasted
// work for no benefit over reusing the one built here, and "rebuilds on
// resize, nothing else" is a simpler invariant to reason about than
// "rebuilds sometimes, for reasons that vary".
func New(log *slog.Logger, th theme.Theme) (*Model, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(th.GlamourStyle()),
		glamour.WithWordWrap(defaultWrap),
	)
	if err != nil {
		return nil, fmt.Errorf("create markdown renderer: %w", err)
	}

	return &Model{
		theme:    th,
		log:      log,
		renderer: renderer,
		viewport: viewport.New(),
		wrap:     defaultWrap,
	}, nil
}

// SetIssue selects the issue this pane describes, or nil to show the
// placeholder before anything is selected.
//
// A no-op call — the same id and the same snapshot as last time — skips
// rebuilding content entirely. That matters because the root model calls
// this after every keypress to keep the pane in sync with the active view's
// cursor, and most keypresses do not move it; without the guard, every "?"
// or filter keystroke would re-run markdown rendering for no reason. The
// scroll position resets to the top only when the id itself changes, so a
// reload or filter re-apply that lands on the same issue does not yank the
// viewport back to the top of content the user is already reading.
//
// issue may be nil — that is the "nothing selected yet" state and is
// handled explicitly, above. snap may not: internal/beads gives *Snapshot no
// nil-receiver guarantee anywhere, so a nil snapshot alongside a non-nil
// issue is a caller contract violation, not a case this pane defends
// against, and composeContent's dependency rendering will panic on it. The
// asymmetry is deliberate, not an oversight.
func (m *Model) SetIssue(issue *beads.Issue, snap *beads.Snapshot) {
	sameIssue := issueID(m.issue) == issueID(issue)
	if sameIssue && m.snapshot == snap {
		return
	}

	m.issue = issue
	m.snapshot = snap
	m.refreshContent()
	if !sameIssue {
		m.viewport.GotoTop()
	}
}

// SetSize resizes the pane and, when the pane's width crosses the word-wrap
// floor, rebuilds the renderer for the new width.
func (m *Model) SetSize(width, height int) {
	width = max(width, 0)
	height = max(height, 0)

	m.width = width
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)

	if wrap := max(width, minWrap); wrap != m.wrap {
		m.rebuildRenderer(wrap)
	}
	m.refreshContent()
}

// Update handles scrolling only; the pane has no other interaction.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)

	return cmd
}

// View renders the pane, or a terse placeholder below minPaneWidth rather
// than an illegible fragment of wrapped markdown.
func (m *Model) View() string {
	if m.width < minPaneWidth {
		return ansi.Truncate(narrowPanePlaceholder, m.width, "")
	}

	return m.viewport.View()
}

// refreshContent rebuilds the viewport's content from the current issue and
// pane width.
//
// It is called from SetIssue and SetSize rather than from View, so scrolling
// within one issue re-renders nothing: View only has to read back what the
// viewport already holds — the same "build once, reuse until something
// actually changed" shape New documents for the renderer itself. Below
// minPaneWidth it does nothing — View never reads the viewport's content at
// that width, and skipping the composition (which includes every
// glamour.Render call) means a pane too narrow to show markdown does not pay
// to produce it either.
//
// The truncateLines call below is this pane's backstop, not its primary
// defence against overflow — wrapLine in render.go is — and the call site
// itself is pinned by TestRefreshContentTruncatesTheStoredViewportContent in
// detail_internal_test.go, via viewport.GetContent(): that reads back the
// stored content directly, which is what makes it possible to tell this
// call's own truncation apart from bubbles/v2/viewport's separate,
// render-time clipping in View().
func (m *Model) refreshContent() {
	if m.width < minPaneWidth {
		return
	}

	m.viewport.SetContent(truncateLines(m.composeContent(), m.width))
}

// rebuildRenderer replaces the markdown renderer for a new word-wrap width.
//
// wrap is the only thing that ever changes here; the style comes from
// theme.GlamourStyle(), which theme.New has already resolved to one of
// "dark", "light" or "notty" — every value it can return is valid — so a
// failure on this call cannot happen in practice given that New succeeded
// with the same style. The previous renderer is kept on that unreachable
// branch rather than losing markdown entirely; it is still logged (I7).
func (m *Model) rebuildRenderer(wrap int) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(m.theme.GlamourStyle()),
		glamour.WithWordWrap(wrap),
	)
	if err != nil {
		if m.log != nil {
			m.log.Warn("rebuild markdown renderer", slog.Int("wrap", wrap), slog.Any("error", err))
		}
		return
	}

	m.renderer = renderer
	m.wrap = wrap
}

// markdown renders src, falling back to the raw text on failure.
//
// A malformed table or an unterminated fence in a description should cost
// formatting, not content — blanking the pane would hide the issue body over
// a typo in it.
func (m *Model) markdown(src string) string {
	return renderOrRaw(m.renderer, src)
}

// renderOrRaw is the fallback seam. It takes the renderer as a parameter
// rather than reading m.renderer so an internal test can inject one that
// fails — the only way to reach this branch, since glamour does not error on
// malformed markdown.
func renderOrRaw(r markdownRenderer, src string) string {
	out, err := r.Render(src)
	if err != nil {
		return src
	}

	return out
}

// issueID returns issue's id, or "" for nil — the zero value a fresh Model
// starts with, so the very first SetIssue call always counts as a change.
func issueID(issue *beads.Issue) string {
	if issue == nil {
		return ""
	}

	return issue.ID
}
