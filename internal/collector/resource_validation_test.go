package collector

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func resourceValidationSnapshot(now time.Time) api.AgentSnapshot {
	resources := model.PodMemoryResources{Configured: model.MemoryResourceBudget{Limit: model.ResourceValue{Bytes: 384, Known: true}}}
	return api.AgentSnapshot{SchemaVersion: 2, NodeName: "node-a", CapturedAt: now, Containers: []api.ContainerSnapshot{
		{Namespace: "tenant-a", PodName: "app", PodUID: "uid-app", ContainerID: "a", Context: api.ContainerContext{Resources: model.ContainerMemoryResources{Pod: resources}}},
		{Namespace: "tenant-a", PodName: "app", PodUID: "uid-app", ContainerID: "b", Context: api.ContainerContext{Resources: model.ContainerMemoryResources{Pod: resources}}},
	}}
}

func TestSnapshotResourceSchemaValidation(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	opts := defaultHandlerOptions(HandlerOptions{})
	for _, version := range []int{1, 2} {
		snapshot := api.AgentSnapshotForSchema(resourceValidationSnapshot(now), version)
		if err := ValidateSnapshot(snapshot, now, opts); err != nil {
			t.Fatal(err)
		}
	}
	for name, change := range map[string]func(*api.AgentSnapshot){
		"legacy with extension":  func(s *api.AgentSnapshot) { s.SchemaVersion = 1 },
		"unknown schema":         func(s *api.AgentSnapshot) { s.SchemaVersion = api.CurrentSnapshotSchemaVersion + 1 },
		"unreported nonzero":     func(s *api.AgentSnapshot) { s.Containers[0].Context.Resources.Applied.Limit.Bytes = 42 },
		"invalid resize":         func(s *api.AgentSnapshot) { s.Containers[0].Context.Resources.Pod.Pending.State = "unsafe state" },
		"conflicting Pod budget": func(s *api.AgentSnapshot) { s.Containers[1].Context.Resources.Pod.Configured.Limit.Bytes = 512 },
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := resourceValidationSnapshot(now)
			change(&snapshot)
			if err := ValidateSnapshot(snapshot, now, opts); err == nil {
				t.Fatal("invalid resource snapshot accepted")
			}
		})
	}
	snapshot := resourceValidationSnapshot(now)
	snapshot.Containers[1].Namespace = "tenant-b"
	snapshot.Containers[1].Context.Resources.Pod.Configured.Limit.Bytes = 512
	if err := ValidateSnapshot(snapshot, now, opts); err != nil {
		t.Fatalf("same-name Pods across namespaces were combined: %v", err)
	}
}
