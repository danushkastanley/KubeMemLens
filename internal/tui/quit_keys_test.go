package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var ctrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

func quits(command tea.Cmd) bool {
	if command == nil {
		return false
	}
	_, quit := command().(tea.QuitMsg)
	return quit
}

func quitTestModel(prepare func(*appModel)) appModel {
	m := newModel(context.Background(), Options{AllNamespaces: true}, &fakeSnapshotReader{}, "test")
	m.width, m.height, m.loading = 100, 24, false
	prepare(&m)
	return m
}

// Help says "q / Ctrl+C quit". Ctrl+C must quit from every screen, including
// the ones that take typed text.
func TestCtrlCQuitsFromEveryMode(t *testing.T) {
	modes := map[string]func(*appModel){
		"main view":      func(*appModel) {},
		"filter input":   func(m *appModel) { m.searching = true; m.query = "api" },
		"action menu":    func(m *appModel) { m.action.mode = actionMenu },
		"capture path":   func(m *appModel) { m.action.mode = actionCapturePath; m.action.input = "capture.json" },
		"action result":  func(m *appModel) { m.action.mode = actionResultMode },
		"history panel":  func(m *appModel) { m.historyPanel.open = true },
		"replica panel":  func(m *appModel) { m.replicaPanel.open = true },
		"session panel":  func(m *appModel) { m.sessionPanel.open = true },
		"trace panel":    func(m *appModel) { m.tracePanel.open = true },
		"trace export":   func(m *appModel) { m.tracePanel.open = true; m.tracePanel.exportMode = "json" },
		"help over main": func(m *appModel) { m.help = true },
	}
	for name, prepare := range modes {
		t.Run(name, func(t *testing.T) {
			m := quitTestModel(prepare)
			if _, command := m.Update(ctrlC); !quits(command) {
				t.Fatalf("Ctrl+C did not quit from the %s", name)
			}
		})
	}
}

func TestCtrlCCancelsInFlightRequestsBeforeQuitting(t *testing.T) {
	cancelled := map[string]bool{}
	m := quitTestModel(func(m *appModel) {
		m.searching = true
		m.historyCancel = func() { cancelled["history"] = true }
		m.nodeCancel = func() { cancelled["node"] = true }
		m.volumeCancel = func() { cancelled["volume"] = true }
		m.historyPanel.cancel = func() { cancelled["history panel"] = true }
		m.replicaPanel.cancel = func() { cancelled["replica panel"] = true }
	})
	if _, command := m.Update(ctrlC); !quits(command) {
		t.Fatal("Ctrl+C did not quit")
	}
	for _, request := range []string{"history", "node", "volume", "history panel", "replica panel"} {
		if !cancelled[request] {
			t.Errorf("the %s request was left running", request)
		}
	}
}

func TestQQuitsFromActionMenuAndResult(t *testing.T) {
	for name, mode := range map[string]actionMode{"action menu": actionMenu, "action result": actionResultMode} {
		t.Run(name, func(t *testing.T) {
			m := quitTestModel(func(m *appModel) { m.action.mode = mode })
			if _, command := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"}); !quits(command) {
				t.Fatalf("q did not quit from the %s", name)
			}
		})
	}
}

// Where text is being typed, q is a character, not a command.
func TestQIsTypedInTextInputs(t *testing.T) {
	q := tea.KeyPressMsg{Code: 'q', Text: "q"}

	m := quitTestModel(func(m *appModel) { m.searching = true })
	updated, command := m.Update(q)
	if quits(command) || updated.(appModel).query != "q" {
		t.Fatalf("filter input: quit=%t query=%q, want q typed", quits(command), updated.(appModel).query)
	}

	m = quitTestModel(func(m *appModel) { m.action.mode = actionCapturePath })
	updated, command = m.Update(q)
	if quits(command) || updated.(appModel).action.input != "q" {
		t.Fatalf("capture path: quit=%t input=%q, want q typed", quits(command), updated.(appModel).action.input)
	}
}
