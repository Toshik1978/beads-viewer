package tui

// This file is the export-test seam: identifiers with zero non-test callers
// that exist solely so tui_test's suites (help_test.go, status_test.go) can
// reach unexported functionality from outside the package. A _test.go file
// compiles only under `go test`, so nothing here counts against the non-test
// line cap or the god-object tripwires that cap guards — see internal_test.go
// for the same pattern applied to WhiteBoxSuite, one file over.

import (
	"reflect"

	"charm.land/bubbles/v2/key"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
	"github.com/Toshik1978/beads-viewer/internal/tui/theme"
)

// RenderHelpForTest exposes renderHelp to help_test.go, which lives in
// tui_test like every other suite in this package's test binary.
func RenderHelpForTest(keys KeyMap, th theme.Theme, width, height int) string {
	return renderHelp(keys, th, width, height)
}

// All returns every KeyMap binding via reflection over k's own fields, not
// by flattening helpGroups (help.go) as an earlier version did. Deriving
// both sides of "every bound key is documented" from helpGroups made the
// drift guard inert for whichever binding a future edit dropped from that
// function: the same edit that stopped documenting a key would also stop
// asserting it, so help_test.go stayed green regardless. Reading k's fields
// directly is independent of helpGroups, so a binding missing from the
// overlay is missing from only one side of the comparison and the guard
// actually catches it. v.Fields() (not NumField/Field(i)) is what
// golangci-lint's modernize check wants for this walk.
func (k KeyMap) All() []key.Binding {
	v := reflect.ValueOf(k)
	all := make([]key.Binding, 0, v.NumField())
	for _, field := range v.Fields() {
		if b, ok := field.Interface().(key.Binding); ok {
			all = append(all, b)
		}
	}

	return all
}

// StatusState is statusState's exported name. status_test.go lives in
// tui_test like every other suite in this package's test binary, and the
// design's own "Produces" contract names the type statusState — the alias
// is what lets both be true at once.
type StatusState = statusState

// RenderStatusForTest exposes renderStatus to status_test.go.
func RenderStatusForTest(st StatusState, th theme.Theme, width int) string {
	return renderStatus(st, th, width)
}

// EmptyReason is emptyReason's exported name, and EmptyWorkspace/EmptyFilter/
// EmptyLoadError its exported values, for empty_test.go — the same alias
// pattern StatusState uses above, for the same reason: the design's own
// "Produces" contract names the unexported type directly.
type EmptyReason = emptyReason

const (
	EmptyWorkspace = emptyWorkspace
	EmptyFilter    = emptyFilter
	EmptyLoadError = emptyLoadError
)

// RenderEmptyForTest exposes renderEmpty to empty_test.go.
func RenderEmptyForTest(reason EmptyReason, f beads.Filter, errText string, th theme.Theme, width, height int) string {
	return renderEmpty(reason, f, errText, th, width, height)
}

// ClearStatusMsg is clearStatusMsg's exported name, for
// app_test.go's token-guard coverage: clearMessageAfter's real delay is 3
// seconds, so a test proving the guard actually discriminates generations
// needs to deliver the timer message by hand instead of waiting one out.
type ClearStatusMsg = clearStatusMsg

// NewClearStatusMsgForTest builds the ClearStatusMsg a real timer would
// eventually deliver, carrying token.
func NewClearStatusMsgForTest(token int) ClearStatusMsg {
	return clearStatusMsg{token: token}
}

// StatusTokenForTest exposes Model.status.token — setStatus's own
// generation counter (status.go) — so a test can read the current
// generation to build a stale or matching ClearStatusMsg against it.
func (m *Model) StatusTokenForTest() int {
	return m.status.token
}

// ExportPaneHeights exposes Layout.paneHeights for the layout suite: the
// border deduction happens there rather than in Compute, and a test that
// could only see Compute's own return value would not catch it.
func ExportPaneHeights(l Layout) (list, detail int) { return l.paneHeights() }

// ViewKindForTest reports which view is active, so a test can assert that a
// key actually switched panes rather than only that selection moved.
func ViewKindForTest(m *Model) config.ViewKind { return viewKindAt(m.active) }
