package aggregate

import "github.com/danushkastanley/kube-memlens/internal/api"

// ContextForContainers shares metadata aggregation without fabricating cgroup
// measurements for an API-only reader.
func ContextForContainers(containers []api.ContainerContext) api.PodContext {
	var pod api.PodContext
	for _, container := range containers {
		addContainerContext(&pod, container)
	}
	return pod
}
