package replicabaseline

import (
	"encoding/hex"
	"math"
	"strings"
	"time"
)

func validState(state DataState) bool {
	switch state {
	case Available, Unreported, Partial, Stale, Unavailable:
		return true
	default:
		return false
	}
}

func validate(input Input) error {
	if input.Workload.Validate() != nil || input.Workload.Kind == "Pod" || input.ObservedAt.IsZero() {
		return ErrInvalid
	}
	if len(input.Peers) > MaxPeers {
		return ErrBounds
	}
	names, identities := map[string]bool{}, map[string]bool{}
	for _, peer := range input.Peers {
		if peer.Object.Validate() != nil || peer.Object.Kind != "Pod" {
			return ErrInvalid
		}
		if peer.Object.Namespace != input.Workload.Namespace || peer.WorkloadUID != input.Workload.UID {
			return ErrScope
		}
		if names[peer.Object.Name] || identities[peer.Object.UID] {
			return ErrInvalid
		}
		names[peer.Object.Name], identities[peer.Object.UID] = true, true
		if err := validatePeer(peer); err != nil {
			return err
		}
	}
	return nil
}

func validatePeer(peer Peer) error {
	if !validState(peer.SampleState) || !validState(peer.HistoryState) || len(peer.Revision) > 256 || strings.IndexFunc(peer.Revision, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return ErrInvalid
	}
	if peer.Shape != "" {
		decoded, err := hex.DecodeString(peer.Shape)
		if err != nil || len(decoded) != 32 || strings.ToLower(peer.Shape) != peer.Shape {
			return ErrInvalid
		}
	}
	switch peer.Lifecycle {
	case Ready, Unready, Terminating, Inactive:
	default:
		return ErrInvalid
	}
	switch peer.Changes {
	case CurrentStable, RecentChange, ChangeUnknown:
	default:
		return ErrInvalid
	}
	if len(peer.History) > MaxHistoryPoints || len(peer.Values) >= len(policies) {
		return ErrBounds
	}
	for metric, value := range peer.Values {
		policy, ok := policyFor(metric)
		if !ok || metric == ChargeSlope || !validState(value.State) || math.IsNaN(value.Number) || math.IsInf(value.Number, 0) || value.Number < 0 || (value.State != Available && value.Number != 0) {
			return ErrInvalid
		}
		if (policy.unit == "bytes" && math.Trunc(value.Number) != value.Number) || (policy.unit == "percent" && value.Number > 100) || (policy.unit == "fraction" && metric != LimitUsage && value.Number > 1) {
			return ErrInvalid
		}
		if value.State == Available && policy.unit == "events-per-second" {
			span := peer.CapturedAt.Sub(peer.DeltaStartedAt)
			if peer.DeltaStartedAt.IsZero() || span <= 0 || span > FreshFor {
				return ErrInvalid
			}
		}
	}
	seen := map[time.Time]bool{}
	for _, point := range peer.History {
		at := point.At.Round(0).UTC()
		if at.IsZero() || at.After(peer.CapturedAt) || seen[at] {
			return ErrInvalid
		}
		current := peer.Values[Charge]
		if peer.HistoryState == Available && current.State == Available && at.Equal(peer.CapturedAt) && float64(point.Bytes) != current.Number {
			return ErrInvalid
		}
		seen[at] = true
	}
	return nil
}
