package main

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Toshik1978/beads-viewer/internal/tui"
)

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
