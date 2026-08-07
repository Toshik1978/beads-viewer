package main

import (
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/tui"
)

// requireTerminal refuses to start the UI when stdin is not a terminal.
//
// Without it, `echo | bv` and `bv < /dev/null` hang. bubbletea does not
// report that its input is unusable: it falls back to opening /dev/tty,
// finds the controlling terminal of whatever shell started bv, and blocks
// there forever — before rendering anything, with the workspace already
// loaded. What the user sees is a process that never returns and never says
// why, which is the worst failure a program can have.
//
// The mode bit is the whole test, and the standard library is enough for it:
// a terminal is a character device and a pipe or a regular file is not.
// Reaching for a terminal library would buy a more thorough answer than this
// question has, at the cost of a tenth direct dependency.
//
// It runs last, after the workspace is open, rather than first. Everything
// before it can fail for reasons the user actually caused — a missing
// workspace, a malformed issues.jsonl — and those are the more useful things
// to be told about first when both are true. The cost of being told late is
// one decode, which is milliseconds.
func requireTerminal(stdin *os.File) error {
	info, err := stdin.Stat()
	if err != nil {
		return fmt.Errorf("inspect stdin: %w", err)
	}

	if info.Mode()&os.ModeCharDevice == 0 {
		return errors.New(
			"stdin is not a terminal — bv is an interactive viewer and cannot run " +
				"from a pipe, a file or a job without a terminal attached")
	}

	return nil
}

// programModel adapts *tui.Model to bubbletea's interface: tea.Model.Update
// must return the tea.Model interface, which ireturn forbids everywhere else,
// so the conversion is confined to this one delegating type.
type programModel struct{ m *tui.Model }

func newProgramModel(m *tui.Model) programModel { return programModel{m: m} }

func (p programModel) Init() tea.Cmd { return p.m.Init() }

//nolint:ireturn // tea.Model's contract requires returning the interface.
func (p programModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := p.m.Update(msg)

	return programModel{m: next}, cmd
}

// View sets alt-screen and mouse mode on tea.View: v2.0.8 moved both off
// ProgramOptions and onto View itself.
func (p programModel) View() tea.View {
	v := tea.NewView(p.m.View())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	return v
}
