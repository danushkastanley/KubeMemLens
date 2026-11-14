package model

import "testing"

func TestKernelTextSaysWhenTheTotalIsNotReported(t *testing.T) {
	unreported := MemoryBreakdown{SlabBytes: 2 << 20, PageTableBytes: 1 << 20}
	if unreported.KernelReported() {
		t.Fatal("a zero kernel total beside non-zero slab was treated as reported")
	}
	for name, text := range map[string]string{"total": FormatKernelTotal(unreported), "other": FormatKernelOther(unreported)} {
		if text != KernelNotReported {
			t.Errorf("kernel %s = %q, want %q", name, text, KernelNotReported)
		}
	}

	reported := MemoryBreakdown{KernelBytes: 6 << 20, SlabBytes: 2 << 20, PageTableBytes: 1 << 20}
	if !reported.KernelReported() || FormatKernelTotal(reported) != FormatBytes(6<<20) || FormatKernelOther(reported) != FormatBytes(4<<20) {
		t.Fatalf("reported kernel: %q / %q", FormatKernelTotal(reported), FormatKernelOther(reported))
	}

	if empty := (MemoryBreakdown{}); !empty.KernelReported() || FormatKernelTotal(empty) != FormatBytes(0) {
		t.Fatal("a cgroup with no kernel memory at all should read as zero, not as unreported")
	}
}

// Containers on one node share a kernel, so a Pod's sum keeps the property.
func TestKernelReportedSurvivesAggregation(t *testing.T) {
	sum := AddMemory(MemoryBreakdown{SlabBytes: 1 << 20}, MemoryBreakdown{PageTableBytes: 1 << 20})
	if sum.KernelReported() {
		t.Fatal("the sum of two unreported kernel totals was treated as reported")
	}
}
