package kube

import (
	"slices"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/profiler"
)

// Limits the collector enforces on one container's context. It rejects the
// whole node snapshot when any container is over one, and Kubernetes accepts
// Pods beyond each of them, so the agent has to stay inside these itself or
// one Pod could stop its node from publishing.
const (
	maxContextLabels        = 64
	maxContextLabelBytes    = 253
	maxContextKindBytes     = 64
	maxContextNameBytes     = 253
	maxContextMemoryVolumes = 256
)

// snapshotContext returns the context the agent may put in a node snapshot,
// and whether anything had to be left out of it. The snapshot has no field
// that says so, so the caller counts reduced Pods where an operator can see
// them. Other readers of containerContext apply their own limits.
func snapshotContext(full api.ContainerContext) (api.ContainerContext, bool) {
	bounded := full
	bounded.Labels = boundedLabels(full.Labels)
	bounded.RuntimeClassName = boundedText(full.RuntimeClassName, maxContextKindBytes)
	bounded.LastTerminationReason = boundedText(full.LastTerminationReason, maxContextKindBytes)
	bounded.OwnerKind, bounded.OwnerName = reportableOwner(full.OwnerKind, full.OwnerName)
	bounded.WorkloadKind, bounded.WorkloadName = reportableOwner(full.WorkloadKind, full.WorkloadName)
	bounded.MemoryEmptyDirCount, bounded.MemoryEmptyDirLimited = boundedVolumeCounts(full.MemoryEmptyDirCount, full.MemoryEmptyDirLimited)
	reduced := len(bounded.Labels) != len(full.Labels) ||
		bounded.RuntimeClassName != full.RuntimeClassName ||
		bounded.LastTerminationReason != full.LastTerminationReason ||
		bounded.OwnerKind != full.OwnerKind || bounded.OwnerName != full.OwnerName ||
		bounded.WorkloadKind != full.WorkloadKind || bounded.WorkloadName != full.WorkloadName ||
		bounded.MemoryEmptyDirCount != full.MemoryEmptyDirCount
	return bounded, reduced
}

// boundedLabels drops labels the collector would refuse, then keeps
// maxContextLabels of the rest. Labels KubeMemLens itself reads are kept
// first, so a Pod with many labels does not lose its profiling declaration;
// the others follow in key order so the same Pod always reports the same set.
func boundedLabels(labels map[string]string) map[string]string {
	keys := make([]string, 0, len(labels))
	for key, value := range labels {
		if len(key) <= maxContextLabelBytes && len(value) <= maxContextLabelBytes {
			keys = append(keys, key)
		}
	}
	if len(keys) == len(labels) && len(keys) <= maxContextLabels {
		return labels
	}
	if len(keys) == 0 {
		return nil
	}
	slices.SortFunc(keys, func(a, b string) int {
		aRead, bRead := strings.HasPrefix(a, profiler.LabelPrefix), strings.HasPrefix(b, profiler.LabelPrefix)
		if aRead != bRead {
			if aRead {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	keys = keys[:min(len(keys), maxContextLabels)]
	bounded := make(map[string]string, len(keys))
	for _, key := range keys {
		bounded[key] = labels[key]
	}
	return bounded
}

// boundedVolumeCounts keeps the memory-backed emptyDir count inside the limit
// without hiding a volume that has no size limit. Unbounded volumes are the
// ones an operator is told to review, so they stay in the count and the
// limited ones give way.
func boundedVolumeCounts(count, limited int) (int, int) {
	unbounded := count - limited
	count = min(count, maxContextMemoryVolumes)
	return count, count - min(unbounded, count)
}

// reportableOwner returns an owner's kind and name only when both fit. The
// workload resolver uses it too: a ReplicaSet's or Job's own owner reference
// is as unrestricted as a Pod's.
func reportableOwner(kind, name string) (string, string) {
	if len(kind) > maxContextKindBytes || len(name) > maxContextNameBytes {
		return "", ""
	}
	return kind, name
}

// boundedText omits a value that is over the limit. A cut-off name would read
// as a different object, so it is reported as absent instead.
func boundedText(value string, limit int) string {
	if len(value) > limit {
		return ""
	}
	return value
}

// ReducedContextPods counts the Pods in the index whose metadata was reduced
// to fit the collector's limits.
func (idx PodIndex) ReducedContextPods() int {
	reduced := map[string]struct{}{}
	for _, ref := range idx.ByContainerID {
		if ref.ContextReduced {
			reduced[ref.PodUID] = struct{}{}
		}
	}
	return len(reduced)
}
