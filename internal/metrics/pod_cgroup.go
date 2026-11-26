package metrics

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// podMemoryMetrics adds the Pod cgroup's own values to the Pod family. The
// existing type="max" keeps meaning the summed container limit; the Pod
// cgroup's memory.max is a separate type, emitted only when finite.
func podMemoryMetrics(pod api.PodSnapshot) []memoryMetric {
	items := memoryMetrics(pod.Memory)
	cgroup := pod.PodCgroup
	if cgroup.CurrentKnown {
		items = append(items, memoryMetric{"pod_cgroup_current", cgroup.CurrentBytes})
	}
	if cgroup.MaxKnown && !cgroup.MaxUnlimited {
		items = append(items, memoryMetric{"pod_cgroup_max", cgroup.MaxBytes})
	}
	return items
}

// podEventMetrics adds the Pod cgroup's memory.events.local counters, and the
// collector's deltas of them, only when they were read.
func podEventMetrics(pod api.PodSnapshot) []eventMetric {
	items := eventMetrics(pod.Memory)
	cgroup := pod.PodCgroup
	if cgroup.LocalEventsKnown {
		items = append(items,
			eventMetric{"pod_cgroup_oom", cgroup.LocalOOMEvents},
			eventMetric{"pod_cgroup_oom_kill", cgroup.LocalOOMKillEvents},
			eventMetric{"pod_cgroup_max", cgroup.LocalMaxEvents},
		)
	}
	if cgroup.LocalEventDeltasKnown {
		items = append(items,
			eventMetric{"pod_cgroup_oom_delta", cgroup.LocalOOMEventsDelta},
			eventMetric{"pod_cgroup_oom_kill_delta", cgroup.LocalOOMKillEventsDelta},
			eventMetric{"pod_cgroup_max_delta", cgroup.LocalMaxEventsDelta},
		)
	}
	return items
}

func zswapMetrics(zswap model.Zswap) []memoryMetric {
	if !zswap.Known {
		return nil
	}
	return []memoryMetric{{"zswap", zswap.PoolBytes}, {"zswapped", zswap.SwappedBytes}}
}

// oomGroupKillMetrics follows the oom_kill series: each counter and delta only
// when the kernel reported the key.
func oomGroupKillMetrics(group model.OOMGroupKill) []eventMetric {
	var items []eventMetric
	if group.Known {
		items = append(items, eventMetric{"oom_group_kill", group.Events})
	}
	if group.DeltasKnown {
		items = append(items, eventMetric{"oom_group_kill_delta", group.EventsDelta})
	}
	if group.LocalKnown {
		items = append(items, eventMetric{"local_oom_group_kill", group.LocalEvents})
	}
	if group.LocalDeltasKnown {
		items = append(items, eventMetric{"local_oom_group_kill_delta", group.LocalEventsDelta})
	}
	return items
}

func podDiagnosis(pod api.PodSnapshot) string {
	return string(explain.AnalyzePodMemory(pod).Diagnosis)
}
