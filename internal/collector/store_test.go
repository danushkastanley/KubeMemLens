package collector

import (
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestStoreReplaceAndListContainers(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName:   "node-a",
		CapturedAt: now,
		Containers: []api.ContainerSnapshot{
			container("default", "pod-a", "app", "id-a", now, 100),
		},
	})

	items := store.ListContainers(now, time.Minute)
	if len(items) != 1 {
		t.Fatalf("containers = %d, want 1", len(items))
	}
	if items[0].NodeName != "node-a" {
		t.Fatalf("NodeName = %q, want node-a", items[0].NodeName)
	}
}

func TestStoreUpdateReplacesContainer(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "id-a", now, 100),
	}})
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "id-a", now.Add(time.Second), 250),
	}})

	items := store.ListContainers(now.Add(time.Second), time.Minute)
	if len(items) != 1 {
		t.Fatalf("containers = %d, want 1", len(items))
	}
	if items[0].Memory.TotalBytes != 250 {
		t.Fatalf("TotalBytes = %d, want 250", items[0].Memory.TotalBytes)
	}
}

func TestStoreReplacementRemovesContainersMissingFromNodeSnapshot(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "old-id", now, 100),
		container("default", "finished-pod", "job", "finished-id", now, 50),
	}})
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "new-id", now.Add(time.Second), 125),
	}})

	items := store.ListContainers(now.Add(time.Second), time.Minute)
	if len(items) != 1 {
		t.Fatalf("containers = %d, want 1: %#v", len(items), items)
	}
	if items[0].ContainerID != "new-id" {
		t.Fatalf("ContainerID = %q, want new-id", items[0].ContainerID)
	}
}

func TestStoreEmptySnapshotClearsOnlyReportingNode(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "id-a", now, 100),
	}})
	nodeB := container("default", "pod-b", "app", "id-b", now, 200)
	nodeB.NodeName = "node-b"
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-b", CapturedAt: now, Containers: []api.ContainerSnapshot{nodeB}})

	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second)})

	items := store.ListContainers(now.Add(time.Second), time.Minute)
	if len(items) != 1 || items[0].NodeName != "node-b" {
		t.Fatalf("unexpected containers after empty replacement: %#v", items)
	}
	latest := store.LatestByNode(now.Add(time.Second))
	if !latest["node-a"].Equal(now.Add(time.Second)) {
		t.Fatalf("node-a latest = %s, want %s", latest["node-a"], now.Add(time.Second))
	}
}

func TestStoreDerivesEventDeltasAcrossNodeSnapshots(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	first := container("default", "pod-a", "app", "id-a", now, 100)
	first.Memory.OOMEvents = 3
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{first}})

	items := store.ListContainers(now, time.Minute)
	if len(items) != 1 || !items[0].Memory.EventDeltasKnown || items[0].Memory.HasOOMRisk() {
		t.Fatalf("first snapshot should establish a baseline without recent OOM risk: %#v", items)
	}
	if items[0].DeltaWindowKnown || !items[0].DeltaStartedAt.IsZero() {
		t.Fatalf("first snapshot should not claim an elapsed counter window: %#v", items[0])
	}

	second := first
	second.Memory.OOMEvents = 4
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{second}})
	items = store.ListContainers(now.Add(time.Second), time.Minute)
	if len(items) != 1 || items[0].Memory.OOMEventsDelta != 1 || !items[0].Memory.HasOOMRisk() {
		t.Fatalf("second snapshot should expose one recent OOM event: %#v", items)
	}
	if !items[0].DeltaWindowKnown || !items[0].DeltaStartedAt.Equal(now) {
		t.Fatalf("second snapshot counter window = %#v, want start %s", items[0], now)
	}

	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{second}})
	items = store.ListContainers(now.Add(time.Second), time.Minute)
	if items[0].Memory.OOMEventsDelta != 1 {
		t.Fatalf("equal-timestamp retry should preserve the stored delta: %#v", items[0].Memory)
	}
}

func TestStoreRejectsOutOfOrderNodeSnapshot(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	newer := container("default", "pod-a", "app", "new-id", now, 200)
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{newer}}); err != nil {
		t.Fatalf("store newer snapshot: %v", err)
	}
	older := container("default", "pod-a", "app", "old-id", now.Add(-time.Second), 100)
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(-time.Second), Containers: []api.ContainerSnapshot{older}}); !errors.Is(err, ErrSnapshotOutOfOrder) {
		t.Fatalf("ReplaceNodeSnapshot error = %v, want ErrSnapshotOutOfOrder", err)
	}

	items := store.ListContainers(now, time.Minute)
	if len(items) != 1 || items[0].ContainerID != "new-id" {
		t.Fatalf("out-of-order snapshot replaced current state: %#v", items)
	}
}

func TestStoreEnforcesNodeAndContainerCapacity(t *testing.T) {
	now := time.Now().UTC()
	store := NewStoreWithHistoryAndLimits(DefaultHistoryOptions(), StoreLimits{MaxNodes: 1, MaxContainers: 1})
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "id-a", now, 100),
	}}); err != nil {
		t.Fatalf("store first snapshot: %v", err)
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-b", CapturedAt: now}); !errors.Is(err, ErrStoreCapacity) {
		t.Fatalf("new node error = %v, want ErrStoreCapacity", err)
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "id-a", now, 100),
		container("default", "pod-b", "app", "id-b", now, 100),
	}}); !errors.Is(err, ErrStoreCapacity) {
		t.Fatalf("container expansion error = %v, want ErrStoreCapacity", err)
	}
	if got := store.ListContainers(now.Add(time.Second), time.Minute); len(got) != 1 || got[0].ContainerID != "id-a" {
		t.Fatalf("rejected snapshot changed stored state: %#v", got)
	}
}

