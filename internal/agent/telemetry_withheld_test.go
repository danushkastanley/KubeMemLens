package agent

import (
	"strings"
	"testing"
)

func TestTelemetryCountsWithheldSnapshotsByReason(t *testing.T) {
	telemetry := &Telemetry{}
	telemetry.RecordWithheld(WithheldPartialScan)
	telemetry.RecordWithheld(WithheldPartialScan)
	telemetry.RecordWithheld(WithheldMetadataCacheUnsynchronised)

	metrics := telemetry.Render()
	for _, want := range []string{
		"# TYPE kubememlens_agent_snapshots_withheld counter",
		`kubememlens_agent_snapshots_withheld_total{reason="metadata_cache_unsynchronised"} 1`,
		`kubememlens_agent_snapshots_withheld_total{reason="partial_scan_not_published"} 2`,
	} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("metrics missing %q:\n%s", want, metrics)
		}
	}
}

func TestTelemetryRendersEveryWithheldReasonBeforeAnyIsRecorded(t *testing.T) {
	metrics := (&Telemetry{}).Render()
	for _, want := range []string{
		`kubememlens_agent_snapshots_withheld_total{reason="metadata_cache_unsynchronised"} 0`,
		`kubememlens_agent_snapshots_withheld_total{reason="partial_scan_not_published"} 0`,
	} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("metrics missing %q:\n%s", want, metrics)
		}
	}
}

func TestTelemetryIgnoresAnUnknownWithheldReason(t *testing.T) {
	telemetry := &Telemetry{}
	telemetry.RecordWithheld(WithheldReason("node=worker-1"))

	if metrics := telemetry.Render(); strings.Contains(metrics, "worker-1") {
		t.Fatalf("an unbounded reason reached the metrics:\n%s", metrics)
	}
}
