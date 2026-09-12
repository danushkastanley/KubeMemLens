package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestVolumeHealthProjectionKeepsCurrentFailureForOlderReaders(t *testing.T) {
	last := &volumecontext.HealthReport{Observation: volumehealth.Observation{Availability: volumehealth.Reported, Adverse: true}}
	value := PodVolumeContext{Context: volumecontext.View{Volumes: []volumecontext.NamedVolume{{Health: []volumecontext.Health{{HealthReport: volumecontext.HealthReport{Observation: volumehealth.Observation{Availability: volumehealth.Unavailable, Reason: volumehealth.ReadFailed}}, LastGood: last}}}}}}
	old := PodVolumeContextForSchema(value, VolumeSnapshotSchemaVersion)
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "lastGood") || !strings.Contains(string(data), `"availability":"unavailable"`) {
		t.Fatal("older representation lost current failure or added an unknown field")
	}
	if value.Context.Volumes[0].Health[0].LastGood == nil {
		t.Fatal("projection mutated original")
	}
	current := PodVolumeContextForSchema(value, VolumeHealthSnapshotSchemaVersion)
	if current.Context.Volumes[0].Health[0].LastGood == nil {
		t.Fatal("current representation lost historical evidence")
	}
	snapshot := AgentSnapshot{SchemaVersion: CurrentSnapshotSchemaVersion, VolumeBatch: json.RawMessage(`{"schemaVersion":1}`)}
	if projected := AgentSnapshotForSchema(snapshot, VolumeSnapshotSchemaVersion); len(projected.VolumeBatch) == 0 || projected.SchemaVersion != 4 {
		t.Fatal("health negotiation broke schema4 usage ingestion")
	}
}
