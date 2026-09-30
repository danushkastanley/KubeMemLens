package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
)

func traceModel(t *testing.T) appModel {
	t.Helper()
	m := loadedFixtureModel(t, 80, 24)
	m.view = viewContainers
	m.data.Containers = []api.ContainerSnapshot{{Namespace: "tenant-a", PodName: "selected-pod", PodUID: "selected-uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), NodeName: "selected-node"}}
	m.tracePanel.available = true
	m.openTracePanel()
	return m
}
func TestTracePanelPinsSelectionAcrossRefreshAndNavigation(t *testing.T) {
	m := traceModel(t)
	original := m.tracePanel.target
	if original.PodUID != "selected-uid" {
		t.Fatal("wrong initial selection")
	}
	m.tracePanel.loading = true
	m.data.Containers[0].PodUID = "replacement-uid"
	updated, _ := m.handleKey(keyMessage("esc"))
	m = updated.(appModel)
	updated, _ = m.handleKey(keyMessage("p"))
	m = updated.(appModel)
	updated, _ = m.handleKey(keyMessage("T"))
	m = updated.(appModel)
	if m.tracePanel.target.PodUID != original.PodUID || m.tracePanel.target.ContainerID != original.ContainerID {
		t.Fatal("refresh retargeted pending trace")
	}
	generation := m.tracePanel.generation
	m.receiveTracePrepared(tracePreparedMsg{generation: generation - 1, err: context.Canceled})
	if !m.tracePanel.loading || m.tracePanel.err != nil {
		t.Fatal("stale response replaced current trace")
	}
}
func TestTracePanelEveryStateFitsAndRetainsCaveats(t *testing.T) {
	for _, state := range []traceclient.State{traceclient.StateNew, traceclient.StatePreflight, traceclient.StateReady, traceclient.StateAdmitting, traceclient.StateAdmitted, traceclient.StateRunning, traceclient.StateCancelling, traceclient.StateCompleted, traceclient.StateCancelled, traceclient.StateTruncated, traceclient.StateFailed} {
		for _, size := range [][2]int{{40, 10}, {80, 24}, {120, 30}, {180, 50}} {
			m := traceModel(t)
			m.width, m.height = size[0], size[1]
			m.resizeViewports()
			m.tracePanel.session = &traceclient.Session{} // View-only marker; no lifecycle methods are called.
			m.tracePanel.snapshot = traceclient.Snapshot{State: state, Cleanup: traceclient.CleanupUnconfirmed}
			frame := m.viewString()
			assertFrameBounds(t, frame, size[0], size[1])
			if size[0] >= 80 && !strings.Contains(frame, "qualification incomplete") {
				t.Fatal("missing development caveat")
			}
			if !strings.Contains(strings.Join(m.tracePanelLines(), "\n"), string(state)) {
				t.Fatal("missing state", state)
			}
		}
	}
}
func TestTracePanelEditsLimitsOnlyBeforePreflight(t *testing.T) {
	m := traceModel(t)
	for _, key := range []string{"o", "d", "l"} {
		updated, _ := m.handleKey(keyMessage(key))
		m = updated.(appModel)
	}
	if m.tracePanel.intent.Kind != trace.OOM || m.tracePanel.intent.Bounds.Duration != time.Minute || m.tracePanel.intent.Bounds.Events != 100000 {
		t.Fatal("limits did not update")
	}
	m.tracePanel.loading = true
	before := m.tracePanel.intent
	for _, key := range []string{"f", "d", "l", "n"} {
		updated, _ := m.handleKey(keyMessage(key))
		m = updated.(appModel)
	}
	if m.tracePanel.intent != before {
		t.Fatal("in-flight intent changed")
	}
}
func TestTraceExportNeedsSeparateConfirmationAndCannotOverwrite(t *testing.T) {
	m := traceModel(t)
	m.tracePanel.snapshot = traceclient.Snapshot{State: traceclient.StateFailed, Cleanup: traceclient.CleanupUnconfirmed}
	updated, command := m.handleKey(keyMessage("x"))
	m = updated.(appModel)
	if command != nil || m.tracePanel.exportMode != "path" {
		t.Fatal("export did not request destination")
	}
	path := filepath.Join(t.TempDir(), "trace.json")
	m.tracePanel.exportPath = path
	updated, command = m.handleKey(keyMessage("enter"))
	m = updated.(appModel)
	if command != nil || m.tracePanel.exportMode != "confirm" {
		t.Fatal("export bypassed consent")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("export wrote before consent")
	}
	updated, command = m.handleKey(keyMessage("y"))
	m = updated.(appModel)
	if command == nil {
		t.Fatal("confirmed export did not start")
	}
	msg := command().(traceExportMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	updated, _ = m.Update(msg)
	m = updated.(appModel)
	data, err := os.ReadFile(path)
	if err != nil || !json.Valid(data) {
		t.Fatal("invalid export", err)
	}
	m.tracePanel.exportMode = "confirm"
	_, command = m.handleKey(keyMessage("y"))
	if msg := command().(traceExportMsg); msg.err == nil {
		t.Fatal("export overwrote file")
	}
}
func TestTraceUnavailableAndRevocationPreventActions(t *testing.T) {
	m := loadedFixtureModel(t, 80, 24)
	m.openTracePanel()
	if m.tracePanel.open || m.action.err == nil {
		t.Fatal("unavailable trace opened")
	}
	m = traceModel(t)
	m.tracePanel.snapshot = traceclient.Snapshot{State: traceclient.StateCompleted, Cleanup: traceclient.CleanupConfirmed}
	m.revokeTraceEvidence()
	if m.tracePanel.target.PodUID != "" || strings.Contains(m.viewString(), "selected-pod") {
		t.Fatal("revoked evidence retained")
	}
	updated, command := m.handleKey(keyMessage("x"))
	m = updated.(appModel)
	if command != nil || m.tracePanel.exportMode != "" {
		t.Fatal("revoked evidence exported")
	}
	// Navigation and ordinary refresh continue while the trace panel is hidden.
	updated, _ = m.handleKey(keyMessage("esc"))
	m = updated.(appModel)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	assertFrameBounds(t, updated.(appModel).viewString(), 120, 30)
}
