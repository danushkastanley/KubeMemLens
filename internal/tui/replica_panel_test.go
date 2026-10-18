package tui

import (
	"context"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/rivo/uniseg"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

type replicaPanelReader struct {
	*fakeSnapshotReader
	query func(context.Context, memoryhistory.Request) (replicabaseline.Report, error)
}

func (r *replicaPanelReader) ReplicaBaseline(ctx context.Context, request memoryhistory.Request) (replicabaseline.Report, error) {
	return r.query(ctx, request)
}

func TestReplicaPanelRefreshCancelsAndDiscardsLateResults(t *testing.T) {
	m := panelModel(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	m.client = &replicaPanelReader{fakeSnapshotReader: tuiFixtureReader(), query: func(ctx context.Context, _ memoryhistory.Request) (replicabaseline.Report, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return replicabaseline.Report{}, ctx.Err()
	}}
	updated, first := m.handleKey(keyMessage("B"))
	current := updated.(appModel)
	if !current.replicaPanel.open || !current.replicaPanel.loading || first == nil {
		t.Fatal("replica panel did not open")
	}
	done := make(chan replicaPanelMsg, 1)
	go func() { done <- first().(replicaPanelMsg) }()
	<-started
	generation := current.replicaPanel.generation
	updated, next := current.replicaPanelKey(keyMessage("r"))
	current = updated.(appModel)
	if next == nil || current.replicaPanel.generation <= generation {
		t.Fatal("refresh did not replace generation")
	}
	<-cancelled
	current.receiveReplicaPanel(<-done)
	if !current.replicaPanel.loading || current.replicaPanel.err != nil {
		t.Fatal("late result replaced refreshed panel")
	}
	current.clearRevokedData()
	if current.replicaPanel.open || current.replicaPanel.report != nil {
		t.Fatal("revoked replica evidence retained")
	}
	current.receiveReplicaPanel(replicaPanelMsg{generation: current.replicaPanel.generation - 1})
	if current.replicaPanel.report != nil {
		t.Fatal("closed panel accepted result")
	}
}

func TestReplicaPanelUnsupportedReaderAndInvalidReportStayExplicit(t *testing.T) {
	m := panelModel(t)
	if command := m.openReplicaPanel(); command != nil || m.replicaPanel.err == nil || m.replicaPanel.report != nil {
		t.Fatal("unsupported reader appeared successful")
	}
	m.replicaPanel.expectedPodUID = "selected"
	m.receiveReplicaPanel(replicaPanelMsg{generation: m.replicaPanel.generation})
	if m.replicaPanel.err == nil || m.replicaPanel.report != nil {
		t.Fatal("invalid report retained")
	}
	updated, _ := m.replicaPanelKey(keyMessage("esc"))
	current := updated.(appModel)
	if current.replicaPanel.open {
		t.Fatal("escape did not close")
	}
}

func TestReplicaPanelViewportWrapsAndReachesCaveats(t *testing.T) {
	m := panelModel(t)
	now := time.Now().UTC()
	root := changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team-a", Name: "app", UID: "root"}
	pod := changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: "app-0", UID: "pod"}
	report, err := replicabaseline.Analyse(replicabaseline.Input{Workload: root, ObservedAt: now, Peers: []replicabaseline.Peer{{Object: pod, WorkloadUID: root.UID, Revision: "rs", Shape: strings.Repeat("a", 64), Lifecycle: replicabaseline.Ready, Changes: replicabaseline.ChangeUnknown, SampleState: replicabaseline.Available, HistoryState: replicabaseline.Unreported, CapturedAt: now, StableSince: now.Add(-time.Hour)}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{80, 24}, {100, 30}} {
		m.width, m.height = size[0], size[1]
		m.replicaPanel = replicaPanel{open: true, report: &report}
		updated, _ := m.replicaPanelKey(keyMessage("G"))
		current := updated.(appModel)
		if current.replicaPanel.viewport.offset == 0 {
			t.Fatal("comparison could not scroll")
		}
		rendered := current.renderReplicaPanel(size[0])
		for _, line := range strings.Split(rendered, "\n") {
			if uniseg.StringWidth(line) > size[0] {
				t.Fatal("line exceeded terminal width")
			}
		}
		if !strings.Contains(rendered, "statistical probability") {
			t.Fatal("confidence caveat unreachable", rendered)
		}
	}
	m.replicaPanel = replicaPanel{open: true, expectedPodUID: "previous", request: memoryhistory.Request{Namespace: "team-a", Name: "app-0"}}
	m.receiveReplicaPanel(replicaPanelMsg{report: report})
	if m.replicaPanel.report != nil || m.replicaPanel.err == nil {
		t.Fatal("replacement Pod retained")
	}
}
