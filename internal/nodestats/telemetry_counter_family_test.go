package nodestats

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/metrics/expositiontest"
)

func TestTelemetryCounterFamiliesOmitTheTotalSuffix(t *testing.T) {
	telemetry := &Telemetry{}
	telemetry.recordVolumes(1, 1, nil)
	metrics := telemetry.Render()
	for _, problem := range expositiontest.CounterFamilyProblems(metrics) {
		t.Error(problem)
	}
	for _, family := range []string{"kubememlens_node_context_reads", "kubememlens_volume_stats_records", "kubememlens_node_context_posts"} {
		if !strings.Contains(metrics, "# TYPE "+family+" counter\n") {
			t.Fatalf("counter family %s not found:\n%s", family, metrics)
		}
	}
}
