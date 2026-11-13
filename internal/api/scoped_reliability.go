package api

// ReliabilityFromPods describes the evidence a namespace-scoped read returned.
// Such a read cannot see the collector's own state, so the result speaks only
// for the rows in hand: an empty list holds nothing stale or partial and is not
// a sign that the collector is still rebuilding.
func ReliabilityFromPods(pods []PodSnapshot) CollectorReliability {
	stale, incomplete := 0, false
	for _, pod := range pods {
		if pod.Freshness == EvidenceFreshnessStale {
			stale++
		}
		incomplete = incomplete || pod.Completeness == EvidencePartial
	}
	switch {
	case stale == 0 && !incomplete:
		return CollectorReliability{State: CollectorReady, Completeness: EvidenceComplete}
	case stale < len(pods):
		return CollectorReliability{State: CollectorDegraded, Completeness: EvidencePartial}
	default:
		return CollectorReliability{State: CollectorStale, Completeness: EvidencePartial}
	}
}
