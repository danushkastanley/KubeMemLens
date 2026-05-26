package collector

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestStoreUpsertAndListContainers(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.UpsertSnapshot(api.AgentSnapshot{
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
	store.UpsertSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "id-a", now, 100),
	}})
	store.UpsertSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{
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

func TestStoreAggregatesPodsAndNamespaces(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore()
	store.UpsertSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "pod-a", "app", "id-a", now, 100),
		container("default", "pod-a", "sidecar", "id-b", now, 50),
		container("kube-system", "pod-b", "app", "id-c", now, 200),
	}})

	pods := store.ListPods(now, time.Minute)
	if len(pods) != 2 {
		t.Fatalf("pods = %d, want 2", len(pods))
	}
	if pods[0].Memory.TotalBytes != 150 {
		t.Fatalf("pod total = %d, want 150", pods[0].Memory.TotalBytes)
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
	store.UpsertSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(-time.Minute), Containers: []api.ContainerSnapshot{
		container("default", "old", "app", "id-a", now.Add(-time.Minute), 100),
	}})
	store.UpsertSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		container("default", "new", "app", "id-b", now, 200),
	}})

	items := store.ListContainers(now, 30*time.Second)
	if len(items) != 1 || items[0].PodName != "new" {
		t.Fatalf("unexpected non-stale containers: %#v", items)
	}

	debug := store.Debug(now, 30*time.Second)
	if debug.TotalContainers != 2 || debug.StaleContainers != 1 || debug.Pods != 1 || debug.Namespaces != 1 {
		t.Fatalf("unexpected debug counts: %#v", debug)
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
