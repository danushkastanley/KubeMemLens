package collector

import (
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

// BenchmarkNodeContextCapacity measures the explicit optional profile. It is
// opt-in because filling the encoded history ceiling is unsuitable per edit.
func BenchmarkNodeContextCapacity(b *testing.B) {
	for range b.N {
		now := time.Now().UTC()
		store := NewStoreWithHistoryAndLimits(DefaultHistoryOptions(), StoreLimits{MaxNodes: 1000, MaxContainers: 20000})
		store.now = func() time.Time { return now }
		identities := make(map[string]string, 1000)
		for n := 0; n < 1000; n++ {
			name := fmt.Sprintf("node-%04d", n)
			identities[name] = fmt.Sprintf("uid-%04d", n)
			containers := make([]api.ContainerSnapshot, 20)
			for i := range containers {
				containers[i] = api.ContainerSnapshot{ContainerID: fmt.Sprintf("container-%04d-%02d", n, i),
					Namespace: "capacity", PodName: fmt.Sprintf("pod-%04d-%02d", n, i), PodUID: fmt.Sprintf("pod-uid-%04d-%02d", n, i), ContainerName: "app"}
			}
			if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: name, CapturedAt: now, Containers: containers}); err != nil {
				b.Fatal(err)
			}
		}
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		encodedBytes := 0
		for tick := 0; tick < 61; tick++ {
			now = now.Add(15 * time.Second)
			if err := store.ReconcileNodeIdentities(identities, now); err != nil {
				b.Fatal(err)
			}
			for name, uid := range identities {
				sample := wideContextSample(now)
				sample.NodeName, sample.NodeUID = name, uid
				if encodedBytes == 0 {
					encoded, _ := json.Marshal(sample)
					encodedBytes = len(encoded)
				}
				if err := store.ReplaceNodeContext(sample); err != nil {
					b.Fatal(err)
				}
			}
		}
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		debug := store.NodeContextDebug(now)
		if debug.Records != 1000 || debug.HistoryBytes > nodecontext.MaxHistoryBytes || !debug.CoverageLost {
			b.Fatalf("capacity accounting: %#v", debug)
		}
		b.ReportMetric(float64(before.HeapAlloc), "baseline-heap-bytes")
		b.ReportMetric(float64(after.HeapAlloc), "total-heap-bytes")
		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc), "node-heap-bytes")
		b.ReportMetric(float64(encodedBytes), "observation-bytes")
		b.ReportMetric(float64(debug.HistoryBytes), "history-bytes")
		b.ReportMetric(float64(debug.HistoryPoints), "history-points")
		runtime.KeepAlive(store)
	}
}

func wideContextSample(at time.Time) nodecontext.Observation {
	value := contextSample(at)
	maximum := uint64(math.MaxUint64)
	memory := &nodecontext.Memory{CapturedAt: at, UsageBytes: &maximum, AvailableBytes: &maximum, WorkingSetBytes: &maximum, RSSBytes: &maximum,
		PageFaults: &maximum, MajorPageFaults: &maximum, PSI: &nodecontext.PSI{
			Some: nodecontext.PSIData{TotalNanoseconds: maximum, Avg10: 99.9999999999, Avg60: 99.9999999999, Avg300: 99.9999999999},
			Full: nodecontext.PSIData{TotalNanoseconds: maximum, Avg10: 99.9999999999, Avg60: 99.9999999999, Avg300: 99.9999999999}}}
	swap := &nodecontext.Swap{CapturedAt: at, UsageBytes: &maximum, AvailableBytes: &maximum}
	value.Stats.Memory, value.Stats.Swap = memory, swap
	for _, category := range []nodecontext.SystemCategory{nodecontext.Kubelet, nodecontext.Runtime, nodecontext.Misc, nodecontext.Pods} {
		value.Stats.SystemContainers = append(value.Stats.SystemContainers, nodecontext.SystemContainer{Category: category, StartedAt: at.Add(-time.Hour), Memory: memory, Swap: swap})
	}
	value.Context = &nodecontext.KubernetesContext{CapturedAt: at, CapacityBytes: &maximum, AllocatableBytes: &maximum, MemoryPressure: "Unknown"}
	for i := 1; i <= 16; i++ {
		value.Context.Hugepages = append(value.Context.Hugepages, nodecontext.Hugepage{Resource: "hugepages-" + strings.Repeat("0", 42) + fmt.Sprintf("%dMi", i), CapacityBytes: &maximum, AllocatableBytes: &maximum})
	}
	for range 16 {
		value.Evidence.Caveats = append(value.Evidence.Caveats, "optional-fields-unreported")
	}
	return value
}
