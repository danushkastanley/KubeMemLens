package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/resourceview"
)

// Workload totals have no single Pod budget. Keep resource changes attributed
// to each observed replica instead of summing them into a synthetic Pod.
func printWorkloadResourceComparison(w io.Writer, before, after api.IncidentBundle, reference string) {
	left := workloadResourcePods(before.Pods, reference)
	right := workloadResourcePods(after.Pods, reference)
	names := make(map[string]bool, len(left)+len(right))
	for name := range left {
		names[name] = true
	}
	for name := range right {
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		for _, line := range resourceview.ComparisonLines(left[name], right[name]) {
			fmt.Fprintf(w, "Pod %s — %s\n", name, line)
		}
	}
}

func workloadResourcePods(pods []api.PodSnapshot, reference string) map[string]api.PodSnapshot {
	result := make(map[string]api.PodSnapshot)
	parts := strings.Split(reference, "/")
	if len(parts) != 3 {
		return result
	}
	for _, pod := range pods {
		if pod.Namespace == parts[0] && strings.EqualFold(pod.Context.WorkloadKind, parts[1]) && pod.Context.WorkloadName == parts[2] {
			result[pod.PodName] = pod
		}
	}
	return result
}
