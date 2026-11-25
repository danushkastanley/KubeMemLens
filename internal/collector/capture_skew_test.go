package collector

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
)

// A node whose clock is 45 seconds behind is accepted on every post, since
// snapshots up to two minutes old are, and is then reported stale by the
// 30-second freshness window. The gauge is what shows why.
func TestCaptureSkewGaugeShowsANodeWhoseClockIsBehind(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	store := NewStore()
	store.now = func() time.Time { return now }
	for node, behind := range map[string]time.Duration{"node-on-time": time.Second, "node-behind": 45 * time.Second} {
		snapshot := api.AgentSnapshot{
			SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: node, CapturedAt: now.Add(-behind),
			Containers: []api.ContainerSnapshot{{ContainerID: "id-" + node}},
		}
		if _, err := store.ReplaceNodeSnapshot(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	view, options := store.BuildMetricsView(now, 30*time.Second, metrics.DefaultOptions(), 1<<20)
	text, err := (metrics.Exporter{Source: view, TTL: 30 * time.Second, Now: func() time.Time { return now }, Opts: options, MaxBytes: 1 << 20}).Render()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`kubememlens_collector_agent_capture_skew_seconds{node="node-behind"} 45`,
		`kubememlens_collector_agent_capture_skew_seconds{node="node-on-time"} 1`,
	} {
		if !strings.Contains(text, line+"\n") {
			t.Fatalf("missing %s in:\n%s", line, text)
		}
	}
}
