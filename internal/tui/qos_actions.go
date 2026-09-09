package tui

import "github.com/danushkastanley/kube-memlens/internal/api"

func qosPodsForRef(ref entityRef, pods []api.PodSnapshot) []api.PodSnapshot {
	var selected []api.PodSnapshot
	for _, pod := range pods {
		if pod.Namespace != ref.namespace {
			continue
		}
		switch ref.kind {
		case entityWorkload:
			if pod.Context.WorkloadKind == ref.workloadKind && pod.Context.WorkloadName == ref.name {
				selected = append(selected, pod)
			}
		case entityPod:
			if pod.PodName == ref.podName {
				selected = append(selected, pod)
			}
		case entityContainer:
			if pod.PodName != ref.podName {
				continue
			}
			for _, container := range pod.Containers {
				if container.ContainerName == ref.containerName {
					pod.Containers = []api.ContainerSnapshot{container}
					selected = append(selected, pod)
				}
			}
		}
	}
	return selected
}
