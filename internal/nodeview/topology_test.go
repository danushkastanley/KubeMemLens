package nodeview

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	"strings"
	"testing"
	"time"
)

func TestTopologyViewRecomputesAgeAndKeepsDomainsSeparate(t *testing.T) {
	now := time.Now().UTC()
	o := memorytopology.Failure("node-a", "uid", now)
	total, free, reserved, surplus := uint64(8), uint64(6), uint64(1), uint64(2)
	o.Pools = memorytopology.Section[memorytopology.HugePool]{Source: memorytopology.HugeTLBSysfs, Availability: capability.Available, Completeness: capability.Complete, CapturedAt: now, Items: []memorytopology.HugePool{{PageSizeBytes: 2 << 20, TotalPages: &total, FreePages: &free, ReservedPages: &reserved, SurplusPages: &surplus}}}
	report, err := memorytopology.Analyse(o, now)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(TopologyLines(api.NodeMemoryTopology{Current: &report}, now, 100), "\n")
	for _, want := range []string{"separate from ordinary memory", "surplus 2", "in use", "reserved", "Topology Manager", "Kubelet availability"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	stale := strings.Join(TopologyLines(api.NodeMemoryTopology{Current: &report}, now.Add(time.Minute), 100), "\n")
	if !strings.Contains(stale, "HugeTLB pools: stale") || strings.Contains(stale, "NUMA distribution: balanced") {
		t.Fatal("age or missing NUMA misreported")
	}
}
