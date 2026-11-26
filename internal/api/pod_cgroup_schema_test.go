package api

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

var schema8Keys = []string{"podCgroup", "zswap", "oomGroupKill"}

func schema8Pod() PodSnapshot {
	pod := resourceSchemaPod()
	cgroup := model.PodCgroupMemory{CurrentBytes: 300, CurrentKnown: true, MaxBytes: 256 << 20, MaxKnown: true,
		LocalEventsKnown: true, LocalOOMEvents: 2, LocalEventDeltasKnown: true, LocalOOMEventsDelta: 1}
	zswap := model.Zswap{Known: true, PoolBytes: 4, SwappedBytes: 12}
	groupKill := model.OOMGroupKill{Known: true, Events: 1, DeltasKnown: true, EventsDelta: 1}
	pod.PodCgroup, pod.Memory.Zswap, pod.Memory.OOMGroupKill = cgroup, zswap, groupKill
	pod.Containers[0].PodCgroup, pod.Containers[0].Memory.Zswap, pod.Containers[0].Memory.OOMGroupKill = cgroup, zswap, groupKill
	pod.Containers[0].Memory.IOPressure = model.IOPressure{State: model.IOAvailable, Some: model.PSIWindow{Avg10: 3, TotalMicros: 50}}
	return pod
}

// Every value a reader or the collector can receive, projected to an older
// schema, must hold no schema 8 key anywhere and must decode with a strict
// decoder of the schema 7 types into the same bytes. The schema 7 types are
// the current types without the three schema 8 fields (schema7Shape).
func TestSchema8EvidenceIsProjectedForEveryOlderReader(t *testing.T) {
	pod := schema8Pod()
	workload := WorkloadSnapshot{Namespace: pod.Namespace, Name: "app", Pods: []PodSnapshot{pod}, Memory: pod.Memory}
	agent := AgentSnapshot{SchemaVersion: CurrentSnapshotSchemaVersion, NodeName: "node-a", Containers: pod.Containers}
	before, _ := json.Marshal(pod)
	for name, project := range map[string]func(int) any{
		"containers":     func(v int) any { return SnapshotView(pod.Containers, v) },
		"container page": func(v int) any { return SnapshotView(ContainerPage{Items: pod.Containers, Continue: "next"}, v) },
		"pods":           func(v int) any { return SnapshotView([]PodSnapshot{pod}, v) },
		"pod":            func(v int) any { return SnapshotView(pod, v) },
		"workloads":      func(v int) any { return SnapshotView([]WorkloadSnapshot{workload}, v) },
		"namespaces": func(v int) any {
			return SnapshotView([]NamespaceSnapshot{{Namespace: pod.Namespace, Memory: pod.Memory}}, v)
		},
		"PodMemory":       func(v int) any { return SnapshotView(PodMemory{Snapshot: pod}, v) },
		"PodMemoryList":   func(v int) any { return SnapshotView(PodMemoryList{Items: []PodMemory{{Snapshot: pod}}}, v) },
		"ContainerMemory": func(v int) any { return SnapshotView(ContainerMemory{Snapshot: pod.Containers[0]}, v) },
		"ContainerMemoryList": func(v int) any {
			return SnapshotView(ContainerMemoryList{Items: []ContainerMemory{{Snapshot: pod.Containers[0]}}}, v)
		},
		"WorkloadMemory": func(v int) any { return SnapshotView(WorkloadMemory{Snapshot: workload}, v) },
		"WorkloadMemoryList": func(v int) any {
			return SnapshotView(WorkloadMemoryList{Items: []WorkloadMemory{{Snapshot: workload}}}, v)
		},
		"WorkloadVolumeContext": func(v int) any {
			return SnapshotView(WorkloadVolumeContext{Workload: workload}, v)
		},
		"agent snapshot": func(v int) any { return AgentSnapshotForSchema(agent, v) },
	} {
		t.Run(name, func(t *testing.T) {
			current := project(PodCgroupSnapshotSchemaVersion)
			shape := schema7Shape(reflect.TypeOf(current))
			currentJSON := mustMarshal(t, current)
			if len(foundSchema8Keys(t, currentJSON)) == 0 {
				t.Fatal("schema 8 response lost its evidence")
			}
			if _, err := reencodeStrict(currentJSON, shape); err == nil {
				t.Fatal("the schema 7 decoder accepted schema 8 evidence, so this check cannot fail")
			}
			for version := LegacySchemaVersion; version < PodCgroupSnapshotSchemaVersion; version++ {
				data := mustMarshal(t, project(version))
				if keys := foundSchema8Keys(t, data); len(keys) != 0 {
					t.Fatalf("schema %d response carries %v", version, keys)
				}
				reencoded, err := reencodeStrict(data, shape)
				if err != nil {
					t.Fatalf("schema 7 strict decoder rejected the schema %d response: %v", version, err)
				}
				if !bytes.Equal(reencoded, data) {
					t.Fatalf("schema %d response is not byte-compatible with schema 7:\n got %s\nwant %s", version, data, reencoded)
				}
				if !strings.Contains(string(data), `"TotalBytes":9`) || !strings.Contains(string(data), `"tenant-a"`) {
					t.Fatalf("schema %d projection changed memory or identity", version)
				}
			}
		})
	}
	if after, _ := json.Marshal(pod); !bytes.Equal(before, after) {
		t.Fatal("projection mutated the stored evidence")
	}
}

