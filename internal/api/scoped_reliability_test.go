package api

import "testing"

func TestReliabilityFromPods(t *testing.T) {
	fresh := PodSnapshot{Freshness: EvidenceFreshnessFresh, Completeness: EvidenceComplete}
	partial := PodSnapshot{Freshness: EvidenceFreshnessFresh, Completeness: EvidencePartial}
	stale := PodSnapshot{Freshness: EvidenceFreshnessStale, Completeness: EvidenceComplete}
	for name, test := range map[string]struct {
		pods         []PodSnapshot
		state        CollectorState
		completeness EvidenceCompleteness
	}{
		"no Pods":         {nil, CollectorReady, EvidenceComplete},
		"fresh":           {[]PodSnapshot{fresh, fresh}, CollectorReady, EvidenceComplete},
		"partial":         {[]PodSnapshot{fresh, partial}, CollectorDegraded, EvidencePartial},
		"fresh and stale": {[]PodSnapshot{fresh, stale}, CollectorDegraded, EvidencePartial},
		"stale":           {[]PodSnapshot{stale}, CollectorStale, EvidencePartial},
	} {
		t.Run(name, func(t *testing.T) {
			result := ReliabilityFromPods(test.pods)
			if result.State != test.state || result.Completeness != test.completeness {
				t.Fatalf("reliability = %#v, want %s/%s", result, test.state, test.completeness)
			}
		})
	}
}
