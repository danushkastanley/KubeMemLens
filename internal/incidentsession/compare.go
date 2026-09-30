package incidentsession

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// CompareCaptures records the operator's ordered selection of two retained
// captures. The payloads remain the source for presentation's existing memory
// comparison rules. Identity and clock discontinuities are separate visible gaps.
func (s *Store) CompareCaptures(ctx context.Context, p Principal, id, beforeDigest, afterDigest string) (Summary, error) {
	if err := s.authorize(ctx, p, Compare, id); err != nil {
		return Summary{}, err
	}
	if !digest(beforeDigest, 32) || !digest(afterDigest, 32) {
		return Summary{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(ctx); err != nil {
		return Summary{}, err
	}
	r, err := s.find(p, id)
	if err != nil {
		return Summary{}, err
	}
	if r.closed != nil {
		return Summary{}, ErrClosed
	}
	before, after := findCapture(r, beforeDigest), findCapture(r, afterDigest)
	if before == nil || after == nil {
		return Summary{}, ErrNotFound
	}
	if !compatibleCaptureSchemas(before.SchemaVersion, after.SchemaVersion) {
		return Summary{}, ErrInvalid
	}
	changes, err := captureChanges(*before, *after)
	if err != nil {
		return Summary{}, err
	}
	now := s.clock.Now()
	inputs := []Input{{Kind: Compared, Source: "operator", ObservedAt: now, References: []CaptureReference{before.reference(), after.reference()}}}
	if changes.identity {
		inputs = append(inputs, Input{Kind: Gap, Source: "collector", GapReason: "source-changed"})
	}
	if changes.clock {
		inputs = append(inputs, Input{Kind: Gap, Source: "collector", GapReason: "clock-uncertain"})
	}
	// Commit the action and all required gaps together, preserving close capacity.
	next := *r
	for _, input := range inputs {
		if err := s.add(ctx, &next, input, now); err != nil {
			if err == ErrCapacity {
				r.limitReached = true
			}
			return Summary{}, err
		}
	}
	*r = next
	return r.summary(), nil
}

func findCapture(r *record, digest string) *CapturedEvidence {
	for i := range r.captures {
		if r.captures[i].Digest == digest {
			return &r.captures[i]
		}
	}
	return nil
}

func compatibleCaptureSchemas(before, after int) bool {
	return before == after || ((before == 1 || before == 2) && (after == 1 || after == 2))
}

type captureDiscontinuities struct{ identity, clock bool }

func captureChanges(before, after CapturedEvidence) (captureDiscontinuities, error) {
	change := captureDiscontinuities{clock: after.ObservedAt.Before(before.ObservedAt)}
	if before.SchemaVersion == api.HistoryIncidentSchemaVersion {
		var left, right api.HistoryIncident
		if json.Unmarshal(before.Data, &left) != nil || json.Unmarshal(after.Data, &right) != nil {
			return change, ErrInvalid
		}
		a, b := left.Context.History, right.Context.History
		change.identity = a.Selection.Request != b.Selection.Request || a.Selection.UID != b.Selection.UID || a.Query.Source != b.Query.Source || a.Query.Metric != b.Query.Metric || !slices.Equal(a.Selection.Targets, b.Selection.Targets)
		change.clock = change.clock || right.Context.Changes.ObservedAt.Before(left.Context.Changes.ObservedAt) || b.ReceivedAt.Before(a.ReceivedAt)
		return change, nil
	}
	var left, right api.IncidentBundle
	if json.Unmarshal(before.Data, &left) != nil || json.Unmarshal(after.Data, &right) != nil || len(left.Pods) != 1 || len(right.Pods) != 1 {
		return change, ErrInvalid
	}
	a, b := left.Pods[0], right.Pods[0]
	change.identity = a.Namespace != b.Namespace || a.PodName != b.PodName || a.PodUID != b.PodUID || a.NodeName != b.NodeName
	change.clock = change.clock || b.CapturedAt.Before(a.CapturedAt)
	return change, nil
}
