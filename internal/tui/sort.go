package tui

import (
	"sort"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func SortNamespaces(items []api.NamespaceSnapshot, mode sortMode) {
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
			return left.Namespace < right.Namespace
		default:
			return left.Memory.TotalBytes > right.Memory.TotalBytes
		}
	})
}

func SortPods(items []api.PodSnapshot, mode sortMode) {
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

func SortContainers(items []api.ContainerSnapshot, mode sortMode) {
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
