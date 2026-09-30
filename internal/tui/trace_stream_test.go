package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
)

func TestTracePanelConsumesMaximumStreamWhileNavigating(t *testing.T) {
	fixture := traceTUIFixture(t)
	m := traceModel(t)
	m.data.Containers[0].PodUID = "private-pod-uid"
	m.selectTraceTarget()
	m.tracePanel.client = fixture.client
	m.tracePanel.intent.Bounds.Events = 100000
	m.tracePanel.intent.Bounds.OutputBytes = 32 << 20
	// Exercise the most demanding supported stream; the UI retains no raw data.
	m.tracePanel.intent.Paths = trace.ConfirmedPaths
	prepare := m.prepareTrace()
	if prepare == nil {
		t.Fatal("preflight did not start")
	}
	m.receiveTracePrepared(prepare().(tracePreparedMsg))
	if m.tracePanel.snapshot.State != traceclient.StateReady {
		t.Fatal("preflight failed", m.tracePanel.err)
	}
	updated, wait := m.handleKey(keyMessage("enter"))
	m = updated.(appModel)
	if wait == nil {
		t.Fatal("stream did not start")
	}
	pinned := m.tracePanel.target.PodUID
	m.data.Containers[0].PodUID = "replacement"
	deadline := time.After(10 * time.Second)
	updates := 0
	for !m.tracePanel.snapshot.State.Terminal() {
		incoming := make(chan tea.Msg, 1)
		go func(command tea.Cmd) { incoming <- command() }(wait)
		// UI keys and resizing run without waiting for the stream reader.
		updated, _ = m.handleKey(keyMessage("esc"))
		m = updated.(appModel)
		updated, _ = m.handleKey(keyMessage("p"))
		m = updated.(appModel)
		updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = updated.(appModel)
		updated, _ = m.handleKey(keyMessage("T"))
		m = updated.(appModel)
		assertFrameBounds(t, m.viewString(), 80, 24)
		select {
		case msg := <-incoming:
			updated, wait = m.Update(msg)
			m = updated.(appModel)
			updates++
		case <-deadline:
			m.tracePanel.session.Cancel()
			t.Fatal("bounded stream did not terminate")
		}
		if m.tracePanel.target.PodUID != pinned {
			t.Fatal("refresh retargeted stream")
		}
	}
	if m.tracePanel.snapshot.State != traceclient.StateTruncated || m.tracePanel.snapshot.Result.DeliveredEvents != 100000 || m.tracePanel.snapshot.Cleanup != traceclient.CleanupConfirmed {
		t.Fatal("maximum stream lost terminal evidence", m.tracePanel.snapshot.Failure)
	}
	if updates > 105 {
		t.Fatal("unbounded UI update queue", updates)
	}
	if fixture.creates.Load() != 1 || fixture.streams.Load() != 1 || fixture.deletes.Load() != 1 {
		t.Fatal("UI repeated activation")
	}
}
