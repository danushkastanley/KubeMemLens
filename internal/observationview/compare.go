package observationview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/observation"
)

// Compare uses working-set provenance only; it never infers cgroup counters,
// rates or object continuity from display names.
func Compare(before, after Row, beforeAt, afterAt time.Time) ([]string, error) {
	if before.Mode != capability.Restricted || after.Mode != capability.Restricted || before.Cgroup != nil || after.Cgroup != nil || before.WorkingSet == nil || after.WorkingSet == nil {
		return nil, fmt.Errorf("comparison requires two restricted working-set observations")
	}
	if before.Scope != after.Scope {
		return nil, fmt.Errorf("comparison requires matching entity scopes")
	}
	a, b := *before.WorkingSet, *after.WorkingSet
	if a.Evidence.Source != capability.KubernetesMetrics || b.Evidence.Source != capability.KubernetesMetrics {
		return nil, fmt.Errorf("working-set comparison requires Kubernetes Metrics API provenance")
	}
	lines := []string{"Restricted working-set comparison", "Before: " + before.Namespace + "/" + before.Name, "After: " + after.Namespace + "/" + after.Name,
		"Working set: " + Memory(before) + " → " + Memory(after), "State: " + State(before, beforeAt) + " → " + State(after, afterAt),
		fmt.Sprintf("Coverage: %d/%d → %d/%d %s observations", a.Coverage.Reported, a.Coverage.Expected, b.Coverage.Reported, b.Coverage.Expected, a.Coverage.Unit),
		"API version: " + textOrUnreported(a.Evidence.APIVersion) + " → " + textOrUnreported(b.Evidence.APIVersion),
		"Sample windows: " + comparisonWindows(before) + " → " + comparisonWindows(after),
		"Sample: " + sampleRange(a) + " → " + sampleRange(b)}
	compatible := a.Bytes != nil && b.Bytes != nil && a.Evidence.APIVersion == b.Evidence.APIVersion && a.Coverage == b.Coverage
	if compatible {
		var delta string
		if *b.Bytes >= *a.Bytes {
			delta = "+" + model.FormatCompactBytes(*b.Bytes-*a.Bytes)
		} else {
			delta = "-" + model.FormatCompactBytes(*a.Bytes-*b.Bytes)
		}
		lines = append(lines, "Observed working-set delta: "+delta)
	} else {
		lines = append(lines, "Delta unavailable: measurements, API versions or coverage differ.")
	}
	if comparisonWindows(before) != comparisonWindows(after) {
		lines = append(lines, "Sample windows differ; the observations cover different intervals.")
	}
	if a.Evidence.Completeness != capability.Complete || b.Evidence.Completeness != capability.Complete {
		lines = append(lines, "Partial evidence: the observed delta is not a complete workload change.")
	}
	if before.Pod == nil || after.Pod == nil || before.Pod.UID == "" || after.Pod.UID == "" {
		lines = append(lines, "Object continuity is unconfirmed: names and redacted identities cannot prove the same object.")
	} else if before.Pod.UID != after.Pod.UID {
		lines = append(lines, "Different object identities: recreation or different Pods; this is not continuous history.")
	}
	lines = append(lines, RestrictedHelp, DeepUnavailable)
	return lines, nil
}

func comparisonWindows(row Row) string {
	values := map[time.Duration]bool{}
	if row.Pod != nil {
		for _, c := range row.Pod.Containers {
			if c.WorkingSet.Bytes != nil {
				values[c.WorkingSet.Evidence.Window] = true
			}
		}
	} else if row.WorkingSet != nil && row.WorkingSet.Evidence.Window > 0 {
		values[row.WorkingSet.Evidence.Window] = true
	}
	if len(values) == 0 {
		return "unreported in aggregate"
	}
	windows := make([]time.Duration, 0, len(values))
	for value := range values {
		windows = append(windows, value)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i] < windows[j] })
	text := make([]string, 0, len(windows))
	for _, value := range windows {
		text = append(text, value.String())
	}
	return strings.Join(text, ", ")
}

func sampleRange(value observation.WorkingSet) string {
	if value.Evidence.CapturedAt.IsZero() {
		return "unreported"
	}
	result := value.Evidence.CapturedAt.UTC().Format(time.RFC3339Nano)
	if !value.LatestSampleAt.IsZero() && !value.LatestSampleAt.Equal(value.Evidence.CapturedAt) {
		result += " to " + value.LatestSampleAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}
