package agent

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/metrics/expositiontest"
)

func TestTelemetryCounterFamiliesOmitTheTotalSuffix(t *testing.T) {
	metrics := (&Telemetry{}).Render()
	for _, problem := range expositiontest.CounterFamilyProblems(metrics) {
		t.Error(problem)
	}
	if !strings.Contains(metrics, "# TYPE kubememlens_agent_scans counter\n") {
		t.Fatalf("no counter family found:\n%s", metrics)
	}
}
