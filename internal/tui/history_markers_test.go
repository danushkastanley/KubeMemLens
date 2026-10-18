package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/rivo/uniseg"
)

type markerPanelReader struct {
	*panelHistoryReader
	contextQuery func(context.Context, memoryhistory.Request, memoryhistory.Query) (changemarkers.Context, error)
}

func (r *markerPanelReader) MemoryHistoryContext(ctx context.Context, request memoryhistory.Request, q memoryhistory.Query) (changemarkers.Context, error) {
	return r.contextQuery(ctx, request, q)
}

func TestMarkerToggleCancelsAndRejectsOldResponses(t *testing.T) {
	m := panelModel(t)
	base := m.client.(*panelHistoryReader)
	started, cancelled := make(chan struct{}), make(chan struct{})
	reader := &markerPanelReader{panelHistoryReader: base, contextQuery: func(ctx context.Context, _ memoryhistory.Request, _ memoryhistory.Query) (changemarkers.Context, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return changemarkers.Context{}, ctx.Err()
	}}
	m.client = reader
	_ = m.openHistoryPanel()
	prior := m.historyPanel.generation
	updated, command := m.historyPanelKey(keyMessage("m"))
	current := updated.(appModel)
	if command == nil || !current.historyPanel.loading || current.historyPanel.mode != markedHistory || current.historyPanel.generation <= prior {
		t.Fatal("marker toggle lost pending state")
	}
	done := make(chan historyPanelMsg, 1)
	go func() { done <- command().(historyPanelMsg) }()
	<-started
	updated, next := current.historyPanelKey(keyMessage("m"))
	current = updated.(appModel)
	<-cancelled
	if next == nil || current.historyPanel.mode != plainHistory {
		t.Fatal("plain history toggle failed")
	}
	current.receiveHistoryPanel(<-done)
	if !current.historyPanel.loading || current.historyPanel.markers != nil || current.historyPanel.err != nil {
		t.Fatal("cancelled marker result replaced plain history")
	}
	current.closeHistoryPanel()
}

func TestDenseMarkersRemainScrollableAtSupportedTerminalSizes(t *testing.T) {
	m := panelModel(t)
	now := time.Now().UTC()
	history := memoryhistory.Report{ReceivedAt: now, Query: memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet}, State: memoryhistory.Missing, Completeness: memoryhistory.Partial}
	report := changemarkers.Report{Events: changemarkers.Partial, Truncated: true}
	for i := 0; i < changemarkers.MaxMarkers; i++ {
		report.Markers = append(report.Markers, changemarkers.Marker{Kind: changemarkers.Rollout, At: now.Add(time.Duration(i) * time.Second), Until: now.Add(time.Duration(i) * time.Second), Subject: changemarkers.Object{Kind: "Deployment", Name: fmt.Sprintf("workload-%03d", i)}, Clock: changemarkers.KubernetesEvent, Count: 1, Uncertain: true})
	}
	for _, size := range [][2]int{{80, 24}, {120, 36}, {160, 48}} {
		m.width, m.height = size[0], size[1]
		m.resizeViewports()
		m.historyPanel = historyPanel{open: true, mode: markedHistory, report: &history, markers: &report}
		updated, _ := m.historyPanelKey(keyMessage("G"))
		current := updated.(appModel)
		text := current.viewString()
		lines := strings.Split(text, "\n")
		if len(lines) > size[1] {
			t.Fatalf("marker rows escaped height %v", size)
		}
		for _, line := range lines {
			if uniseg.StringWidth(line) > size[0] {
				t.Fatalf("marker row escaped width %v", size)
			}
		}
		if !strings.Contains(text, "workload-063") {
			t.Fatalf("last bounded marker unreachable at %v", size)
		}
	}
}
