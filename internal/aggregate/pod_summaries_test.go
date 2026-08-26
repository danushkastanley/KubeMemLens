package aggregate

import (
	"reflect"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestPodSummaryAccumulatorMatchesPodAggregateWithoutContainers(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	containers := []api.ContainerSnapshot{
		{
			Namespace: "team-a", PodName: "api", PodUID: "uid-a", NodeName: "worker-a", ContainerName: "app",
			CapturedAt: now, Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidenceComplete,
			Context: api.ContainerContext{MemoryRequestKnown: true, MemoryRequestBytes: 64 << 20, WorkloadKind: "Deployment", WorkloadName: "api"},
			Memory:  model.MemoryBreakdown{TotalBytes: 80 << 20, AnonBytes: 64 << 20},
		},
		{
			Namespace: "team-a", PodName: "api", PodUID: "uid-a", NodeName: "worker-a", ContainerName: "sidecar",
			CapturedAt: now.Add(time.Second), Freshness: api.EvidenceFreshnessStale, Completeness: api.EvidenceComplete,
			Context: api.ContainerContext{MemoryLimitKnown: true, MemoryLimitBytes: 128 << 20},
			Memory:  model.MemoryBreakdown{TotalBytes: 32 << 20, FileBytes: 24 << 20},
		},
	}

	accumulator := NewPodSummaryAccumulator()
	for _, container := range containers {
		accumulator.Add(container)
	}
	got := accumulator.Snapshots()
	want := Pods(containers)
	for index := range want {
		want[index].Containers = nil
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary = %#v, want %#v", got, want)
	}
}
