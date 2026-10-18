package replicabaseline

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"
)

type peerMeasurements struct {
	values  map[Metric]Value
	history *HistoryWindow
}

// Analyse never chooses a tenant, acquires evidence or changes the input. Each
// candidate is compared with the original matching eligible peers, excluding
// itself, so iteration order cannot change a subsequent candidate's baseline.
func Analyse(input Input) (Report, error) {
	if err := validate(input); err != nil {
		return Report{}, err
	}
	peers := append([]Peer(nil), input.Peers...)
	slices.SortFunc(peers, func(a, b Peer) int { return cmp.Compare(a.Object.Name, b.Object.Name) })
	result := Report{SchemaVersion: 1, PolicyVersion: 1, Workload: input.Workload, ObservedAt: input.ObservedAt, Peers: []PeerReport{}, Caveats: []string{
		"Replica differences are informational; diagnosis severity and resource settings are unchanged.",
		"Comparable current evidence does not establish equal traffic, health or causation.",
		"Missing change history does not prove that no changes occurred.",
		"Confidence describes evidence coverage, not a statistical probability.",
	}}
	measurements := make(map[string]peerMeasurements, len(peers))
	for _, peer := range peers {
		values := make(map[Metric]Value, len(policies))
		for key, value := range peer.Values {
			values[key] = value
		}
		points := recentHistory(peer)
		values[ChargeSlope] = slope(peer, points)
		entry := peerMeasurements{values: values}
		if len(points) > 0 && peer.HistoryState == Available {
			entry.history = &HistoryWindow{StartedAt: points[0].At, EndedAt: points[len(points)-1].At, Samples: len(points)}
		}
		measurements[peer.Object.UID] = entry
	}
	for _, candidate := range peers {
		result.Peers = append(result.Peers, analysePeer(input.ObservedAt, candidate, peers, measurements))
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return Report{}, ErrInvalid
	}
	if len(encoded) > MaxResponseBytes {
		return Report{}, ErrBounds
	}
	return result, nil
}

func analysePeer(now time.Time, candidate Peer, peers []Peer, values map[string]peerMeasurements) PeerReport {
	result := PeerReport{Peer: candidate.Object, CapturedAt: candidate.CapturedAt, StableSince: candidate.StableSince, Revision: candidate.Revision, Shape: candidate.Shape, Exclusion: exclusion(now, candidate), Excluded: []Exclusion{}, Comparisons: []Comparison{}}
	if result.Exclusion != "" {
		return result
	}
	result.History = values[candidate.Object.UID].history
	result.DeltaStartedAt = candidate.DeltaStartedAt
	var references []Peer
	for _, peer := range peers {
		if peer.Object.UID == candidate.Object.UID {
			continue
		}
		reason := referenceExclusion(now, candidate, peer)
		if reason != "" {
			result.Excluded = append(result.Excluded, Exclusion{Peer: peer.Object, Reason: reason})
			continue
		}
		references = append(references, peer)
	}
	for _, policy := range policies {
		value, known := values[candidate.Object.UID].values[policy.metric]
		if !known {
			value.State = Unreported
		}
		if value.State != Available {
			result.Comparisons = append(result.Comparisons, Comparison{Metric: policy.metric, Unit: policy.unit, State: string(value.State), References: []string{}, Outlier: "none", Confidence: "insufficient"})
			continue
		}
		measurements, identities := []float64{}, []string{}
		var omitted []MetricOmission
		limited := candidate.Changes == ChangeUnknown
		for _, peer := range references {
			reference, known := values[peer.Object.UID].values[policy.metric]
			if !known {
				reference.State = Unreported
			}
			if reference.State != Available {
				omitted = append(omitted, MetricOmission{PeerUID: peer.Object.UID, Reason: "metric-" + string(reference.State)})
				continue
			}
			if policy.unit == "events-per-second" && !alignedDeltaWindows(candidate, peer) {
				omitted = append(omitted, MetricOmission{PeerUID: peer.Object.UID, Reason: "delta-window-mismatch"})
				continue
			}
			measurements = append(measurements, reference.Number)
			identities = append(identities, peer.Object.UID)
			limited = limited || peer.Changes == ChangeUnknown
		}
		comparison := compare(policy, value.Number, measurements, identities)
		comparison.Omitted = omitted
		if limited && comparison.State == "compared" {
			comparison.Confidence = "limited-change-history"
		}
		result.Comparisons = append(result.Comparisons, comparison)
	}
	return result
}

func alignedDeltaWindows(a, b Peer) bool {
	first, second := a.CapturedAt.Sub(a.DeltaStartedAt), b.CapturedAt.Sub(b.DeltaStartedAt)
	small, large := min(first, second), max(first, second)
	if small <= 0 || large-small > small/10 {
		return false
	}
	start, end := a.DeltaStartedAt, a.CapturedAt
	if b.DeltaStartedAt.After(start) {
		start = b.DeltaStartedAt
	}
	if b.CapturedAt.Before(end) {
		end = b.CapturedAt
	}
	return end.Sub(start)*5 >= small*4
}

func exclusion(now time.Time, peer Peer) string {
	if peer.Lifecycle != Ready {
		return string(peer.Lifecycle)
	}
	if peer.SampleState != Available {
		return "source-" + string(peer.SampleState)
	}
	if peer.CapturedAt.IsZero() || peer.CapturedAt.After(now.Add(MaximumSampleSkew)) {
		return "source-clock-uncertain"
	}
	if now.Sub(peer.CapturedAt) > FreshFor {
		return "source-stale"
	}
	if peer.Revision == "" {
		return "revision-unreported"
	}
	if peer.Shape == "" {
		return "container-shape-unreported"
	}
	if peer.Changes == RecentChange || peer.StableSince.IsZero() || now.Sub(peer.StableSince) < StabilityWindow {
		return "recent-or-unreported-stability"
	}
	return ""
}

func referenceExclusion(now time.Time, candidate, reference Peer) string {
	if reason := exclusion(now, reference); reason != "" {
		return reason
	}
	if reference.Revision != candidate.Revision {
		return "revision-mismatch"
	}
	if reference.Shape != candidate.Shape {
		return "container-shape-mismatch"
	}
	difference := reference.CapturedAt.Sub(candidate.CapturedAt)
	if difference < -MaximumSampleSkew || difference > MaximumSampleSkew {
		return "source-clock-skew"
	}
	if adverseReference(reference) {
		return "adverse-reference-evidence"
	}
	return ""
}

func adverseReference(peer Peer) bool {
	for _, metric := range []Metric{OOMRate, OOMKillRate, HighRate, MaxRate, PSIFull} {
		value := peer.Values[metric]
		if value.State == Available && value.Number > 0 {
			return true
		}
	}
	for metric, threshold := range map[Metric]float64{PSISome: 1, LimitUsage: .9} {
		value := peer.Values[metric]
		if value.State == Available && value.Number >= threshold {
			return true
		}
	}
	return false
}
