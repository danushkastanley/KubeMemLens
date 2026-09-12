package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVolumeProjectionPreservesOlderMemoryRepresentations(t *testing.T) {
	original := AgentSnapshot{SchemaVersion: VolumeSnapshotSchemaVersion, NodeName: "node-a", VolumeBatch: json.RawMessage(`{"private":"tenant-volume"}`)}
	for _, version := range []int{1, 2, 3} {
		projected := AgentSnapshotForSchema(original, version)
		data, err := json.Marshal(projected)
		if err != nil {
			t.Fatal(err)
		}
		if projected.SchemaVersion != version || projected.NodeName != original.NodeName || len(projected.VolumeBatch) != 0 || strings.Contains(string(data), "tenant-volume") {
			t.Fatal("volume projection changed legacy semantics")
		}
	}
	if len(original.VolumeBatch) == 0 || len(AgentSnapshotForSchema(original, VolumeSnapshotSchemaVersion).VolumeBatch) == 0 {
		t.Fatal("projection mutated original or current representation")
	}
}
