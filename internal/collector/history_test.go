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
	reliability := store.HistoryReliability(now.Add(2 * time.Minute))
	if reliability.Completeness != api.EvidencePartial || !reliability.AvailableFrom.IsZero() {
		t.Fatalf("empty history was presented as complete: %#v", reliability)
	}
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(2 * time.Minute), Containers: []api.ContainerSnapshot{snapshot}})
	if got := store.HistoryReliability(now.Add(2 * time.Minute)); got.Completeness != api.EvidencePartial {
		t.Fatalf("one point after a gap was presented as complete: %#v", got)
	}
}

func TestScopedHistoryReliabilityDoesNotExposeOtherNamespaceLoss(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: time.Hour, MaxSeries: 10, MaxPoints: 2})
	for index := range 3 {
		teamA := api.ContainerSnapshot{Namespace: "team-a", PodName: "api", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", NodeName: "node-a"}
		containers := []api.ContainerSnapshot{teamA}
		if index == 2 {
			containers = append(containers, api.ContainerSnapshot{Namespace: "team-b", PodName: "api", PodUID: "uid-b", ContainerName: "app", ContainerID: "id-b", NodeName: "node-a"})
		}
		_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base.Add(time.Duration(index) * time.Second), Containers: containers})
	}
	_, teamB := store.ListPodHistoryWithReliability("team-b", "api", "", base.Add(2*time.Second))
	if teamB.DroppedSeries != 0 || teamB.EvictedPoints != 0 || !teamB.LastLossAt.IsZero() {
		t.Fatalf("unexpected team-b history reliability: %#v", teamB)
	}
	_, teamA := store.ListPodHistoryWithReliability("team-a", "api", "", base.Add(2*time.Second))
	if teamA.EvictedPoints != 1 {
		t.Fatalf("team-a scoped loss missing: %#v", teamA)
	}
}

func TestScopedHistoryReportsBoundedSeriesRejection(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: time.Hour, MaxSeries: 1, MaxPoints: 10})
	teamA := api.ContainerSnapshot{Namespace: "team-a", PodName: "api", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", NodeName: "node-a"}
	teamB := api.ContainerSnapshot{Namespace: "team-b", PodName: "api", PodUID: "uid-b", ContainerName: "app", ContainerID: "id-b", NodeName: "node-a"}
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base, Containers: []api.ContainerSnapshot{teamA}})
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base.Add(time.Second), Containers: []api.ContainerSnapshot{teamA, teamB}})
	series, reliability := store.ListPodHistoryWithReliability("team-b", "api", "", base.Add(time.Second))
	if len(series) != 0 || reliability.DroppedSeries != 1 || !reliability.LastLossAt.Equal(base.Add(time.Second)) {
		t.Fatalf("scoped series rejection = series=%#v reliability=%#v", series, reliability)
	}
	_, teamAStatus := store.ListPodHistoryWithReliability("team-a", "api", "", base.Add(time.Second))
	if teamAStatus.DroppedSeries != 0 || !teamAStatus.LastLossAt.IsZero() {
		t.Fatalf("team-a saw team-b loss: %#v", teamAStatus)
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

func TestHistoryReliabilityRecoversAfterCapacityLossLeavesWindow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: 2 * time.Second, MaxSeries: 10, MaxPoints: 2})
	snapshot := api.ContainerSnapshot{Namespace: "default", PodName: "api", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", NodeName: "node-a"}
	for _, offset := range []time.Duration{0, time.Second, 2 * time.Second} {
		_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base.Add(offset), Containers: []api.ContainerSnapshot{snapshot}})
	}
	loss := store.HistoryReliability(base.Add(2 * time.Second))
	if loss.Completeness != api.EvidencePartial || loss.EvictedPoints != 1 || !loss.LastLossAt.Equal(base.Add(2*time.Second)) {
		t.Fatalf("history loss was not explicit: %#v", loss)
	}

	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base.Add(4 * time.Second), Containers: []api.ContainerSnapshot{snapshot}})
	recovered := store.HistoryReliability(base.Add(4 * time.Second))
	if recovered.Completeness != api.EvidenceComplete || recovered.EvictedPoints != 1 {
		t.Fatalf("history completeness did not recover after loss aged out: %#v", recovered)
	}
}

func TestDefaultHistoryCapacityCoversInclusiveFiveSecondWindow(t *testing.T) {
	options := DefaultHistoryOptions()
	if options.Duration != 15*time.Minute || options.MaxPoints != 181 {
		t.Fatalf("default history options do not cover inclusive five-second endpoints: %#v", options)
	}
}

func TestHistoryGapResetsScopedCompleteness(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: time.Minute, MaxSeries: 10, MaxPoints: 100, ContinuityGap: 10 * time.Second})
	snapshot := api.ContainerSnapshot{Namespace: "default", PodName: "api", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", NodeName: "node-a"}
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base, Containers: []api.ContainerSnapshot{snapshot}})
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base.Add(time.Minute), Containers: []api.ContainerSnapshot{snapshot}})
	_, reliability := store.ListPodHistoryWithReliability("default", "api", "", base.Add(time.Minute))
	if reliability.Completeness != api.EvidencePartial {
		t.Fatalf("history gap was presented as complete: %#v", reliability)
	}
}

func TestHistoryWithoutCurrentTailNeverBecomesComplete(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	store := NewStoreWithHistory(HistoryOptions{Duration: time.Minute, MaxSeries: 10, MaxPoints: 100, ContinuityGap: 10 * time.Second})
	snapshot := api.ContainerSnapshot{Namespace: "default", PodName: "api", PodUID: "uid-a", ContainerName: "app", ContainerID: "id-a", NodeName: "node-a"}
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: base, Containers: []api.ContainerSnapshot{snapshot}})
	_, reliability := store.ListPodHistoryWithReliability("default", "api", "", base.Add(time.Minute))
	if reliability.Completeness != api.EvidencePartial {
		t.Fatalf("history without a current tail was presented as complete: %#v", reliability)
	}
}
