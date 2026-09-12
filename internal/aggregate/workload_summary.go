package aggregate

import (
	"slices"
	"sort"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// SummariseWorkload aggregates an explicitly selected set without inferring
// membership from cached owner names. Callers own membership verification.
func SummariseWorkload(namespace, kind, name string, pods []api.PodSnapshot) api.WorkloadSnapshot {
	w := api.WorkloadSnapshot{Namespace: namespace, Kind: kind, Name: name, Pods: slices.Clone(pods), PodCount: len(pods)}
	memories := make([]model.MemoryBreakdown, 0, len(pods))
	for _, pod := range pods {
		memories = append(memories, pod.Memory)
		if pod.CapturedAt.After(w.CapturedAt) {
			w.CapturedAt = pod.CapturedAt
		}
		if w.LargestPodName == "" || pod.Memory.TotalBytes > w.LargestPodBytes {
			w.LargestPodName, w.LargestPodBytes = pod.PodName, pod.Memory.TotalBytes
		}
		mergeEvidence(&w.Freshness, &w.Completeness, pod.Freshness, pod.Completeness)
	}
	w.Memory = model.SumMemory(namespace+"/"+kind+"/"+name, memories)
	normaliseWorkloadBoundaries(&w.Memory, w.Pods)
	sort.SliceStable(w.Pods, func(i, j int) bool { return w.Pods[i].Memory.TotalBytes > w.Pods[j].Memory.TotalBytes })
	return w
}
