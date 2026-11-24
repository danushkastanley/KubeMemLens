package metrics

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Per-entity series carry no Pod UID or container ID. Two instances that share
// a namespace, name and node, such as a Pod recreated under the same name while
// the old cgroup lingers, would otherwise render the same series twice. They
// are merged first, so the series reports the memory charged under that name.

type podSeries struct {
	namespace, pod, node string
}

type containerSeries struct {
	pod       podSeries
	container string
}

func mergePodSeries(items []api.PodSnapshot) []api.PodSnapshot {
	return mergeSeries(items,
		func(item api.PodSnapshot) podSeries {
			return podSeries{item.Namespace, item.PodName, item.NodeName}
		},
		func(into *api.PodSnapshot, item api.PodSnapshot) {
			into.Memory = model.AddMemory(into.Memory, item.Memory)
		})
}

func mergeContainerSeries(items []api.ContainerSnapshot) []api.ContainerSnapshot {
	return mergeSeries(items,
		func(item api.ContainerSnapshot) containerSeries {
			return containerSeries{podSeries{item.Namespace, item.PodName, item.NodeName}, item.ContainerName}
		},
		func(into *api.ContainerSnapshot, item api.ContainerSnapshot) {
			into.Memory = model.AddMemory(into.Memory, item.Memory)
		})
}

// mergeSeries keeps the first item for each key, in its original position, and
// folds every later item with that key into it.
func mergeSeries[T any, K comparable](items []T, key func(T) K, add func(*T, T)) []T {
	merged := make([]T, 0, len(items))
	position := make(map[K]int, len(items))
	for _, item := range items {
		at, seen := position[key(item)]
		if !seen {
			position[key(item)] = len(merged)
			merged = append(merged, item)
			continue
		}
		add(&merged[at], item)
	}
	return merged
}
