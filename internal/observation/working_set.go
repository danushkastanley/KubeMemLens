package observation

import (
	"math"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

const (
	IncompleteMetrics   capability.Reason = "incomplete-metrics"
	IncompatibleMetrics capability.Reason = "incompatible-metrics"
	QuantityOverflow    capability.Reason = "quantity-overflow"
	IdentityUnconfirmed capability.Reason = "identity-unconfirmed"
	IdentityMismatch    capability.Reason = "identity-mismatch"
)

// SumWorkingSets reports the observed sum with its coverage. It never supplies
// zero for a group without measurements or a sum which overflowed its domain.
func SumWorkingSets(values []WorkingSet, scope capability.Scope) WorkingSet {
	result := WorkingSet{Availability: capability.Unreported, Reason: capability.NotObserved,
		Evidence: capability.Envelope{Source: capability.KubernetesMetrics, Scope: scope, Freshness: capability.UnknownFreshness, Completeness: capability.Partial},
		Coverage: Coverage{Unit: capability.ContainerScope}}
	var sum uint64
	measured := 0
	missing := 0
	partial := false
	for _, value := range values {
		if value.Coverage.Expected < 0 || value.Coverage.Reported < 0 || value.Coverage.Reported > value.Coverage.Expected || value.Coverage.Expected > math.MaxInt-result.Coverage.Expected || value.Coverage.Reported > math.MaxInt-result.Coverage.Reported {
			return failedSum(result, IncompleteMetrics)
		}
		result.Coverage.Expected += value.Coverage.Expected
		result.Coverage.Reported += value.Coverage.Reported
		if value.Evidence.ReceivedAt.After(result.Evidence.ReceivedAt) {
			result.Evidence.ReceivedAt = value.Evidence.ReceivedAt
		}
		if value.Bytes == nil {
			partial = true
			missing++
			availability, reason := value.Availability, value.Reason
			if availability == "" {
				availability = capability.Unreported
			}
			if reason == "" {
				reason = capability.NotObserved
			}
			if missing == 1 {
				result.Availability, result.Reason = availability, reason
			} else if result.Availability != availability || result.Reason != reason {
				result.Availability, result.Reason = capability.Unavailable, IncompleteMetrics
			}
			continue
		}
		if measured == 0 {
			result.Evidence.APIVersion = value.Evidence.APIVersion
			result.Evidence.Source = value.Evidence.Source
			result.Evidence.Stability = value.Evidence.Stability
			result.Evidence.Freshness = value.Evidence.Freshness
			result.Coverage.Unit = value.Coverage.Unit
		} else if value.Evidence.Source != result.Evidence.Source || value.Evidence.APIVersion != result.Evidence.APIVersion || value.Coverage.Unit != result.Coverage.Unit {
			return failedSum(result, IncompatibleMetrics)
		}
		if *value.Bytes > math.MaxInt64-sum {
			return failedSum(result, QuantityOverflow)
		}
		sum += *value.Bytes
		measured++
		partial = partial || value.Evidence.Completeness != capability.Complete
		if result.Evidence.CapturedAt.IsZero() || value.Evidence.CapturedAt.Before(result.Evidence.CapturedAt) {
			result.Evidence.CapturedAt = value.Evidence.CapturedAt
		}
		latest := value.LatestSampleAt
		if latest.IsZero() {
			latest = value.Evidence.CapturedAt
		}
		if latest.After(result.LatestSampleAt) {
			result.LatestSampleAt = latest
		}
		switch {
		case value.Evidence.Freshness == capability.Stale:
			result.Evidence.Freshness = capability.Stale
		case value.Evidence.Freshness != capability.Fresh && result.Evidence.Freshness != capability.Stale:
			result.Evidence.Freshness = capability.UnknownFreshness
		}
	}
	if measured == 0 {
		return result
	}
	result.Bytes = &sum
	result.Availability, result.Reason = capability.Available, ""
	result.Evidence.Completeness = capability.Complete
	if partial || result.Coverage.Reported != result.Coverage.Expected {
		result.Evidence.Completeness = capability.Partial
		result.Reason = IncompleteMetrics
	}
	if !result.Evidence.CapturedAt.Equal(result.LatestSampleAt) {
		result.Evidence.Caveats = []string{"The aggregate contains samples from different instants."}
	}
	return result
}

func failedSum(result WorkingSet, reason capability.Reason) WorkingSet {
	result.Bytes = nil
	result.Availability, result.Reason = capability.Unavailable, reason
	result.Evidence.Completeness = capability.Partial
	return result
}
