package aggregate

import (
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestPodResourceBudgetIsNotMultipliedByContainers(t *testing.T) {
	resources := model.PodMemoryResources{Configured: model.MemoryResourceBudget{
		Request: model.ResourceValue{Bytes: 192, Known: true}, Limit: model.ResourceValue{Bytes: 384, Known: true},
	}}
	containers := []api.ContainerSnapshot{
		{Namespace: "tenant-a", PodName: "app", PodUID: "uid-app", ContainerName: "a", Context: api.ContainerContext{
			MemoryRequestBytes: 64, MemoryRequestKnown: true, MemoryLimitBytes: 128, MemoryLimitKnown: true,
			Resources: model.ContainerMemoryResources{Pod: resources},
		}},
		{Namespace: "tenant-a", PodName: "app", PodUID: "uid-app", ContainerName: "b", Context: api.ContainerContext{
			MemoryRequestBytes: 64, MemoryRequestKnown: true, MemoryLimitBytes: 128, MemoryLimitKnown: true,
			Resources: model.ContainerMemoryResources{Pod: resources},
		}},
	}
	summaries := NewPodSummaryAccumulator()
	for _, container := range containers {
		summaries.Add(container)
	}
	for _, pods := range [][]api.PodSnapshot{Pods(containers), summaries.Snapshots()} {
		if len(pods) != 1 {
			t.Fatalf("got %d Pods", len(pods))
		}
		ctx := pods[0].Context
		if ctx.Resources != resources || ctx.MemoryRequestBytes != 128 || ctx.MemoryLimitBytes != 256 || ctx.MemoryRequestContainers != 2 {
			t.Fatalf("Pod and container budgets were conflated: %+v", ctx)
		}
	}
	if containers[0].Context.Resources.Pod != resources || containers[1].Context.Resources.Pod != resources {
		t.Fatal("aggregation mutated container resource context")
	}
}
