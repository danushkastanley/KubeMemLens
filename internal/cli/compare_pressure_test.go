package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func pressureRow(t *testing.T, before, after model.MemoryBreakdown) string {
	t.Helper()
	var output bytes.Buffer
	printPodComparison(&output, "Comparison",
		api.PodSnapshot{Namespace: "default", PodName: "api", Memory: before},
		api.PodSnapshot{Namespace: "default", PodName: "api", Memory: after}, 0)
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, "PSI some avg10") {
			return strings.Join(strings.Fields(line), " ")
		}
	}
	t.Fatalf("comparison has no PSI row:\n%s", output.String())
	return ""
}

func TestComparisonDoesNotShowUnknownPressureAsZero(t *testing.T) {
	unread := model.MemoryBreakdown{TotalBytes: 10 << 20}
	stalled := model.MemoryBreakdown{TotalBytes: 10 << 20, PressureKnown: true, PSISomeAvg10: 1.5}
	calm := model.MemoryBreakdown{TotalBytes: 10 << 20, PressureKnown: true}

	if row := pressureRow(t, unread, unread); row != "PSI some avg10 unknown unknown n/a" {
		t.Fatalf("PSI row = %q, want both sides unknown and no delta", row)
	}
	// One side unread: its value is unknown and no delta can be stated.
	if row := pressureRow(t, unread, stalled); row != "PSI some avg10 unknown 1.50% n/a" {
		t.Fatalf("PSI row = %q, want the unread side unknown and no delta", row)
	}
	if row := pressureRow(t, calm, stalled); row != "PSI some avg10 0.00% 1.50% +1.50pp" {
		t.Fatalf("PSI row = %q, want both values and the delta", row)
	}
}
