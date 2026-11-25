package kube

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/profiler"
	corev1 "k8s.io/api/core/v1"
)

// Limits the collector enforces on one container's context, with the text it
// refuses in unprintable below. It rejects the
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
// them. containers are the Pod's container statuses, which decide the labels
// KubeMemLens reads. Other readers of containerContext apply their own limits.
func snapshotContext(full api.ContainerContext, containers []corev1.ContainerStatus) (api.ContainerContext, bool) {
	bounded := full
	bounded.Labels = boundedLabels(full.Labels, containers)
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
// maxContextLabels of the rest. The profiling declarations of the Pod's own
// containers are the labels KubeMemLens itself reads, so they are kept first
// and a Pod with many labels does not lose them. The others follow in key
// order, so every container of a Pod reports the same set on every scan.
func boundedLabels(labels map[string]string, containers []corev1.ContainerStatus) map[string]string {
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
	read := make(map[string]bool, len(containers))
	for _, container := range containers {
		read[profiler.LabelPrefix+container.Name] = true
	}
	slices.SortFunc(keys, func(a, b string) int {
		aRead, bRead := read[a], read[b]
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
// limited ones give way. MemoryEmptyDirLimitBytes is not reduced with them:
// it stays the total of every size limit the Pod declares.
func boundedVolumeCounts(count, limited int) (int, int) {
	unbounded := count - limited
	count = min(count, maxContextMemoryVolumes)
	return count, count - min(unbounded, count)
}

// reportableOwner returns an owner's kind and name only when the collector
// accepts both. The workload resolver uses it too: a ReplicaSet's or Job's own
// owner reference is as unrestricted as a Pod's.
func reportableOwner(kind, name string) (string, string) {
	if boundedText(kind, maxContextKindBytes) == "" || boundedText(name, maxContextNameBytes) == "" {
		return "", ""
	}
	return kind, name
}

// boundedText omits a value that is over the limit or that holds text the
// collector refuses. A cut-off or rewritten name would read as a different
// object, so it is reported as absent instead.
func boundedText(value string, limit int) string {
	if len(value) > limit || !utf8.ValidString(value) || strings.ContainsFunc(value, unprintable) {
		return ""
	}
	return value
}

// unprintable matches the characters the collector refuses in agent text,
// control and format characters, and the line and paragraph separators. The
// API server does not restrict an owner reference's kind or name, and a
// termination reason comes from the container runtime.
func unprintable(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
}

// ReducedContextPods counts the Pods in the index whose metadata was reduced
// to fit the collector's limits. The index holds every Pod with a container
// ID, so this can include a Pod whose containers have exited.
func (idx PodIndex) ReducedContextPods() int {
	reduced := map[string]struct{}{}
	for _, ref := range idx.ByContainerID {
		if ref.ContextReduced {
			reduced[ref.PodUID] = struct{}{}
		}
	}
	return len(reduced)
}
