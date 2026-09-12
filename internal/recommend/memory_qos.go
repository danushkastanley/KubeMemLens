package recommend

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
)

// ForPodMemoryQoS deduplicates the interpreter's read-only checks across the
// selected Pods without inventing a kubelet setting or a sizing patch.
func ForPodMemoryQoS(pods []api.PodSnapshot) []Recommendation {
	var conditions []string
	seen := map[string]bool{}
	for _, pod := range pods {
		for _, container := range pod.Containers {
			for _, check := range explain.InterpretMemoryQoS(container).SuggestedChecks {
				if !seen[check] {
					conditions = append(conditions, check)
					seen[check] = true
				}
			}
		}
	}
	if len(conditions) == 0 {
		return nil
	}
	return []Recommendation{{ID: "inspect-memory-qos-evidence", Priority: "investigate", Action: "Review observed protection and throttling against applied resources and workload symptoms.", Rationale: "Cgroup controls show enforcement evidence; kubelet policy, parent controls and safe workload sizing require separate verification.", Conditions: conditions}}
}
