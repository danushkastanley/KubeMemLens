package tui

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestRiskSortPrioritisesRecentEvidence(t *testing.T) {
	pods := []api.PodSnapshot{
		{Namespace: "default", PodName: "large", Memory: model.MemoryBreakdown{TotalBytes: 1000, EventDeltasKnown: true}},
		{Namespace: "default", PodName: "oom", Memory: model.MemoryBreakdown{TotalBytes: 10, EventDeltasKnown: true, OOMKillEventsDelta: 1}},
		{Namespace: "default", PodName: "pressure", Memory: model.MemoryBreakdown{TotalBytes: 100, EventDeltasKnown: true, PressureKnown: true, PSIFullAvg10: 0.1}},
	}
	SortPodsAt(pods, sortRisk, time.Now(), time.Minute)
	if pods[0].PodName != "oom" || pods[1].PodName != "pressure" {
		t.Fatalf("risk order = %s, %s, %s", pods[0].PodName, pods[1].PodName, pods[2].PodName)
	}
}

func TestRiskSortIsDeterministicAcrossInputOrder(t *testing.T) {
	base := []api.PodSnapshot{
		{Namespace: "b", PodName: "two", Memory: model.MemoryBreakdown{TotalBytes: 10, EventDeltasKnown: true}},
		{Namespace: "a", PodName: "one", Memory: model.MemoryBreakdown{TotalBytes: 10, EventDeltasKnown: true}},
		{Namespace: "a", PodName: "three", Memory: model.MemoryBreakdown{TotalBytes: 10, EventDeltasKnown: true}},
	}
	want := []string{"a/one", "a/three", "b/two"}
	for seed := int64(0); seed < 20; seed++ {
		items := append([]api.PodSnapshot(nil), base...)
		rand.New(rand.NewSource(seed)).Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
		SortPodsAt(items, sortRisk, time.Now(), time.Minute)
		for index, pod := range items {
			if got := pod.Namespace + "/" + pod.PodName; got != want[index] {
				t.Fatalf("seed %d index %d = %q, want %q", seed, index, got, want[index])
			}
		}
	}
}

func TestStructuredPodFiltersComposeWithText(t *testing.T) {
	now := time.Now()
	pods := []api.PodSnapshot{
		{
			Namespace: "default", PodName: "api-danger", CapturedAt: now,
			Context: api.PodContext{WorkloadKind: "Deployment", WorkloadName: "api"},
			Memory:  model.MemoryBreakdown{EventDeltasKnown: true, OOMKillEventsDelta: 1},
		},
		{
			Namespace: "default", PodName: "worker", CapturedAt: now,
			Context: api.PodContext{WorkloadKind: "Deployment", WorkloadName: "worker"},
			Memory:  model.MemoryBreakdown{EventDeltasKnown: true},
		},
	}
	got := FilterPodsAt(pods, "default", false, "api severity:critical owner:api state:incomplete", now, time.Minute)
	if len(got) != 1 || got[0].PodName != "api-danger" {
		t.Fatalf("filtered Pods = %#v", got)
	}
}

func TestStaleFilterDoesNotTreatZeroAsFreshEvidence(t *testing.T) {
	now := time.Now()
	pods := []api.PodSnapshot{
		{Namespace: "default", PodName: "stale", CapturedAt: now.Add(-time.Minute)},
		{Namespace: "default", PodName: "fresh", CapturedAt: now},
	}
	got := FilterPodsAt(pods, "", true, "state:stale", now, 15*time.Second)
	if len(got) != 1 || got[0].PodName != "stale" {
		t.Fatalf("stale filter = %#v", got)
	}
	if risk := podRisk(got[0], now, 15*time.Second); risk.label != "STALE" {
		t.Fatalf("stale risk = %#v", risk)
	}
}

func TestServerFreshnessOverridesLocalRefreshHeuristic(t *testing.T) {
	now := time.Now()
	pods := []api.PodSnapshot{
		{Namespace: "default", PodName: "authoritative-fresh", CapturedAt: now.Add(-time.Minute), Freshness: api.EvidenceFreshnessFresh},
		{Namespace: "default", PodName: "authoritative-stale", CapturedAt: now, Freshness: api.EvidenceFreshnessStale},
	}
	stale := FilterPodsAt(pods, "", true, "state:stale", now, 15*time.Second)
	if len(stale) != 1 || stale[0].PodName != "authoritative-stale" {
		t.Fatalf("authoritative stale filter = %#v", stale)
	}
	if risk := podRisk(pods[0], now, 15*time.Second); risk.stale {
		t.Fatalf("fresh server evidence was relabelled stale: %#v", risk)
	}
}

func BenchmarkRiskSortAndFilterTenThousandPods(b *testing.B) {
	now := time.Now()
	base := make([]api.PodSnapshot, 10_000)
	for index := range base {
		base[index] = api.PodSnapshot{
			Namespace: "load", PodName: fmt.Sprintf("pod-%05d", index), CapturedAt: now,
			Memory: model.MemoryBreakdown{TotalBytes: uint64(index + 1), EventDeltasKnown: true},
		}
	}
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		items := FilterPodsAt(base, "", true, "state:fresh", now, time.Minute)
		SortPodsAt(items, sortRisk, now, time.Minute)
	}
}
