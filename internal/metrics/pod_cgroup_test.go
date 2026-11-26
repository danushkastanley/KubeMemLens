package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func renderPodCgroupForTest(t *testing.T, pod api.PodSnapshot) string {
	t.Helper()
	now := fixedNow()
	pod.Namespace, pod.PodName, pod.NodeName, pod.CapturedAt = "default", "budget", "node-a", now.Add(-5*time.Second)
	source := testSource{
		pods:   []api.PodSnapshot{pod},
		debug:  api.DebugStore{Pods: 1},
		latest: map[string]time.Time{"node-a": now.Add(-5 * time.Second)},
	}
	out, err := (Exporter{Source: source, TTL: time.Minute, Now: fixedNow, Opts: DefaultOptions()}).Render()
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	return out
}

func podCgroupPod() api.PodSnapshot {
	unlimited := model.MemoryBreakdown{TotalBytes: 230 << 20, AnonBytes: 230 << 20, MaxKnown: true, MaxUnlimited: true,
		Zswap:        model.Zswap{Known: true, PoolBytes: 4 << 20, SwappedBytes: 12 << 20},
		OOMGroupKill: model.OOMGroupKill{Known: true, Events: 3, DeltasKnown: true, EventsDelta: 1, LocalKnown: true, LocalEvents: 2, LocalDeltasKnown: true}}
	return api.PodSnapshot{Memory: unlimited, PodCgroup: model.PodCgroupMemory{
		CurrentKnown: true, CurrentBytes: 240 << 20, MaxKnown: true, MaxBytes: 256 << 20,
		LocalEventsKnown: true, LocalOOMKillEvents: 2, LocalMaxEvents: 5,
		LocalEventDeltasKnown: true, LocalOOMKillEventsDelta: 1,
	}}
}

func TestPodCgroupSeriesAreAddedWhenKnown(t *testing.T) {
	out := renderPodCgroupForTest(t, podCgroupPod())
	for _, want := range []string{
		`kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="budget",type="pod_cgroup_current"} 251658240`,
		`kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="budget",type="pod_cgroup_max"} 268435456`,
		`kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="budget",type="zswap"} 4194304`,
		`kubememlens_pod_memory_bytes{namespace="default",node="node-a",pod="budget",type="zswapped"} 12582912`,
		`kubememlens_pod_memory_events{event="pod_cgroup_oom",namespace="default",node="node-a",pod="budget"} 0`,
		`kubememlens_pod_memory_events{event="pod_cgroup_oom_kill",namespace="default",node="node-a",pod="budget"} 2`,
		`kubememlens_pod_memory_events{event="pod_cgroup_max",namespace="default",node="node-a",pod="budget"} 5`,
		`kubememlens_pod_memory_events{event="pod_cgroup_oom_kill_delta",namespace="default",node="node-a",pod="budget"} 1`,
		`kubememlens_pod_memory_events{event="pod_cgroup_max_delta",namespace="default",node="node-a",pod="budget"} 0`,
		`kubememlens_pod_memory_events{event="oom_group_kill",namespace="default",node="node-a",pod="budget"} 3`,
		`kubememlens_pod_memory_events{event="oom_group_kill_delta",namespace="default",node="node-a",pod="budget"} 1`,
		`kubememlens_pod_memory_events{event="local_oom_group_kill",namespace="default",node="node-a",pod="budget"} 2`,
		`kubememlens_pod_memory_events{event="local_oom_group_kill_delta",namespace="default",node="node-a",pod="budget"} 0`,
		// Recent Pod cgroup OOM kills make the Pod diagnosis oom-risk.
		`kubememlens_pod_diagnosis{diagnosis="oom-risk",namespace="default",node="node-a",pod="budget"} 1`,
	} {
		assertContains(t, out, want)
	}
	// type="max" keeps meaning the summed container limit, unlimited here.
	assertNotContains(t, out, `pod="budget",type="max"}`)
}

func TestPodCgroupSeriesAreAbsentWhenUnknown(t *testing.T) {
	pod := podCgroupPod()
	pod.PodCgroup = model.PodCgroupMemory{MaxKnown: true, MaxUnlimited: true}
	pod.Memory.Zswap, pod.Memory.OOMGroupKill = model.Zswap{}, model.OOMGroupKill{}
	out := renderPodCgroupForTest(t, pod)
	for _, absent := range []string{"pod_cgroup_", "zswap", "oom_group_kill"} {
		if strings.Contains(out, absent) {
			t.Errorf("series %q rendered without the evidence:\n%s", absent, out)
		}
	}
}

// Two Pod instances under one series name have two Pod cgroups. Neither may
// stand for both.
func TestMergedPodSeriesDropDifferingPodCgroups(t *testing.T) {
	first, second := podCgroupPod(), podCgroupPod()
	second.PodCgroup.CurrentBytes = 10 << 20
	merged := mergePodSeries([]api.PodSnapshot{first, second})
	if len(merged) != 1 || merged[0].PodCgroup != (model.PodCgroupMemory{}) {
		t.Fatalf("merged Pod cgroup = %+v, want none", merged[0].PodCgroup)
	}
	if same := mergePodSeries([]api.PodSnapshot{first, podCgroupPod()}); same[0].PodCgroup != first.PodCgroup {
		t.Fatalf("identical Pod cgroups were dropped: %+v", same[0].PodCgroup)
	}
}
