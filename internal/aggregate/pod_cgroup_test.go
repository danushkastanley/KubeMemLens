package aggregate

import (
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Both Pod builders take the Pod cgroup from whichever container carries it;
// a Pod rebuilt without it would fall back to the container-sum limit.
func TestPodBuildersKeepThePodCgroup(t *testing.T) {
	cgroup := model.PodCgroupMemory{CurrentBytes: 300, CurrentKnown: true, MaxBytes: 256 << 20, MaxKnown: true}
	containers := []api.ContainerSnapshot{
		{Namespace: "team-a", PodName: "api", PodUID: "uid-a", ContainerName: "unattributed", Memory: model.MemoryBreakdown{TotalBytes: 10, MaxKnown: true, MaxUnlimited: true}},
		{Namespace: "team-a", PodName: "api", PodUID: "uid-a", ContainerName: "app", PodCgroup: cgroup, Memory: model.MemoryBreakdown{TotalBytes: 20, Zswap: model.Zswap{Known: true, PoolBytes: 4, SwappedBytes: 12}}},
	}
	accumulator := NewPodSummaryAccumulator()
	for _, container := range containers {
		accumulator.Add(container)
	}
	for name, pods := range map[string][]api.PodSnapshot{"Pods": Pods(containers), "summaries": accumulator.Snapshots()} {
		if len(pods) != 1 || pods[0].PodCgroup != cgroup || !pods[0].Memory.Zswap.Known {
			t.Fatalf("%s lost the Pod cgroup or zswap: %#v", name, pods)
		}
		if limit := pods[0].EffectiveLimit(); limit.Source != api.PodLimitPodCgroup || limit.LimitBytes != cgroup.MaxBytes {
			t.Fatalf("%s effective limit = %#v", name, limit)
		}
	}
}