func TestStoreReleasesContainerCapacityWhenSnapshotExpires(t *testing.T) {
	now := time.Now().UTC()
	store := NewStoreWithHistoryAndLimits(DefaultHistoryOptions(), StoreLimits{MaxNodes: 2, MaxContainers: 1})
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "id-a", now, 100),
	}})
	if got := store.ListContainers(now.Add(2*time.Minute), time.Minute); len(got) != 0 {
		t.Fatalf("expired containers remain visible: %#v", got)
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-b", CapturedAt: now.Add(2 * time.Minute), Containers: []api.ContainerSnapshot{
		container("default", "pod-b", "app", "id-b", now.Add(2*time.Minute), 100),
	}}); err != nil {
		t.Fatalf("capacity was not released: %v", err)
	}
}

func TestStoreAggregatesPodsAndNamespaces(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	app := container("default", "pod-a", "app", "id-a", now, 100)
	app.Context = api.ContainerContext{MemoryRequestKnown: true, MemoryRequestBytes: 80, MemoryLimitKnown: true, MemoryLimitBytes: 200, QoSClass: "Burstable", RestartCount: 1, PodPhase: "Running", PodCreatedAt: now.Add(-time.Hour), OwnerKind: "ReplicaSet", OwnerName: "pod-a-rs"}
	sidecar := container("default", "pod-a", "sidecar", "id-b", now, 50)
	sidecar.Context = api.ContainerContext{MemoryRequestKnown: true, MemoryRequestBytes: 20, RestartCount: 2, QoSClass: "Burstable", PodPhase: "Running", PodCreatedAt: now.Add(-time.Hour), OwnerKind: "ReplicaSet", OwnerName: "pod-a-rs"}
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		app,
		sidecar,
		container("kube-system", "pod-b", "app", "id-c", now, 200),
	}})

	pods := store.ListPods(now, time.Minute)
	if len(pods) != 2 {
		t.Fatalf("pods = %d, want 2", len(pods))
	}
	if pods[0].Memory.TotalBytes != 150 {
		t.Fatalf("pod total = %d, want 150", pods[0].Memory.TotalBytes)
	}
	if pods[0].Context.MemoryRequestBytes != 100 || pods[0].Context.MemoryRequestContainers != 2 {
		t.Fatalf("unexpected Pod requests: %#v", pods[0].Context)
	}
	if pods[0].Context.MemoryLimitBytes != 200 || pods[0].Context.MemoryLimitContainers != 1 || pods[0].Context.RestartCount != 3 {
		t.Fatalf("unexpected Pod limits/restarts: %#v", pods[0].Context)
	}
	if pods[0].Context.OwnerKind != "ReplicaSet" || pods[0].Context.QoSClass != "Burstable" {
		t.Fatalf("unexpected Pod identity context: %#v", pods[0].Context)
	}

	namespaces := store.ListNamespaces(now, time.Minute)
	if len(namespaces) != 2 {
		t.Fatalf("namespaces = %d, want 2", len(namespaces))
	}
	if namespaces[0].Namespace != "default" || namespaces[0].PodCount != 1 || namespaces[0].Memory.TotalBytes != 150 {
		t.Fatalf("unexpected default namespace aggregate: %#v", namespaces[0])
	}
}

func TestStoreFiltersStaleSnapshots(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(-time.Minute), Containers: []api.ContainerSnapshot{
		container("default", "old", "app", "id-a", now.Add(-time.Minute), 100),
	}})
	fresh := container("default", "new", "app", "id-b", now, 200)
	fresh.NodeName = "node-b"
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-b", CapturedAt: now, Containers: []api.ContainerSnapshot{
		fresh,
	}})

	items := store.ListContainers(now, 30*time.Second)
	if len(items) != 1 || items[0].PodName != "new" {
		t.Fatalf("unexpected non-stale containers: %#v", items)
	}

	debug := store.Debug(now, 30*time.Second)
	if debug.TotalContainers != 1 || debug.StaleContainers != 0 || debug.Pods != 1 || debug.Namespaces != 1 {
		t.Fatalf("unexpected debug counts: %#v", debug)
	}
	latest := store.LatestByNode(now)
	if len(latest) != 2 || !latest["node-a"].Equal(now.Add(-time.Minute)) {
		t.Fatalf("latest snapshots should retain node freshness after container GC: %#v", latest)
	}
}

func TestStoreListsFreshAndStaleNodeCoverage(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-b", CapturedAt: now})
	store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(-time.Minute), Containers: []api.ContainerSnapshot{
		container("default", "old", "app", "id-a", now.Add(-time.Minute), 100),
	}})

	nodes := store.ListNodes(now, 30*time.Second)
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want 2: %#v", len(nodes), nodes)
	}
	if nodes[0].NodeName != "node-a" || !nodes[0].Stale || nodes[0].ContainerCount != 1 {
		t.Fatalf("unexpected node-a status: %#v", nodes[0])
	}
	if nodes[1].NodeName != "node-b" || nodes[1].Stale || nodes[1].ContainerCount != 0 {
		t.Fatalf("unexpected node-b status: %#v", nodes[1])
	}
}

func container(namespace, pod, name, id string, capturedAt time.Time, total uint64) api.ContainerSnapshot {
	return api.ContainerSnapshot{
		Namespace:     namespace,
		PodName:       pod,
		PodUID:        namespace + "-" + pod + "-uid",
		ContainerName: name,
		ContainerID:   id,
		NodeName:      "node-a",
		CapturedAt:    capturedAt,
		Memory: model.MemoryBreakdown{
			Name:       namespace + "/" + pod + "/" + name,
			TotalBytes: total,
			AnonBytes:  total / 2,
			FileBytes:  total / 4,
		},
	}
}
