package replicabaseline

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
)

// Validate checks a received report before its evidence is rendered. In
// particular, ranges, scores and outlier labels are recomputed from the named
// reference values rather than accepted as independent claims.
func (r Report) Validate() error {
	if r.SchemaVersion != 1 || r.PolicyVersion != 1 || r.Workload.Validate() != nil || r.Workload.Kind == "Pod" || r.ObservedAt.IsZero() {
		return ErrInvalid
	}
	if len(r.Peers) > MaxPeers || len(r.Caveats) > 8 {
		return ErrBounds
	}
	for _, caveat := range r.Caveats {
		if len(caveat) > 512 || strings.IndexFunc(caveat, func(r rune) bool { return r < 32 || r > 126 }) >= 0 {
			return ErrInvalid
		}
	}
	peers := map[string]PeerReport{}
	names := map[string]bool{}
	for _, p := range r.Peers {
		if len(p.Excluded) >= MaxPeers || len(p.Comparisons) > len(policies) {
			return ErrBounds
		}
		if p.Peer.Validate() != nil || p.Peer.Kind != "Pod" || p.Peer.Namespace != r.Workload.Namespace || names[p.Peer.Name] {
			return ErrInvalid
		}
		if _, exists := peers[p.Peer.UID]; exists {
			return ErrInvalid
		}
		peers[p.Peer.UID] = p
		names[p.Peer.Name] = true
		base := Peer{Object: p.Peer, Revision: p.Revision, Shape: p.Shape, Lifecycle: Ready, Changes: ChangeUnknown, SampleState: Available, HistoryState: Unreported}
		if validatePeer(base) != nil {
			return ErrInvalid
		}
		if p.Exclusion != "" {
			if !validExclusion(p.Exclusion) || len(p.Comparisons) != 0 || len(p.Excluded) != 0 || p.History != nil || !p.DeltaStartedAt.IsZero() {
				return ErrInvalid
			}
			continue
		}
		if p.CapturedAt.IsZero() || p.CapturedAt.After(r.ObservedAt.Add(MaximumSampleSkew)) || r.ObservedAt.Sub(p.CapturedAt) > FreshFor || p.StableSince.IsZero() || r.ObservedAt.Sub(p.StableSince) < StabilityWindow || p.Shape == "" || p.Revision == "" || len(p.Comparisons) != len(policies) {
			return ErrInvalid
		}
		if p.History != nil {
			h := p.History
			if h.Samples < 1 || h.Samples > MaxHistoryPoints || h.StartedAt.IsZero() || h.StartedAt.Before(p.StableSince) || h.StartedAt.Before(p.CapturedAt.Add(-HistoryLookback)) || h.EndedAt.Before(h.StartedAt) || h.EndedAt.After(p.CapturedAt) {
				return ErrInvalid
			}
		}
		if !p.DeltaStartedAt.IsZero() && (p.CapturedAt.Sub(p.DeltaStartedAt) <= 0 || p.CapturedAt.Sub(p.DeltaStartedAt) > FreshFor) {
			return ErrInvalid
		}
		for i, c := range p.Comparisons {
			if c.Metric != policies[i].metric || c.Unit != policies[i].unit {
				return ErrInvalid
			}
			if c.Candidate != nil && !validMetricNumber(c.Metric, *c.Candidate) {
				return ErrInvalid
			}
		}
	}
	for _, p := range r.Peers {
		if p.Exclusion != "" {
			continue
		}
		for _, c := range p.Comparisons {
			if err := validateComparison(p, c, peers); err != nil {
				return err
			}
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return ErrInvalid
	}
	if len(encoded) > MaxResponseBytes {
		return ErrBounds
	}
	return nil
}

func validMetricNumber(metric Metric, n float64) bool {
	if math.IsNaN(n) || math.IsInf(n, 0) || (metric != ChargeSlope && n < 0) {
		return false
	}
	policy, _ := policyFor(metric)
	return !(policy.unit == "bytes" && math.Trunc(n) != n || policy.unit == "percent" && n > 100 || policy.unit == "fraction" && metric != LimitUsage && n > 1)
}

func validateComparison(p PeerReport, c Comparison, peers map[string]PeerReport) error {
	if len(c.References) >= MaxPeers || len(c.Omitted) >= MaxPeers {
		return ErrBounds
	}
	accounted := map[string]bool{p.Peer.UID: true}
	for _, excluded := range p.Excluded {
		other, found := peers[excluded.Peer.UID]
		if !found || other.Peer != excluded.Peer || accounted[other.Peer.UID] || !validExclusion(excluded.Reason) {
			return ErrInvalid
		}
		accounted[other.Peer.UID] = true
	}
	if c.Candidate == nil {
		if !slices.Contains([]string{string(Unreported), string(Partial), string(Stale), string(Unavailable)}, c.State) || c.Distribution != nil || c.Difference != nil || c.ModifiedScore != nil || c.Method != "" || c.Outlier != "none" || c.Confidence != "insufficient" || len(c.References) != 0 || len(c.Omitted) != 0 {
			return ErrInvalid
		}
		return nil
	}
	values := []float64{}
	for _, uid := range c.References {
		other, found := peers[uid]
		if !found || accounted[uid] || other.Exclusion != "" || other.Revision != p.Revision || other.Shape != p.Shape || (other.CapturedAt.Sub(p.CapturedAt) > MaximumSampleSkew || p.CapturedAt.Sub(other.CapturedAt) > MaximumSampleSkew) {
			return ErrInvalid
		}
		var value *float64
		for _, measurement := range other.Comparisons {
			if measurement.Metric == c.Metric {
				value = measurement.Candidate
			}
		}
		if value == nil {
			return ErrInvalid
		}
		if policy, _ := policyFor(c.Metric); policy.unit == "events-per-second" && !alignedDeltaWindows(Peer{CapturedAt: p.CapturedAt, DeltaStartedAt: p.DeltaStartedAt}, Peer{CapturedAt: other.CapturedAt, DeltaStartedAt: other.DeltaStartedAt}) {
			return ErrInvalid
		}
		accounted[uid] = true
		values = append(values, *value)
	}
	for _, omitted := range c.Omitted {
		other, found := peers[omitted.PeerUID]
		if !found || accounted[omitted.PeerUID] || other.Exclusion != "" || !slices.Contains([]string{"metric-unreported", "metric-partial", "metric-stale", "metric-unavailable", "delta-window-mismatch"}, omitted.Reason) {
			return ErrInvalid
		}
		accounted[omitted.PeerUID] = true
	}
	if len(accounted) != len(peers) {
		return ErrInvalid
	}
	policy, _ := policyFor(c.Metric)
	expected := compare(policy, *c.Candidate, values, c.References)
	expected.Omitted = c.Omitted
	if expected.State == "compared" && c.Confidence == "limited-change-history" {
		expected.Confidence = c.Confidence
	}
	if !reflect.DeepEqual(expected, c) {
		return ErrInvalid
	}
	if c.Metric == ChargeSlope {
		h := p.History
		if h == nil || h.Samples < MinHistoryPoints || h.EndedAt.Sub(h.StartedAt) < StabilityWindow || h.StartedAt.After(p.CapturedAt.Add(-HistoryLookback+MaximumHistoryGap)) || p.CapturedAt.Sub(h.EndedAt) > MaximumSampleSkew {
			return ErrInvalid
		}
	}
	if policy.unit == "events-per-second" && p.DeltaStartedAt.IsZero() {
		return ErrInvalid
	}
	return nil
}

func validExclusion(reason string) bool {
	return slices.Contains([]string{"source-unreported", "source-partial", "source-stale", "source-unavailable", "source-clock-uncertain", "revision-unreported", "container-shape-unreported", "recent-or-unreported-stability", "revision-mismatch", "container-shape-mismatch", "source-clock-skew", "adverse-reference-evidence", "unready", "terminating", "inactive"}, reason)
}
