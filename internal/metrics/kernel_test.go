package metrics

import (
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

func metricTypes(memory model.MemoryBreakdown) map[string]uint64 {
	types := map[string]uint64{}
	for _, metric := range memoryMetrics(memory) {
		types[metric.kind] = metric.value
	}
	return types
}

// A zero series would claim the workload uses no kernel memory.
func TestKernelSeriesAreOmittedWhenTheKernelDoesNotReportATotal(t *testing.T) {
	types := metricTypes(model.MemoryBreakdown{TotalBytes: 100, SlabBytes: 40, PageTableBytes: 8})
	for _, name := range []string{"kernel", "kernel_other"} {
		if _, present := types[name]; present {
			t.Errorf("%s series is present although the kernel total was not reported", name)
		}
	}
	if types["slab"] != 40 || types["page_tables"] != 8 {
		t.Fatalf("the reported parts changed: %v", types)
	}
}

func TestKernelSeriesArePresentWhenTheTotalIsReported(t *testing.T) {
	types := metricTypes(model.MemoryBreakdown{TotalBytes: 100, KernelBytes: 50, SlabBytes: 40})
	if types["kernel"] != 50 || types["kernel_other"] != 10 {
		t.Fatalf("kernel=%d kernel_other=%d, want 50 and 10", types["kernel"], types["kernel_other"])
	}
	types = metricTypes(model.MemoryBreakdown{TotalBytes: 100})
	if _, present := types["kernel"]; !present {
		t.Fatal("a cgroup with no kernel memory at all should still report a zero kernel series")
	}
}

func TestRenderedOutputKeepsReportedKernelSeries(t *testing.T) {
	out := renderForTest(t, DefaultOptions())
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="kernel"} 50`)
	assertContains(t, out, `kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="api",type="kernel_other"} 10`)
}
