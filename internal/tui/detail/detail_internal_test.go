package detail

import (
	"errors"
	"log/slog"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// InternalSuite holds tests that need access to detail's unexported seam —
// renderOrRaw and markdownRenderer — to reach the raw-text fallback that no
// external test can trigger: glamour v2.0.1 does not error on malformed
// markdown, so only an injected failing renderer can prove the fallback
// exists. TestDetail in detail_test.go runs this suite; folio's one-entry-
// point rule is per test binary, not per file, so this file declares no
// top-level Test function of its own.
type InternalSuite struct {
	suite.Suite
}

// stubRenderer always fails, standing in for glamour when a test needs to
// reach the fallback path glamour itself never takes.
type stubRenderer struct{}

func (stubRenderer) Render(string) (string, error) {
	return "", errors.New("stub renderer always fails")
}

// TestRenderFallsBackWhenTheRendererFails covers the path
// TestMalformedMarkdownStillShowsItsContent cannot reach: glamour degrades
// malformed input rather than erroring, so only an injected failing renderer
// exercises renderOrRaw's fallback branch.
func (s *InternalSuite) TestRenderFallsBackWhenTheRendererFails() {
	const src = "raw **markdown** source"

	s.Equal(src, renderOrRaw(stubRenderer{}, src))
}

// TestTruncateLinesClampsEveryLineIndependently unit-tests truncateLines
// directly, rather than only through the full pane.
//
// TestNoLineExceedsTheWidth in detail_test.go proves the pane's overall
// output never overflows, but it cannot isolate truncateLines as the reason:
// bubbles/v2/viewport cuts every line to its own width internally whenever
// SoftWrap is left false (this pane's setting), so — measured directly by
// temporarily deleting the truncateLines call in refreshContent and rerunning
// the suite — that pane-level test stays green even with the backstop
// removed. The invariant still holds end to end, just via two overlapping
// mechanisms; this test is what actually pins truncateLines' own behaviour so
// a regression in the helper itself has somewhere to fail.
func (s *InternalSuite) TestTruncateLinesClampsEveryLineIndependently() {
	const width = 5

	in := "short\nthis line is much longer than the width\n"
	got := truncateLines(in, width)

	lines := strings.Split(got, "\n")
	s.Require().Len(lines, 3, "line count must be preserved")
	for _, line := range lines {
		s.LessOrEqual(ansi.StringWidth(line), width, "line: %q", line)
	}
	s.Equal("short", lines[0], "a line already within width must survive untouched")
}

// TestRefreshContentTruncatesTheStoredViewportContent pins refreshContent's
// call to truncateLines — the call site itself, not just the helper's own
// behaviour, which TestTruncateLinesClampsEveryLineIndependently above
// already covers in isolation.
//
// Removing the call at detail.go's refreshContent (leaving truncateLines
// intact) leaves TestNoLineExceedsTheWidth and every other View()-level
// width assertion in detail_test.go green, because
// bubbles/v2/viewport@v2.1.1's own View() unconditionally clips every line
// when SoftWrap is false (this pane's setting) — viewport.go:352-364.
// That clipping is reversible: the wide line is still sitting in the
// viewport's stored content, restored the moment the pane widens again.
// truncateLines' clipping is not — it deletes before SetContent ever sees
// the overhang. viewport.GetContent() reads the stored content back
// directly, bypassing View()'s own clipping, which is what makes this test
// able to tell the two apart.
//
// The fixture is a single 200-cell run with no spaces or hyphens, as an
// assignee: measured directly, both wrapLine (render.go, word-boundary
// wrapping) and glamour's own reflow give up on a run that long and emit it
// on one unbroken line regardless of pane width — a markdown table was tried
// first and turned out not to be discriminating, since this glamour version
// reflows table cells just fine down to very narrow widths. Only a run with
// no breakpoint at all is guaranteed to still need truncateLines' backstop
// after both of those wrapping layers have had their turn.
func (s *InternalSuite) TestRefreshContentTruncatesTheStoredViewportContent() {
	const width = 20

	issue := &beads.Issue{
		ID: "bv-1", Title: "T", Status: beads.StatusOpen,
		Assignee: strings.Repeat("y", 200),
	}
	m, err := New(slog.New(slog.DiscardHandler), theme.New(config.ThemeDark, theme.BackgroundDark))
	s.Require().NoError(err)
	m.SetSize(width, 40)
	m.SetIssue(issue, beads.NewSnapshot([]beads.Issue{*issue}))

	stored := m.viewport.GetContent()
	s.Require().NotEmpty(stored)
	for line := range strings.SplitSeq(stored, "\n") {
		s.LessOrEqual(ansi.StringWidth(line), width,
			"the content stored in the viewport must already be truncated, not just clipped at render time: %q", line)
	}
}

// TestSetIssueNeverReplacesTheRenderer is a structural counterpart to
// TestRendererIsBuiltOnce in detail_test.go.
//
// Measured on this machine: glamour.NewTermRenderer(WithStandardStyle("dark"),
// WithWordWrap(60)) constructed 200 times takes about 16ms total — reproduced
// by temporarily adding a call to rebuildRenderer at the top of SetIssue and
// rerunning the suite, which still passed TestRendererIsBuiltOnce's 2-second
// budget in 0.02s. The timing assertion is therefore not discriminating on
// fast hardware, even though it is the acceptance criterion as written; this
// test pins the same invariant structurally instead, by asserting the
// renderer's identity is untouched across repeated SetIssue calls, which
// holds regardless of how fast reconstruction happens to be.
func (s *InternalSuite) TestSetIssueNeverReplacesTheRenderer() {
	stub := &countingStub{}
	m := &Model{
		renderer: stub,
		viewport: viewport.New(),
		wrap:     defaultWrap,
		width:    60,
	}
	m.viewport.SetWidth(60)
	m.viewport.SetHeight(30)

	issue := &beads.Issue{ID: "bv-1", Title: "T"}
	snap := beads.NewSnapshot([]beads.Issue{*issue})
	for range 5 {
		m.SetIssue(issue, snap)
	}

	s.Same(stub, m.renderer, "SetIssue must never replace the renderer")
}

// countingStub satisfies markdownRenderer; TestSetIssueNeverReplacesTheRenderer
// only needs its identity, not its output.
type countingStub struct{}

func (*countingStub) Render(src string) (string, error) {
	return src, nil
}
