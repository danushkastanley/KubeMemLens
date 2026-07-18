package collector

import (
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestAggregateWorkloadsGroupsReplicasAndKeepsOutliers(t *testing.T) {
	pods := []api.PodSnapshot{
		workloadPod("api-a", "Deployment", "api", 100),
		workloadPod("api-b", "Deployment", "api", 300),
		workloadPod("worker-a", "StatefulSet", "worker", 50),
	}
	workloads := aggregateWorkloads(pods)
	if len(workloads) != 2 {
		t.Fatalf("workloads = %d, want 2: %#v", len(workloads), workloads)
	}
	apiWorkload := workloads[0]
	if apiWorkload.Kind != "Deployment" || apiWorkload.Name != "api" || apiWorkload.PodCount != 2 {
		t.Fatalf("unexpected API workload: %#v", apiWorkload)
	}
	if apiWorkload.Memory.TotalBytes != 400 || apiWorkload.LargestPodName != "api-b" || apiWorkload.LargestPodBytes != 300 {
		t.Fatalf("unexpected API workload roll-up: %#v", apiWorkload)
	}
	if len(apiWorkload.Pods) != 2 || apiWorkload.Pods[0].PodName != "api-b" {
		t.Fatalf("replica outliers were not retained in descending order: %#v", apiWorkload.Pods)
	}
}

func TestAggregateWorkloadsFallsBackToStandalonePod(t *testing.T) {
	workloads := aggregateWorkloads([]api.PodSnapshot{{Namespace: "default", PodName: "debug", Memory: model.MemoryBreakdown{TotalBytes: 1}}})
	if len(workloads) != 1 || workloads[0].Kind != "Pod" || workloads[0].Name != "debug" {
		t.Fatalf("unexpected standalone Pod workload: %#v", workloads)
	}
}

func TestAggregateWorkloadsDoesNotClaimPartialLimitOrSyntheticPeak(t *testing.T) {
	first := workloadPod("api-a", "Deployment", "api", 100)
	first.Memory.MaxKnown, first.Memory.MaxBytes = true, 200
	first.Memory.PeakKnown, first.Memory.PeakBytes = true, 150
	second := workloadPod("api-b", "Deployment", "api", 100)
	workload := aggregateWorkloads([]api.PodSnapshot{first, second})[0]
	if workload.Memory.MaxKnown || workload.Memory.PeakKnown {
		t.Fatalf("partial limit or synthetic peak reported: %#v", workload.Memory)
	}
}

func workloadPod(name, kind, workload string, total uint64) api.PodSnapshot {
	return api.PodSnapshot{
		Namespace: "default", PodName: name,
		Context: api.PodContext{WorkloadKind: kind, WorkloadName: workload},
		Memory:  model.MemoryBreakdown{TotalBytes: total},
	}
}
