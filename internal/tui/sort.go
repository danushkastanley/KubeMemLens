package tui

import (
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
)

func SortNamespaces(items []api.NamespaceSnapshot, mode sortMode) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		switch mode {
		case sortRisk:
			leftRisk := memoryRisk(left.Memory, explain.Analyze(left.Memory).Severity)
			rightRisk := memoryRisk(right.Memory, explain.Analyze(right.Memory).Severity)
			if leftRisk.score != rightRisk.score {
				return leftRisk.score > rightRisk.score
			}
			if left.Memory.TotalBytes != right.Memory.TotalBytes {
				return left.Memory.TotalBytes > right.Memory.TotalBytes
			}
			return left.Namespace < right.Namespace
		case sortRSS:
			return left.Memory.RSSBytes() > right.Memory.RSSBytes()
		case sortCache:
			return left.Memory.CacheBytes() > right.Memory.CacheBytes()
		case sortShmem:
			return left.Memory.ShmemBytes > right.Memory.ShmemBytes
		case sortName:
			return left.Namespace < right.Namespace
		default:
			return left.Memory.TotalBytes > right.Memory.TotalBytes
		}
	})
}

func SortPods(items []api.PodSnapshot, mode sortMode) {
	SortPodsAt(items, mode, time.Time{}, 0)
}

func SortPodsAt(items []api.PodSnapshot, mode sortMode, now time.Time, staleAfter time.Duration) {
	if mode == sortRisk {
		sortPodsByRisk(items, func(pod api.PodSnapshot) int { return podRisk(pod, now, staleAfter).score })
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		switch mode {
		case sortRSS:
			return left.Memory.RSSBytes() > right.Memory.RSSBytes()
		case sortCache:
			return left.Memory.CacheBytes() > right.Memory.CacheBytes()
		case sortShmem:
			return left.Memory.ShmemBytes > right.Memory.ShmemBytes
		case sortName:
			if left.Namespace == right.Namespace {
				return left.PodName < right.PodName
			}
			return left.Namespace < right.Namespace
		default:
			return left.Memory.TotalBytes > right.Memory.TotalBytes
		}
	})
}

// sortPodsByRisk scores each Pod once. Scoring analyses the Pod, which is far
// too slow to repeat for every comparison.
func sortPodsByRisk(items []api.PodSnapshot, score func(api.PodSnapshot) int) {
	type scored struct {
		pod   api.PodSnapshot
		score int
	}
	ranked := make([]scored, len(items))
	for index, pod := range items {
		ranked[index] = scored{pod, score(pod)}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		left, right := ranked[i], ranked[j]
		if left.score != right.score {
			return left.score > right.score
		}
		if left.pod.Memory.TotalBytes != right.pod.Memory.TotalBytes {
			return left.pod.Memory.TotalBytes > right.pod.Memory.TotalBytes
		}
		if left.pod.Namespace != right.pod.Namespace {
			return left.pod.Namespace < right.pod.Namespace
		}
		return left.pod.PodName < right.pod.PodName
	})
	for index := range ranked {
		items[index] = ranked[index].pod
	}
}

func SortWorkloads(items []api.WorkloadSnapshot, mode sortMode) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		switch mode {
		case sortRisk:
			leftRisk := memoryRisk(left.Memory, explain.AnalyzeWorkload(left).Severity)
			rightRisk := memoryRisk(right.Memory, explain.AnalyzeWorkload(right).Severity)
			if leftRisk.score != rightRisk.score {
				return leftRisk.score > rightRisk.score
			}
			if left.Memory.TotalBytes != right.Memory.TotalBytes {
				return left.Memory.TotalBytes > right.Memory.TotalBytes
			}
			return left.Namespace+left.Kind+left.Name < right.Namespace+right.Kind+right.Name
		case sortRSS:
			return left.Memory.RSSBytes() > right.Memory.RSSBytes()
		case sortCache:
			return left.Memory.CacheBytes() > right.Memory.CacheBytes()
		case sortShmem:
			return left.Memory.ShmemBytes > right.Memory.ShmemBytes
		case sortName:
			if left.Namespace != right.Namespace {
				return left.Namespace < right.Namespace
			}
			if left.Kind != right.Kind {
				return left.Kind < right.Kind
			}
			return left.Name < right.Name
		default:
			return left.Memory.TotalBytes > right.Memory.TotalBytes
		}
	})
}

func SortContainers(items []api.ContainerSnapshot, mode sortMode) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		switch mode {
		case sortRisk:
			leftRisk := memoryRisk(left.Memory, explain.AnalyzeContainer(left).Severity)
			rightRisk := memoryRisk(right.Memory, explain.AnalyzeContainer(right).Severity)
			if leftRisk.score != rightRisk.score {
				return leftRisk.score > rightRisk.score
			}
			if left.Memory.TotalBytes != right.Memory.TotalBytes {
				return left.Memory.TotalBytes > right.Memory.TotalBytes
			}
			return left.Namespace+left.PodName+left.ContainerName < right.Namespace+right.PodName+right.ContainerName
		case sortRSS:
			return left.Memory.RSSBytes() > right.Memory.RSSBytes()
		case sortCache:
			return left.Memory.CacheBytes() > right.Memory.CacheBytes()
		case sortShmem:
			return left.Memory.ShmemBytes > right.Memory.ShmemBytes
		case sortName:
			if left.Namespace != right.Namespace {
				return left.Namespace < right.Namespace
			}
			if left.PodName != right.PodName {
				return left.PodName < right.PodName
			}
			return left.ContainerName < right.ContainerName
		default:
			return left.Memory.TotalBytes > right.Memory.TotalBytes
		}
	})
}
