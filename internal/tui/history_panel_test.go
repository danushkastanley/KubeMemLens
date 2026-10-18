package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/historyview"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/rivo/uniseg"
)

type panelHistoryReader struct {
	*fakeSnapshotReader
	query func(context.Context, memoryhistory.Request, memoryhistory.Query) (memoryhistory.Report, error)
}

func (r *panelHistoryReader) MemoryHistory(ctx context.Context, request memoryhistory.Request, q memoryhistory.Query) (memoryhistory.Report, error) {
	return r.query(ctx, request, q)
}

func panelModel(t *testing.T) *appModel {
	t.Helper()
	reader := &panelHistoryReader{fakeSnapshotReader: tuiFixtureReader()}
	reader.query = func(context.Context, memoryhistory.Request, memoryhistory.Query) (memoryhistory.Report, error) {
		return memoryhistory.Report{}, memoryhistory.ErrUnavailable
	}
	m := newModel(t.Context(), Options{AllNamespaces: true}, reader, "test")
	m.width, m.height = 80, 24
	m.resizeViewports()
	m.data = m.fetchCmd()().(fetchMsg).data
	m.loading = false
	return &m
}

func TestHistoryPanelSourceSwitchCancelsAndRejectsLateResults(t *testing.T) {
	m := panelModel(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	r := m.client.(*panelHistoryReader)
	r.query = func(ctx context.Context, _ memoryhistory.Request, _ memoryhistory.Query) (memoryhistory.Report, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return memoryhistory.Report{}, ctx.Err()
	}
	updated, first := m.handleKey(keyMessage("H"))
	current := updated.(appModel)
	if !current.historyPanel.open || !current.historyPanel.loading || first == nil {
		t.Fatal("history did not open asynchronously")
	}
	firstID := current.historyPanel.generation
	done := make(chan historyPanelMsg, 1)
	go func() { done <- first().(historyPanelMsg) }()
	<-started
	updated, next := current.historyPanelKey(keyMessage("p"))
	current = updated.(appModel)
	if next == nil || current.historyPanel.source != memoryhistory.Prometheus || current.historyPanel.generation == firstID {
		t.Fatal("source switch failed")
	}
	<-cancelled
	current.receiveHistoryPanel(<-done)
	if current.historyPanel.err != nil || !current.historyPanel.loading {
		t.Fatal("old source response replaced the pending source")
	}
	current.closeHistoryPanel()
}

func TestHistoryPanelRevocationAndPodReplacementClearEvidence(t *testing.T) {
	m := panelModel(t)
	_ = m.openHistoryPanel()
	p := &m.historyPanel
	p.expectedPodUID = "original"
	m.receiveHistoryPanel(historyPanelMsg{generation: p.generation, report: memoryhistory.Report{Selection: memoryhistory.Selection{UID: "replacement"}}})
	if p.report != nil || p.err == nil {
		t.Fatal("replacement Pod history displayed")
	}
	old := p.generation
	m.clearRevokedData()
	m.receiveHistoryPanel(historyPanelMsg{generation: old, report: memoryhistory.Report{}})
	if m.historyPanel.open || m.historyPanel.report != nil {
		t.Fatal("revoked history survived")
	}
}

func TestHistoryPanelBoundsMaximumHistoryAtSupportedSizes(t *testing.T) {
	m := panelModel(t)
	now := time.Now().UTC().Truncate(time.Second)
	r := memoryhistory.Report{ReceivedAt: now, State: memoryhistory.Missing, Completeness: memoryhistory.Partial, Query: memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: now.Add(-24 * time.Hour), End: now, Step: 6 * time.Minute}}
	for range memoryhistory.MaxTargets {
		s := memoryhistory.Series{Target: memoryhistory.Target{Namespace: strings.Repeat("n", 63), Pod: strings.Repeat("p", 253), Container: "app"}, Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Origin: "cadvisor", SampleClock: "prometheus-sample", Freshness: memoryhistory.Missing, Completeness: memoryhistory.Partial}
		for i := 0; i < memoryhistory.MaxPoints; i++ {
			s.Points = append(s.Points, memoryhistory.Point{At: r.Query.Start.Add(time.Duration(i) * r.Query.Step), State: memoryhistory.Missing})
		}
		r.Series = append(r.Series, s)
	}
	m.historyPanel = historyPanel{open: true, source: memoryhistory.Prometheus, report: &r}
	for _, size := range [][2]int{{80, 24}, {120, 36}, {160, 48}} {
		m.width, m.height = size[0], size[1]
		m.resizeViewports()
		text := m.viewString()
		lines := strings.Split(text, "\n")
		if len(lines) > size[1] {
			t.Fatalf("height=%d at %v", len(lines), size)
		}
		for _, line := range lines {
			if uniseg.StringWidth(line) > size[0] {
				t.Fatalf("overflow at %v: %q", size, line)
			}
		}
		if !strings.Contains(text, "Prometheus") || !strings.Contains(text, "coverage: partial") {
			t.Fatal("source or completeness hidden")
		}
		updated, _ := m.historyPanelKey(keyMessage("G"))
		end := updated.(appModel)
		if end.historyPanel.viewport.offset == 0 {
			t.Fatal("maximum response could not scroll")
		}
		if !strings.Contains(end.renderHistoryPanel(size[0]), "Sample: unreported · prometheus-sample") {
			t.Fatal("last series sample clock is not reachable")
		}
	}
	if len(historyview.Lines(r, now, 80)) < memoryhistory.MaxTargets {
		t.Fatal("series silently discarded")
	}
}
