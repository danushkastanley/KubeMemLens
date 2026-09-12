package observation

import (
	"sort"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

// GroupPods sums only working sets with their coverage; cgroup totals retain
// their separate existing aggregation path.
func GroupPods(pods []Pod) ([]Group, []Group) {
	namespaces, workloads := map[string][]Pod{}, map[string][]Pod{}
	for _, pod := range pods {
		namespaces[pod.Namespace] = append(namespaces[pod.Namespace], pod)
		key := pod.Namespace + "/" + pod.Context.WorkloadKind + "/" + pod.Context.WorkloadName
		if pod.Context.WorkloadName == "" {
			key = pod.Namespace + "/Pod/" + pod.Name
		}
		workloads[key] = append(workloads[key], pod)
	}
	build := func(groups map[string][]Pod, scope capability.Scope) []Group {
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make([]Group, 0, len(keys))
		for _, key := range keys {
			members := groups[key]
			first := members[0]
			group := Group{Name: first.Namespace, Kind: "Namespace", PodCount: len(members)}
			if scope == capability.WorkloadScope {
				group.Namespace, group.Name, group.Kind = first.Namespace, first.Context.WorkloadName, first.Context.WorkloadKind
				if group.Name == "" {
					group.Name, group.Kind = first.Name, "Pod"
				}
			}
			values := make([]WorkingSet, 0, len(members))
			for _, pod := range members {
				values = append(values, pod.WorkingSet)
			}
			group.WorkingSet = SumWorkingSets(values, scope)
			result = append(result, group)
		}
		return result
	}
	return build(namespaces, capability.NamespaceScope), build(workloads, capability.WorkloadScope)
}
