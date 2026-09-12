package collector

import (
	"math"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestIOPressureIngestionSchemaAndValidation(t *testing.T) {
	now := time.Now().UTC()
	for schema := 1; schema <= 6; schema++ {
		snapshot := api.AgentSnapshotForSchema(resourceValidationSnapshot(now), schema)
		snapshot.Containers[0].Memory.IOPressure = model.IOPressure{State: model.IOAvailable}
		err := ValidateSnapshot(snapshot, now, defaultHandlerOptions(HandlerOptions{}))
		if (err == nil) != (schema == 6) {
			t.Fatalf("schema %d: %v", schema, err)
		}
	}
	for _, input := range []model.IOPressure{
		{State: "unexpected"}, {State: model.IOUnreported, Some: model.PSIWindow{TotalMicros: 1}},
		{State: model.IOAvailable, Some: model.PSIWindow{Avg10: math.NaN()}},
		{State: model.IOAvailable, Some: model.PSIWindow{Avg10: 101}},
		{State: model.IOAvailable, CounterReset: true, DeltaKnown: true},
		{State: model.IOAvailable, SomeDeltaMicros: 5},
	} {
		snapshot := api.AgentSnapshotForSchema(resourceValidationSnapshot(now), 6)
		snapshot.Containers[0].Memory.IOPressure = input
		if ValidateSnapshot(snapshot, now, defaultHandlerOptions(HandlerOptions{})) == nil {
			t.Fatalf("invalid I/O accepted: %+v", input)
		}
	}
}

func TestIOPressureStoreInstanceBoundaries(t *testing.T) {
	for name, mutate := range map[string]func(*api.ContainerSnapshot){
		"same":                  func(*api.ContainerSnapshot) {},
		"Pod replacement":       func(c *api.ContainerSnapshot) { c.PodUID = "replacement" },
		"tenant":                func(c *api.ContainerSnapshot) { c.Namespace = "other" },
		"container replacement": func(c *api.ContainerSnapshot) { c.ContainerID = "replacement" },
		"cgroup replacement":    func(c *api.ContainerSnapshot) { c.CgroupPath = "/other" },
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Now().UTC()
			store := NewStore()
			first := api.ContainerSnapshot{Namespace: "team", PodName: "app", PodUID: "pod", ContainerName: "app", ContainerID: "runtime", CgroupPath: "/fixture", Memory: model.MemoryBreakdown{TotalBytes: 4096, IOPressure: model.IOPressure{State: model.IOAvailable, Some: model.PSIWindow{TotalMicros: 10}}}}
			if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node", CapturedAt: now, Containers: []api.ContainerSnapshot{first}}); err != nil {
				t.Fatal(err)
			}
			second := first
			second.Memory.IOPressure.Some.TotalMicros = 30
			mutate(&second)
			if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{second}}); err != nil {
				t.Fatal(err)
			}
			got := store.ListContainers(now.Add(time.Second), time.Minute)[0]
			if got.Memory.IOPressure.DeltaKnown != (name == "same") || got.DeltaWindowKnown != (name == "same") {
				t.Fatalf("instance evidence crossed boundary: %+v", got.Memory.IOPressure)
			}
			if got.Memory.TotalBytes != 4096 {
				t.Fatal("I/O altered memory")
			}
			got.Memory.IOPressure.Some.TotalMicros = 999
			if store.ListContainers(now.Add(time.Second), time.Minute)[0].Memory.IOPressure.Some.TotalMicros != 30 {
				t.Fatal("read mutated retained I/O")
			}
			pods := store.ListPods(now.Add(time.Second), time.Minute)
			if pods[0].Memory.IOPressure != (model.IOPressure{}) {
				t.Fatal("container pressure aggregated into Pod")
			}
		})
	}
}
