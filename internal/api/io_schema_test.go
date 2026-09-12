package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestIOPressureReadSchemaProjection(t *testing.T) {
	pod := resourceSchemaPod()
	pod.Containers[0].Memory.IOPressure = model.IOPressure{State: model.IOAvailable, Some: model.PSIWindow{Avg10: 3, TotalMicros: 50}}
	workload := WorkloadSnapshot{Namespace: pod.Namespace, Name: "app", Pods: []PodSnapshot{pod}}
	for name, original := range map[string]any{
		"containers": pod.Containers, "container page": ContainerPage{Items: pod.Containers, Continue: "next"},
		"pods": []PodSnapshot{pod}, "pod": pod, "workloads": []WorkloadSnapshot{workload},
		"PodMemory": PodMemory{Snapshot: pod}, "PodMemoryList": PodMemoryList{Items: []PodMemory{{Snapshot: pod}}},
		"ContainerMemory":     ContainerMemory{Snapshot: pod.Containers[0]},
		"ContainerMemoryList": ContainerMemoryList{Items: []ContainerMemory{{Snapshot: pod.Containers[0]}}},
		"WorkloadMemory":      WorkloadMemory{Snapshot: workload},
		"WorkloadMemoryList":  WorkloadMemoryList{Items: []WorkloadMemory{{Snapshot: workload}}},
	} {
		t.Run(name, func(t *testing.T) {
			before, _ := json.Marshal(original)
			for version := 1; version <= 6; version++ {
				data, err := json.Marshal(SnapshotView(original, version))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), `"ioPressure"`) != (version == 6) {
					t.Fatalf("schema %d I/O projection failed", version)
				}
				if !strings.Contains(string(data), `"TotalBytes":9`) || !strings.Contains(string(data), `"tenant-a"`) {
					t.Fatal("memory or identity changed")
				}
				if strings.Contains(string(data), `"resources"`) != (version != 1) {
					t.Fatal("resource schema changed")
				}
			}
			after, _ := json.Marshal(original)
			if string(before) != string(after) {
				t.Fatal("projection mutated current evidence")
			}
		})
	}
}

func TestIOPressureLegacyIncidentExport(t *testing.T) {
	pod := resourceSchemaPod()
	pod.Containers[0].Memory.IOPressure = model.IOPressure{State: model.IOUnreported}
	original := IncidentBundle{SchemaVersion: 2, Pods: []PodSnapshot{pod}}
	for _, value := range []IncidentBundle{WithoutIOIncident(original), LegacyIncident(original)} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"ioPressure"`) || !value.Partial || len(value.Caveats) == 0 {
			t.Fatal("legacy export retained I/O or concealed omission")
		}
	}
	if original.Pods[0].Containers[0].Memory.IOPressure.State != model.IOUnreported {
		t.Fatal("source mutated")
	}
}
