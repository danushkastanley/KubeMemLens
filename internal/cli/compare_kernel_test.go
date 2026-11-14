package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func kernelRow(t *testing.T, before, after model.MemoryBreakdown) string {
	t.Helper()
	var output bytes.Buffer
	printPodComparison(&output, "Comparison",
		api.PodSnapshot{Namespace: "default", PodName: "api", Memory: before},
		api.PodSnapshot{Namespace: "default", PodName: "api", Memory: after}, 0)
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, "Kernel") {
			return line
		}
	}
	t.Fatalf("comparison has no Kernel row:\n%s", output.String())
	return ""
}

func TestComparisonDoesNotShowAnUnreportedKernelTotalAsZero(t *testing.T) {
	old := model.MemoryBreakdown{TotalBytes: 10 << 20, SlabBytes: 2 << 20}
	current := model.MemoryBreakdown{TotalBytes: 12 << 20, SlabBytes: 2 << 20, KernelBytes: 4 << 20}

	if row := kernelRow(t, old, old); !strings.Contains(row, model.KernelNotReported) || strings.Contains(row, "0B") {
		t.Fatalf("Kernel row = %q, want %q and no zero", row, model.KernelNotReported)
	}
	// One side from an older kernel: no delta can be stated.
	if row := kernelRow(t, old, current); !strings.Contains(row, model.KernelNotReported) {
		t.Fatalf("Kernel row = %q, want %q", row, model.KernelNotReported)
	}
	if row := kernelRow(t, current, current); !strings.Contains(row, "4Mi") || strings.Contains(row, model.KernelNotReported) {
		t.Fatalf("Kernel row = %q, want the reported total", row)
	}
}