// A value without schema 8 evidence must be written exactly as schema 7 wrote
// it, even at schema 8: the new fields are omitted, not written as zero.
func TestValuesWithoutSchema8EvidenceKeepTheSchema7Bytes(t *testing.T) {
	pod := resourceSchemaPod()
	pod.Containers[0].Memory.IOPressure = model.IOPressure{State: model.IOUnreported}
	for name, value := range map[string]any{
		"MemoryBreakdown":   pod.Memory,
		"ContainerSnapshot": pod.Containers[0],
		"PodSnapshot":       pod,
		"WorkloadSnapshot":  WorkloadSnapshot{Namespace: pod.Namespace, Name: "app", Pods: []PodSnapshot{pod}, Memory: pod.Memory},
		"NamespaceSnapshot": NamespaceSnapshot{Namespace: pod.Namespace, PodCount: 1, Memory: pod.Memory},
	} {
		t.Run(name, func(t *testing.T) {
			data := mustMarshal(t, value)
			if keys := foundSchema8Keys(t, data); len(keys) != 0 {
				t.Fatalf("value without evidence writes %v", keys)
			}
			reencoded, err := reencodeStrict(data, schema7Shape(reflect.TypeOf(value)))
			if err != nil || !bytes.Equal(reencoded, data) {
				t.Fatalf("bytes differ from the schema 7 shape (%v):\n got %s\nwant %s", err, data, reencoded)
			}
		})
	}
}

func TestIncidentSchemasNeverKeepSchema8Evidence(t *testing.T) {
	original := IncidentBundle{SchemaVersion: CurrentIncidentSchemaVersion, Pods: []PodSnapshot{schema8Pod()}}
	for name, bundle := range map[string]IncidentBundle{
		"schema 2": WithoutIOIncident(original),
		"schema 1": LegacyIncident(original),
	} {
		t.Run(name, func(t *testing.T) {
			if keys := foundSchema8Keys(t, mustMarshal(t, bundle)); len(keys) != 0 {
				t.Fatalf("incident keeps %v", keys)
			}
			if !bundle.Partial || !slices.Contains(bundle.Caveats, PodCgroupIncidentCaveat) || !slices.ContainsFunc(bundle.Caveats, func(c string) bool { return strings.Contains(c, "I/O pressure") }) {
				t.Fatalf("omission was not recorded: partial=%t caveats=%q", bundle.Partial, bundle.Caveats)
			}
			if err := ValidateIncidentSchema(bundle); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := ValidateIncidentSchema(original); err == nil {
		t.Fatal("incident validation accepted schema 8 evidence")
	}
	if !PodHasSchema8Evidence(original.Pods[0]) {
		t.Fatal("incident export mutated its source")
	}
	if plain := WithoutIOIncident(IncidentBundle{SchemaVersion: CurrentIncidentSchemaVersion, Pods: []PodSnapshot{resourceSchemaPod()}}); plain.Partial || len(plain.Caveats) != 0 {
		t.Fatalf("an incident without the evidence gained a caveat: %q", plain.Caveats)
	}
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// reencodeStrict decodes data into shape, refusing unknown fields as every
// released reader does, and encodes the result again.
func reencodeStrict(data []byte, shape reflect.Type) ([]byte, error) {
	target := reflect.New(shape)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target.Interface()); err != nil {
		return nil, err
	}
	return json.Marshal(target.Elem().Interface())
}

func foundSchema8Keys(t *testing.T, data []byte) []string {
	t.Helper()
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	var found []string
	var visit func(any)
	visit = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if slices.Contains(schema8Keys, key) {
					found = append(found, key)
				}
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	visit(tree)
	return found
}
