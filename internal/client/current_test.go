package client

import (
	"context"
	"fmt"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestLoadCurrentSnapshotTraversesPagesAndBuildsAggregates(t *testing.T) {
	calls := 0
	get := func(_ context.Context, path string, out any) error {
		calls++
		page := out.(*api.ContainerPage)
		switch calls {
		case 1:
			page.Items = []api.ContainerSnapshot{clientTestContainer("api-a", "id-a", 100)}
			page.Continue = "next"
		case 2:
			if path != "/api/v1/pages/containers?limit=500&continue=next" {
				t.Fatalf("second path = %q", path)
			}
			page.Items = []api.ContainerSnapshot{clientTestContainer("api-b", "id-b", 300)}
		default:
			return fmt.Errorf("unexpected page call %d", calls)
		}
		return nil
	}

	snapshot, err := loadCurrentSnapshot(context.Background(), get)
	if err != nil {
		t.Fatalf("loadCurrentSnapshot returned error: %v", err)
	}
	if len(snapshot.Containers) != 2 || len(snapshot.Pods) != 2 || len(snapshot.Namespaces) != 1 || len(snapshot.Workloads) != 1 {
		t.Fatalf("unexpected aggregate counts: %#v", snapshot)
	}
	if snapshot.Workloads[0].Memory.TotalBytes != 400 || snapshot.Workloads[0].LargestPodName != "api-b" {
		t.Fatalf("unexpected workload: %#v", snapshot.Workloads[0])
	}
}

func TestLoadContainerPagesRejectsRepeatedContinuation(t *testing.T) {
	get := func(_ context.Context, _ string, out any) error {
		page := out.(*api.ContainerPage)
		page.Items = []api.ContainerSnapshot{{ContainerID: "id-a"}}
		page.Continue = "same"
		return nil
	}
	if _, err := loadContainerPages(context.Background(), get); err == nil {
		t.Fatal("loadContainerPages returned nil error")
	}
}

func clientTestContainer(pod, id string, total uint64) api.ContainerSnapshot {
	return api.ContainerSnapshot{
		Namespace: "default", PodName: pod, PodUID: "uid-" + pod,
		ContainerName: "app", ContainerID: id,
		Context: api.ContainerContext{WorkloadKind: "Deployment", WorkloadName: "api"},
		Memory:  model.MemoryBreakdown{TotalBytes: total},
	}
}
