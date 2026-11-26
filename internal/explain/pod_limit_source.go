package explain

import "github.com/danushkastanley/kube-memlens/internal/api"

// NamedPodLimitSource returns where the Pod's effective limit was read, only
// when the Pod cgroup's memory.max was read. Without that value the limit is
// the container sum every output showed before snapshot schema 8, so outputs
// that name the source stay exactly as they were.
func NamedPodLimitSource(pod api.PodSnapshot) (api.PodLimitSource, bool) {
	if !pod.PodCgroup.MaxKnown {
		return "", false
	}
	return pod.EffectiveLimit().Source, true
}

// PodLimitSourceWords is the source in words for text and detail panes.
func PodLimitSourceWords(source api.PodLimitSource) string {
	if source == api.PodLimitPodCgroup {
		return "Pod cgroup"
	}
	return "sum of container limits"
}
