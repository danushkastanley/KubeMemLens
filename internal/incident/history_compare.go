package incident

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type HistoryComparison struct {
	Continuity       string
	ComparableMetric bool
	Replacements     []changemarkers.Marker
}

func CompareHistory(before, after HistoryBundle) (HistoryComparison, error) {
	if ValidateHistory(before) != nil || ValidateHistory(after) != nil || after.CapturedAt.Before(before.CapturedAt) || after.Context.Changes.ObservedAt.Before(before.Context.Changes.ObservedAt) {
		return HistoryComparison{}, fmt.Errorf("history comparison requires valid chronologically ordered captures")
	}
	a, b := before.Context.History, after.Context.History
	result := HistoryComparison{Continuity: "same-selection", ComparableMetric: a.Query.Source == b.Query.Source && a.Query.Metric == b.Query.Metric}
	if before.Redacted || after.Redacted {
		result.Continuity = "identity-unavailable"
		return result, nil
	}
	if a.Selection.Request != b.Selection.Request {
		result.Continuity = "different-selection"
		return result, nil
	}
	if a.Selection.UID != b.Selection.UID {
		result.Continuity = "different-instances"
		if b.Selection.Request.Scope == memoryhistory.Workload {
			return result, nil
		}
	}
	previous := map[string]string{}
	for _, target := range a.Selection.Targets {
		previous[target.Pod] = target.PodUID
	}
	seen := map[string]bool{}
	for _, target := range b.Selection.Targets {
		prior, known := previous[target.Pod]
		if !known || seen[target.Pod] || prior == target.PodUID {
			continue
		}
		seen[target.Pod] = true
		// Derive an interval only when the after capture retains the exact owner
		// chain; do not invent a historical chain from a reused Pod name.
		for _, evidence := range after.Context.Changes.Markers {
			if evidence.Subject.Kind != "Pod" || evidence.Subject.Name != target.Pod || evidence.Subject.UID != target.PodUID {
				continue
			}
			marker := changemarkers.Marker{Kind: changemarkers.Replacement, At: before.Context.Changes.ObservedAt, Until: after.Context.Changes.ObservedAt, Clock: changemarkers.Observation, SourceUID: target.PodUID, Subject: evidence.Subject, Owners: append([]changemarkers.Object(nil), evidence.Owners...), PreviousUID: prior, Count: 1, Uncertain: true}
			if marker.Validate() != nil {
				return HistoryComparison{}, fmt.Errorf("invalid replacement provenance")
			}
			result.Replacements = append(result.Replacements, marker)
			break
		}
	}
	return result, nil
}
