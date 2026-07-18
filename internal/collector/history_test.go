package collector

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestHistoryIsBoundedAndUsesEventDeltas(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: time.Hour, MaxSeries: 10, MaxPoints: 2})
	for i := 0; i < 3; i++ {
		snapshot := api.ContainerSnapshot{
			Namespace: "default", PodName: "api", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", NodeName: "node-a",
			Memory: model.MemoryBreakdown{TotalBytes: uint64(100 + i), AnonBytes: 50, FileBytes: 20, ShmemBytes: 5, SlabReclaimableBytes: 3, SlabUnreclaimableBytes: 2, KernelBytes: 10, SocketBytes: 1, PageTableBytes: 2, FileMappedBytes: 4, AnonTHPBytes: 5, FileTHPBytes: 6, ShmemTHPBytes: 7, LocalEventsKnown: true, LocalHighEvents: uint64(i), ReclaimCountersKnown: true, PageScan: uint64(i * 10), PageSteal: uint64(i * 8), WorkingsetRefaultFile: uint64(i * 2)},
		}
		if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Duration(i) * time.Second), Containers: []api.ContainerSnapshot{snapshot}}); err != nil {
			t.Fatalf("ReplaceNodeSnapshot returned error: %v", err)
		}
	}
	history := store.ListPodHistory("default", "api", "", now.Add(2*time.Second))
	if len(history) != 1 || len(history[0].Points) != 2 {
		t.Fatalf("unexpected bounded history: %#v", history)
	}
	if history[0].Points[0].TotalBytes != 101 || history[0].Points[1].TotalBytes != 102 {
		t.Fatalf("unexpected retained points: %#v", history[0].Points)
	}
	point := history[0].Points[1]
	if point.ResidualBytes != 32 || point.SlabReclaimableBytes != 3 || point.SlabUnreclaimableBytes != 2 || point.SocketBytes != 1 || point.PageTableBytes != 2 || point.FileMappedBytes != 4 || point.AnonTHPBytes != 5 || point.FileTHPBytes != 6 || point.ShmemTHPBytes != 7 {
		t.Fatalf("unexpected composition history: %#v", point)
	}
	if history[0].Points[1].HighEventsDelta != 1 {
		t.Fatalf("HighEventsDelta = %d, want 1", history[0].Points[1].HighEventsDelta)
	}
	if !history[0].Points[1].ReclaimDeltasKnown || history[0].Points[1].PageScanDelta != 10 || history[0].Points[1].PageStealDelta != 8 || history[0].Points[1].RefaultFileDelta != 2 {
		t.Fatalf("unexpected reclaim history: %#v", history[0].Points[1])
	}
}

func TestHistoryPrunesExpiredSeries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: time.Minute, MaxSeries: 10, MaxPoints: 10})
	snapshot := api.ContainerSnapshot{Namespace: "default", PodName: "api", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", NodeName: "node-a"}
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{snapshot}})
	if got := store.ListPodHistory("default", "api", "", now.Add(2*time.Minute)); len(got) != 0 {
		t.Fatalf("expired history remains: %#v", got)
	}
}

func TestHistoryResponseKeepsNewestSeriesWithinLimit(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: time.Hour, MaxSeries: 10, MaxPoints: 10, MaxResponseSeries: 2})
	for i, uid := range []string{"uid-old", "uid-middle", "uid-new"} {
		snapshot := api.ContainerSnapshot{Namespace: "default", PodName: "api", PodUID: uid, ContainerName: "app", ContainerID: uid, NodeName: "node-a"}
		_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base.Add(time.Duration(i) * time.Second), Containers: []api.ContainerSnapshot{snapshot}})
	}
	history := store.ListPodHistory("default", "api", "", base.Add(3*time.Second))
	if len(history) != 2 {
		t.Fatalf("history series = %d, want 2", len(history))
	}
	if history[0].PodUID != "uid-new" || history[1].PodUID != "uid-middle" {
		t.Fatalf("unexpected response order: %#v", history)
	}
}
