package tui

import (
	"bytes"
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type sessionProgramModel struct {
	app       appModel
	completed bool
	err       error
}

func (m sessionProgramModel) Init() tea.Cmd {
	return tea.Sequence(func() tea.Msg { return tea.WindowSizeMsg{Width: 80, Height: 24} }, func() tea.Msg { return keyMessage("I") }, func() tea.Msg { return keyMessage("n") })
}
func (m sessionProgramModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	next, command := m.app.Update(message)
	m.app = next.(appModel)
	if result, ok := message.(sessionPanelMsg); ok {
		if result.err != nil {
			m.err = result.err
			return m, tea.Quit
		}
		switch result.operation {
		case sessionStart:
			return m, tea.Sequence(command, func() tea.Msg { return keyMessage("r") })
		case sessionRefresh:
			m.completed = true
			return m, tea.Sequence(command, func() tea.Msg { return keyMessage("q") })
		}
	}
	return m, command
}
func (m sessionProgramModel) View() tea.View { return m.app.View() }

func TestSessionRunsThroughTerminalProgramAndTLS(t *testing.T) {
	app, requests := liveSessionModel(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var output bytes.Buffer
	program := tea.NewProgram(sessionProgramModel{app: *app}, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(&output), tea.WithoutSignalHandler(), tea.WithEnvironment([]string{"TERM=xterm-256color", "NO_COLOR=1"}))
	result, err := program.Run()
	if err != nil {
		t.Fatal(err)
	}
	model := result.(sessionProgramModel)
	if model.err != nil || !model.completed || model.app.sessionPanel.open || model.app.sessionPanel.connection != nil {
		t.Fatal("terminal session lifecycle incomplete", model.err)
	}
	if requests.Load() != 2 {
		t.Fatalf("unexpected request count: %d", requests.Load())
	}
	if output.Len() == 0 {
		t.Fatal("terminal program did not render")
	}
}
