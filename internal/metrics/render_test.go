package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestRenderIncludesHelpTypeNamespaceAndPodMetrics(t *testing.T) {
	out := renderForTest(t, DefaultOptions())

	assertContains(t, out, "# HELP kubememlens_collector_store_entities")
	assertContains(t, out, "# TYPE kubememlens_collector_store_entities gauge")
	assertContains(t, out, `kubememlens_collector_store_entities{kind="history_series"}`)
	assertContains(t, out, `kubememlens_collector_store_entities{kind="history_points"}`)
	assertContains(t, out, `kubememlens_namespace_memory_bytes{namespace="default",type="total"} 300`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="anon"} 150`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="residual"} 75`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="slab_reclaimable"} 25`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="slab_unreclaimable"} 15`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="socket"} 7`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="page_tables"} 8`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="file_mapped"} 9`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="anon_thp"} 11`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="file_thp"} 12`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="shmem_thp"} 13`)
	assertContains(t, out, "# EOF\n")
}

func TestRenderSkipsPodMetricsWhenDisabled(t *testing.T) {
	opts := DefaultOptions()
	opts.IncludePodMetrics = false

	out := renderForTest(t, opts)

	assertNotContains(t, out, "kubememlens_pod_memory_bytes")
	assertContains(t, out, `kubememlens_metrics_dropped_entities{level="pod",reason="disabled"} 1`)
}

func TestRenderDoesNotRenderContainerMetricsByDefault(t *testing.T) {
	out := renderForTest(t, DefaultOptions())

	assertNotContains(t, out, "kubememlens_container_memory_bytes")
	assertContains(t, out, `kubememlens_metrics_dropped_entities{level="container",reason="disabled"} 1`)
}

func TestRenderContainerMetricsWhenEnabled(t *testing.T) {
	opts := DefaultOptions()
	opts.IncludeContainerMetrics = true

	out := renderForTest(t, opts)

	assertContains(t, out, `kubememlens_container_memory_bytes{container="app",namespace="default",node="node-a",pod="api",type="total"} 300`)
}

func TestRenderRespectsMaxPodAndContainerGuardrails(t *testing.T) {
	opts := DefaultOptions()
	opts.IncludeContainerMetrics = true
	opts.MaxPods = 0
	opts.MaxContainers = 0

	out := renderForTest(t, opts)

	assertNotContains(t, out, "kubememlens_pod_memory_bytes")
	assertNotContains(t, out, "kubememlens_container_memory_bytes")
	assertContains(t, out, `kubememlens_metrics_dropped_entities{level="pod",reason="max_entities_exceeded"} 1`)
	assertContains(t, out, `kubememlens_metrics_dropped_entities{level="container",reason="max_entities_exceeded"} 1`)
}

func TestRenderDiagnosisAndEventsCanBeDisabled(t *testing.T) {
	opts := DefaultOptions()
	opts.IncludeDiagnosisMetrics = false
	opts.IncludeEventMetrics = false

	out := renderForTest(t, opts)

	assertNotContains(t, out, "kubememlens_pod_diagnosis")
	assertNotContains(t, out, "kubememlens_pod_memory_events")
}

