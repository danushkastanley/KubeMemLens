package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestFilterPods(t *testing.T) {
	pods := []api.PodSnapshot{
		{Namespace: "default", PodName: "api", NodeName: "node-a", Memory: model.MemoryBreakdown{TotalBytes: 100}},
		{Namespace: "kube-system", PodName: "coredns", NodeName: "node-a", Memory: model.MemoryBreakdown{TotalBytes: 200}},
	}

	got := FilterPods(pods, "default", false, "api")
	if len(got) != 1 || got[0].PodName != "api" {
		t.Fatalf("FilterPods returned %#v", got)
	}

	got = FilterPods(pods, "", true, "rss")
	if len(got) != 0 {
		t.Fatalf("FilterPods diagnosis filter returned %#v, want none", got)
	}
}

func TestFilterContainers(t *testing.T) {
	containers := []api.ContainerSnapshot{
		{Namespace: "default", PodName: "api", ContainerName: "app", NodeName: "node-a"},
		{Namespace: "default", PodName: "api", ContainerName: "sidecar", NodeName: "node-a"},
		{ContainerName: "", NodeName: "node-a"},
	}

	got := FilterContainers(containers, "default", false, "side")
	if len(got) != 1 || got[0].ContainerName != "sidecar" {
		t.Fatalf("FilterContainers returned %#v", got)
	}
}

func TestSortPodsByTotalAndRSS(t *testing.T) {
	pods := []api.PodSnapshot{
		{PodName: "small", Memory: model.MemoryBreakdown{TotalBytes: 10, AnonBytes: 100}},
		{PodName: "large", Memory: model.MemoryBreakdown{TotalBytes: 20, AnonBytes: 50}},
	}

	SortPods(pods, sortTotal)
	if pods[0].PodName != "large" {
		t.Fatalf("sortTotal first = %s, want large", pods[0].PodName)
	}

	SortPods(pods, sortRSS)
	if pods[0].PodName != "small" {
		t.Fatalf("sortRSS first = %s, want small", pods[0].PodName)
	}
}

func TestSortNamespacesByCache(t *testing.T) {
	namespaces := []api.NamespaceSnapshot{
		{Namespace: "a", Memory: model.MemoryBreakdown{FileBytes: 10}},
		{Namespace: "b", Memory: model.MemoryBreakdown{FileBytes: 20}},
	}

	SortNamespaces(namespaces, sortCache)
	if namespaces[0].Namespace != "b" {
		t.Fatalf("sortCache first = %s, want b", namespaces[0].Namespace)
	}
}

func TestFilterAndSortWorkloads(t *testing.T) {
	workloads := []api.WorkloadSnapshot{
		{Namespace: "default", Kind: "Deployment", Name: "api", LargestPodName: "api-b", Memory: model.MemoryBreakdown{TotalBytes: 200}},
		{Namespace: "default", Kind: "StatefulSet", Name: "worker", Memory: model.MemoryBreakdown{TotalBytes: 100}},
	}
	filtered := FilterWorkloads(workloads, "default", false, "deploy")
	if len(filtered) != 1 || filtered[0].Name != "api" {
		t.Fatalf("unexpected workloads: %#v", filtered)
	}
	SortWorkloads(workloads, sortTotal)
	if workloads[0].Name != "api" {
		t.Fatalf("largest workload = %s, want api", workloads[0].Name)
	}
}

func TestHistoryTrendShowsShapeAndEvents(t *testing.T) {
	pod := api.PodSnapshot{PodUID: "uid-a", NodeName: "node-a"}
	lines := renderHistoryTrend(pod, []api.PodHistory{{
		PodUID: "uid-a", NodeName: "node-a",
		Points: []api.MemoryHistoryPoint{
			{TotalBytes: 1 << 20},
			{TotalBytes: 2 << 20, HighEventsDelta: 1},
			{TotalBytes: 3 << 20, OOMKillEventsDelta: 1, PSISomeAvg10: 2.5, ReclaimDeltasKnown: true, PageScanDelta: 10, PageStealDelta: 8, RefaultFileDelta: 2},
		},
	}}, 100)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"▁▄█", "Δ +2Mi", "kill=1", "high=1", "2.50", "efficiency=80%", "refault=2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("history trend missing %q: %s", want, joined)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("truncate = %q, want abc…", got)
	}
	if got := truncate("abcdef", 20); got != "abcdef" {
		t.Fatalf("truncate = %q, want abcdef", got)
	}
}

func TestFormatAge(t *testing.T) {
	got := FormatAge(time.Now().Add(-2 * time.Minute))
	if got != "2m" {
		t.Fatalf("FormatAge = %q, want 2m", got)
	}
}
