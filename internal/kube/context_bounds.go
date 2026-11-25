package kube

import (
	"slices"

	"github.com/danushkastanley/kube-memlens/internal/api"
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

// snapshotContext returns the context the agent may put in a node snapshot.
// Other readers of containerContext apply their own limits.
func snapshotContext(context api.ContainerContext) api.ContainerContext {
	context.Labels = boundedLabels(context.Labels)
	context.RuntimeClassName = boundedText(context.RuntimeClassName, maxContextKindBytes)
	context.LastTerminationReason = boundedText(context.LastTerminationReason, maxContextKindBytes)
	context.OwnerKind, context.OwnerName = reportableOwner(context.OwnerKind, context.OwnerName)
	context.WorkloadKind, context.WorkloadName = reportableOwner(context.WorkloadKind, context.WorkloadName)
	context.MemoryEmptyDirCount = min(context.MemoryEmptyDirCount, maxContextMemoryVolumes)
	context.MemoryEmptyDirLimited = min(context.MemoryEmptyDirLimited, context.MemoryEmptyDirCount)
	return context
}

// boundedLabels drops labels the collector would refuse, then keeps the first
// maxContextLabels in key order so the same Pod always reports the same set.
func boundedLabels(labels map[string]string) map[string]string {
	keys := make([]string, 0, len(labels))
	for key, value := range labels {
		if len(key) <= maxContextLabelBytes && len(value) <= maxContextLabelBytes {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)
	keys = keys[:min(len(keys), maxContextLabels)]
	bounded := make(map[string]string, len(keys))
	for _, key := range keys {
		bounded[key] = labels[key]
	}
	return bounded
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
