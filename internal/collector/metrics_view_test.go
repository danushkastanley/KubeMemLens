package collector

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
)

func TestMetricsViewDropsEntitySeriesBeforeRetainingOverBudgetAggregates(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := NewStore()
	containers := []api.ContainerSnapshot{
		scopedContainer("team-a", "pod-a", "uid-a", "container-a", now),
		scopedContainer("team-b", "pod-b", "uid-b", "container-b", now),
		scopedContainer("team-c", "pod-c", "uid-c", "container-c", now),
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	opts := metrics.DefaultOptions()
	view, effective := store.BuildMetricsView(now, time.Minute, opts, 1024)
	if view.debug.TotalContainers != 3 || view.debug.Pods != 3 || view.debug.Namespaces != 3 {
		t.Fatalf("debug counts = %#v", view.debug)
	}
	if len(view.namespaces) != 0 || len(view.pods) != 0 || effective.MaxPods != 1 {
		t.Fatalf("retained namespaces=%d pods=%d maxPods=%d", len(view.namespaces), len(view.pods), effective.MaxPods)
	}
	content, err := (metrics.Exporter{Source: view, TTL: time.Minute, Now: func() time.Time { return now }, Opts: effective}).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(content, `level="namespace",reason="response_budget_exceeded"`) ||
		strings.Contains(content, "kubememlens_namespace_memory_bytes{") {
		t.Fatalf("unexpected bounded metrics:\n%s", content)
	}
}

func TestMetricsViewRendersWithinBudget(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := NewStore()
	container := scopedContainer("team-a", "pod-a", "uid-a", "container-a", now)
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{container}}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	view, effective := store.BuildMetricsView(now, time.Minute, metrics.DefaultOptions(), 16<<20)
	content, err := (metrics.Exporter{Source: view, TTL: time.Minute, Now: func() time.Time { return now }, Opts: effective}).Render()
	if err != nil || !strings.Contains(content, `kubememlens_namespace_memory_bytes{namespace="team-a"`) ||
		!strings.Contains(content, `kubememlens_pod_memory_bytes{namespace="team-a"`) {
		t.Fatalf("Render err=%v content=%s", err, content)
	}
}

func TestMetricsViewRetainsMappingCoverageWhenContainerSeriesAreDisabled(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := NewStore()
	mapped := scopedContainer("team-a", "pod-a", "uid-a", "container-a", now)
	unmapped := api.ContainerSnapshot{ContainerID: "container-b", CapturedAt: now}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{mapped, unmapped},
	}); err != nil {
		t.Fatalf("ReplaceNodeSnapshot: %v", err)
	}

	view, effective := store.BuildMetricsView(now, time.Minute, metrics.DefaultOptions(), 16<<20)
	if len(view.containers) != 0 {
		t.Fatalf("retained %d container series with container metrics disabled", len(view.containers))
	}
	content, err := (metrics.Exporter{Source: view, TTL: time.Minute, Now: func() time.Time { return now }, Opts: effective}).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`kubememlens_collector_mapping_containers{result="found"} 2`,
		`kubememlens_collector_mapping_containers{result="mapped"} 1`,
		`kubememlens_collector_mapping_containers{result="unmapped"} 1`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("metrics missing %q:\n%s", want, content)
		}
	}
}

func BenchmarkMetricsViewAtConfiguredCapacity(b *testing.B) {
	now := time.Unix(1_800_000_000, 0).UTC()
	containers := make([]api.ContainerSnapshot, DefaultStoreLimits().MaxContainers)
	for index := range containers {
		containers[index] = api.ContainerSnapshot{
			Namespace: "target", PodName: fmt.Sprintf("pod-%06d", index), PodUID: fmt.Sprintf("uid-%06d", index),
			ContainerName: "app", ContainerID: fmt.Sprintf("container-%06d", index), CapturedAt: now,
		}
	}
	store := NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		b.Fatalf("ReplaceNodeSnapshot: %v", err)
	}
	opts := metrics.DefaultOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		view, _ := store.BuildMetricsView(now, time.Minute, opts, 16<<20)
		if view.debug.TotalContainers != len(containers) || view.debug.Pods != len(containers) {
			b.Fatalf("debug = %#v", view.debug)
		}
	}
}
