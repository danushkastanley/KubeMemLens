package observation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestDeepProjectionPreservesSnapshotsWithoutReinterpretingMemory(t *testing.T) {
	now := time.Now().UTC()
	snapshot := api.PodSnapshot{Namespace: "team-a", PodName: "app", PodUID: "uid", NodeName: "node-a", CapturedAt: now,
		Freshness: api.EvidenceFreshnessStale, Completeness: api.EvidencePartial, Memory: model.MemoryBreakdown{TotalBytes: 123, AnonBytes: 100},
		Context: api.PodContext{Labels: map[string]string{"app": "example"}},
		Containers: []api.ContainerSnapshot{{Namespace: "team-a", PodName: "app", PodUID: "uid", ContainerName: "worker", NodeName: "node-a",
			ContainerID: "private-id", CgroupPath: "/private/cgroup", CapturedAt: now, Freshness: api.EvidenceFreshnessStale,
			Completeness: api.EvidencePartial, DeltaWindowKnown: true, DeltaStartedAt: now.Add(-time.Minute),
			Context: api.ContainerContext{Labels: map[string]string{"app": "example"}}, Memory: model.MemoryBreakdown{TotalBytes: 123, AnonBytes: 100}}}}
	pod := FromDeepPod(snapshot, now)
	if pod.WorkingSet.Bytes != nil {
		t.Fatal("cgroup charge populated working set")
	}
	actual, ok := pod.DeepSnapshot()
	if !ok || !reflect.DeepEqual(snapshot, actual) {
		t.Fatalf("snapshot changed: %+v", actual)
	}
	actual.Context.Labels["app"] = "changed"
	actual.Containers[0].Context.Labels["app"] = "changed"
	if snapshot.Context.Labels["app"] != "example" || pod.Containers[0].Context.Labels["app"] != "example" {
		t.Fatal("projection shares mutable labels")
	}
	encoded, err := json.Marshal(pod)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-id") || strings.Contains(string(encoded), "/private/cgroup") {
		t.Fatal("private cgroup identity leaked into observation JSON")
	}
}

func TestRestrictedOrMixedPodCannotProduceDeepSnapshot(t *testing.T) {
	for _, pod := range []Pod{{}, {Cgroup: &Cgroup{}, Containers: []Container{{Name: "missing"}}}} {
		if _, ok := pod.DeepSnapshot(); ok {
			t.Fatal("missing cgroup evidence became a snapshot")
		}
	}
}
