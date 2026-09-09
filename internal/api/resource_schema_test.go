package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSnapshotSchemaNegotiation(t *testing.T) {
	for input, want := range map[string]int{"": 1, "1": 1, "2": 2, "3": 2} {
		got, err := NegotiateSnapshotSchema(input)
		if err != nil || got != want {
			t.Fatalf("schema %q = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, input := range []string{"0", "-1", "1,2", "two", "65536", " 2"} {
		if _, err := NegotiateSnapshotSchema(input); err == nil {
			t.Fatalf("invalid schema %q accepted", input)
		}
	}
}

func resourceSchemaPod() PodSnapshot {
	resources := model.PodMemoryResources{Configured: model.MemoryResourceBudget{Limit: model.ResourceValue{Bytes: 384, Known: true}}}
	container := ContainerSnapshot{
		Namespace: "tenant-a", PodName: "app", PodUID: "uid-app", ContainerName: "worker", ContainerID: "id-worker",
		Context: ContainerContext{MemoryRequestBytes: 128, MemoryRequestKnown: true, Resources: model.ContainerMemoryResources{Pod: resources}},
		Memory:  model.MemoryBreakdown{TotalBytes: 9},
	}
	return PodSnapshot{
		Namespace: "tenant-a", PodName: "app", PodUID: "uid-app", Containers: []ContainerSnapshot{container},
		Context: PodContext{MemoryRequestBytes: 128, MemoryRequestContainers: 1, Resources: resources},
		Memory:  model.MemoryBreakdown{TotalBytes: 9},
	}
}

func TestLegacyViewsKeepExistingEvidenceAndDoNotMutateTheSource(t *testing.T) {
	pod := resourceSchemaPod()
	workload := WorkloadSnapshot{Namespace: "tenant-a", Name: "app", Pods: []PodSnapshot{pod}}
	for name, original := range map[string]any{
		"container page":      ContainerPage{Items: pod.Containers, Continue: "next-page"},
		"containers":          pod.Containers,
		"pods":                []PodSnapshot{pod},
		"pod":                 pod,
		"workloads":           []WorkloadSnapshot{workload},
		"PodMemory":           PodMemory{Snapshot: pod},
		"PodMemoryList":       PodMemoryList{ListMeta: metav1.ListMeta{Continue: "next-page"}, Items: []PodMemory{{Snapshot: pod}}},
		"ContainerMemory":     ContainerMemory{Snapshot: pod.Containers[0]},
		"ContainerMemoryList": ContainerMemoryList{Items: []ContainerMemory{{Snapshot: pod.Containers[0]}}},
		"WorkloadMemory":      WorkloadMemory{Snapshot: workload},
		"WorkloadMemoryList":  WorkloadMemoryList{Items: []WorkloadMemory{{Snapshot: workload}}},
	} {
		t.Run(name, func(t *testing.T) {
			before, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			legacy := SnapshotView(original, LegacySchemaVersion)
			encoded, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"resources":`) {
				t.Fatalf("legacy client received resource context: %s", encoded)
			}
			for _, expected := range []string{`"tenant-a"`, `"memoryRequestBytes":128`, `"TotalBytes":9`} {
				if !strings.Contains(string(encoded), expected) {
					t.Fatalf("legacy evidence lost %s: %s", expected, encoded)
				}
			}
			if strings.Contains(string(before), "next-page") && !strings.Contains(string(encoded), "next-page") {
				t.Fatal("pagination cursor changed")
			}
			after, _ := json.Marshal(original)
			if string(before) != string(after) {
				t.Fatal("legacy projection mutated the source")
			}
			if !reflect.DeepEqual(SnapshotView(original, CurrentSnapshotSchemaVersion), original) {
				t.Fatal("current schema lost evidence")
			}
		})
	}
	status := StoreDebug{TotalContainers: 10}
	if !reflect.DeepEqual(SnapshotView(status, LegacySchemaVersion), status) {
		t.Fatal("operational response changed")
	}
}

func TestAgentAndIncidentLegacyProjection(t *testing.T) {
	pod := resourceSchemaPod()
	agent := AgentSnapshot{SchemaVersion: CurrentSnapshotSchemaVersion, Containers: pod.Containers}
	legacy := AgentSnapshotForSchema(agent, LegacySchemaVersion)
	if legacy.SchemaVersion != 1 || !legacy.Containers[0].Context.Resources.IsZero() || agent.Containers[0].Context.Resources.IsZero() {
		t.Fatal("legacy agent projection changed its source or retained new fields")
	}
	if IncidentSchema([]PodSnapshot{pod}) != CurrentIncidentSchemaVersion || IncidentSchema(nil) != LegacySchemaVersion {
		t.Fatal("incident schema did not follow the recorded evidence")
	}
	bundle := LegacyIncident(IncidentBundle{SchemaVersion: 2, Pods: []PodSnapshot{pod}})
	if bundle.SchemaVersion != 1 || IncidentSchema(bundle.Pods) != 1 || pod.Context.Resources.IsZero() {
		t.Fatal("legacy incident projection changed the source or retained new fields")
	}
	pod.Context.Resources = model.PodMemoryResources{}
	if IncidentSchema([]PodSnapshot{pod}) != 2 {
		t.Fatal("container-only resize evidence was omitted from schema detection")
	}
}
