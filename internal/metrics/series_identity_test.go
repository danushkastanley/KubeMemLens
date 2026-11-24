package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// Two instances that share every rendered label must come out as one series.
// A scraper rejects, or silently drops, a series that appears twice.
func TestRenderEmitsEachSeriesOnceWhenInstancesShareTheirLabels(t *testing.T) {
	now := fixedNow()
	container := func(podUID, containerID string, total uint64) api.ContainerSnapshot {
		return api.ContainerSnapshot{
			Namespace: "default", PodName: "db-0", PodUID: podUID, ContainerName: "app", ContainerID: containerID,
			NodeName: "node-a", CapturedAt: now, Memory: memory(total),
		}
	}
	pod := func(podUID string, total uint64) api.PodSnapshot {
		return api.PodSnapshot{Namespace: "default", PodName: "db-0", PodUID: podUID, NodeName: "node-a", CapturedAt: now, Memory: memory(total)}
	}
	source := testSource{
		containers: []api.ContainerSnapshot{container("uid-old", "id-old", 100), container("uid-new", "id-new", 200)},
		pods:       []api.PodSnapshot{pod("uid-old", 100), pod("uid-new", 200)},
		namespaces: []api.NamespaceSnapshot{{Namespace: "default", CapturedAt: now, PodCount: 2, Memory: memory(300)}},
		debug:      api.DebugStore{TotalContainers: 2, Pods: 2, Namespaces: 1},
		latest:     map[string]time.Time{"node-a": now},
	}
	opts := DefaultOptions()
	opts.IncludeContainerMetrics = true
	opts.IncludeEventMetrics = true
	opts.IncludeDiagnosisMetrics = true

	out, err := (Exporter{Source: source, TTL: time.Minute, Now: fixedNow, Opts: opts}).Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	seen := map[string]struct{}{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		series := line[:strings.LastIndex(line, " ")]
		if _, repeated := seen[series]; repeated {
			t.Errorf("series rendered more than once: %s", series)
		}
		seen[series] = struct{}{}
	}
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="db-0",type="total"} 300`)
	assertContains(t, out, `kubememlens_container_memory_bytes{container="app",namespace="default",node="node-a",pod="db-0",type="total"} 300`)
}
