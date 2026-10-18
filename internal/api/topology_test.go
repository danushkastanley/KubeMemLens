package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTopologySchemaProjection(t *testing.T) {
	original := AgentSnapshot{SchemaVersion: 7, Topology: json.RawMessage(`{"private":"topology-secret"}`)}
	for version := 1; version < 7; version++ {
		projected := AgentSnapshotForSchema(original, version)
		data, err := json.Marshal(projected)
		if err != nil || len(projected.Topology) != 0 || strings.Contains(string(data), "topology-secret") {
			t.Fatalf("topology leaked to schema %d", version)
		}
	}
	if len(original.Topology) == 0 || len(AgentSnapshotForSchema(original, 7).Topology) == 0 {
		t.Fatal("projection lost current topology")
	}
}