func TestRenderDiagnosisAndEventsWhenEnabled(t *testing.T) {
	out := renderForTest(t, DefaultOptions())

	assertContains(t, out, `kubememlens_namespace_diagnosis{diagnosis="memory-pressure",namespace="default"} 1`)
	assertContains(t, out, `kubememlens_pod_memory_events{event="oom",namespace="default",node="node-a",pod="api"} 2`)
	assertContains(t, out, `kubememlens_pod_memory_events{event="local_high_delta",namespace="default",node="node-a",pod="api"} 1`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="peak"} 450`)
}

func TestRenderDoesNotExposeHighCardinalityIdentifiers(t *testing.T) {
	opts := DefaultOptions()
	opts.IncludeContainerMetrics = true
	out := renderForTest(t, opts)

	for _, forbidden := range []string{"container-123", "pod-uid-123", "cgroupPath", "/host/sys/fs/cgroup", "/var/lib/app/cache/file.db", "customer-secret"} {
		assertNotContains(t, out, forbidden)
	}
}

func TestRenderEscapesLabelValues(t *testing.T) {
	source := testSource{
		namespaces: []api.NamespaceSnapshot{{
			Namespace: "team/\"a\\b\nc",
			Memory:    model.MemoryBreakdown{TotalBytes: 1},
		}},
		debug: api.DebugStore{Namespaces: 1},
	}
	out, err := (Exporter{
		Source: source,
		TTL:    time.Minute,
		Now:    fixedNow,
		Opts:   DefaultOptions(),
	}).Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	assertContains(t, out, `namespace="team/\"a\\b\nc"`)
}

func renderForTest(t *testing.T, opts Options) string {
	t.Helper()
	now := fixedNow()
	source := testSource{
		containers: []api.ContainerSnapshot{{
			Namespace:     "default",
			PodName:       "api",
			PodUID:        "pod-uid-123",
			ContainerName: "app",
			ContainerID:   "container-123",
			NodeName:      "node-a",
			CgroupPath:    "/host/sys/fs/cgroup/kubepods/pod-uid/container-123",
			CapturedAt:    now.Add(-5 * time.Second),
			Context:       api.ContainerContext{Labels: map[string]string{"customer": "customer-secret"}},
			Memory:        memory(300),
		}},
		pods: []api.PodSnapshot{{
			Namespace:  "default",
			PodName:    "api",
			PodUID:     "pod-uid-123",
			NodeName:   "node-a",
			CapturedAt: now.Add(-5 * time.Second),
			Context:    api.PodContext{Labels: map[string]string{"customer": "customer-secret"}},
			Memory:     memory(300),
		}},
		namespaces: []api.NamespaceSnapshot{{
			Namespace:  "default",
			CapturedAt: now.Add(-5 * time.Second),
			PodCount:   1,
			Memory:     memory(300),
		}},
		debug: api.DebugStore{TotalContainers: 1, Pods: 1, Namespaces: 1},
		latest: map[string]time.Time{
			"node-a": now.Add(-5 * time.Second),
		},
	}
	out, err := (Exporter{Source: source, TTL: time.Minute, Now: fixedNow, Opts: opts}).Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	return out
}

func memory(total uint64) model.MemoryBreakdown {
	return model.MemoryBreakdown{
		Name:                   "/var/lib/app/cache/file.db",
		TotalBytes:             total,
		AnonBytes:              total / 2,
		FileBytes:              total / 4,
		ActiveFileBytes:        10,
		InactiveFileBytes:      20,
		ShmemBytes:             30,
		SlabBytes:              40,
		SlabReclaimableBytes:   25,
		SlabUnreclaimableBytes: 15,
		KernelBytes:            50,
		SocketBytes:            7,
		PageTableBytes:         8,
		FileMappedBytes:        9,
		AnonTHPBytes:           11,
		FileTHPBytes:           12,
		ShmemTHPBytes:          13,
		DirtyBytes:             60,
		WritebackBytes:         70,
		PeakBytes:              total + 150,
		PeakKnown:              true,
		MaxBytes:               total * 2,
		MaxKnown:               true,
		LocalEventsKnown:       true,
		LocalEventDeltasKnown:  true,
		LocalHighEvents:        8,
		LocalHighEventsDelta:   1,
		OOMEvents:              2,
		OOMKillEvents:          1,
		HighEvents:             3,
		MaxEvents:              4,
	}
}

func fixedNow() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}

type testSource struct {
	containers []api.ContainerSnapshot
	pods       []api.PodSnapshot
	namespaces []api.NamespaceSnapshot
	debug      api.DebugStore
	latest     map[string]time.Time
}

func (s testSource) ListContainers(time.Time, time.Duration) []api.ContainerSnapshot {
	return s.containers
}

func (s testSource) ListPods(time.Time, time.Duration) []api.PodSnapshot {
	return s.pods
}

func (s testSource) ListNamespaces(time.Time, time.Duration) []api.NamespaceSnapshot {
	return s.namespaces
}

func (s testSource) Debug(time.Time, time.Duration) api.DebugStore {
	return s.debug
}

func (s testSource) LatestByNode(time.Time) map[string]time.Time {
	return s.latest
}

func assertContains(t *testing.T, value string, needle string) {
	t.Helper()
	if !strings.Contains(value, needle) {
		t.Fatalf("output does not contain %q:\n%s", needle, value)
	}
}

func assertNotContains(t *testing.T, value string, needle string) {
	t.Helper()
	if strings.Contains(value, needle) {
		t.Fatalf("output contains %q:\n%s", needle, value)
	}
}
