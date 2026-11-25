package agent

import (
	"strings"
	"testing"
)

func TestReducedMetadataGaugeFollowsTheLatestScan(t *testing.T) {
	telemetry := &Telemetry{}
	if !strings.Contains(telemetry.Render(), "kubememlens_agent_metadata_reduced_pods 0\n") {
		t.Fatalf("gauge is not present at zero before any Pod is reduced:\n%s", telemetry.Render())
	}
	telemetry.RecordReducedMetadata(3)
	telemetry.RecordReducedMetadata(1)
	if !strings.Contains(telemetry.Render(), "kubememlens_agent_metadata_reduced_pods 1\n") {
		t.Fatalf("gauge does not report the latest scan's count of 1:\n%s", telemetry.Render())
	}
}
