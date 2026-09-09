package explain

import "github.com/danushkastanley/kube-memlens/internal/api"

func correlateQoS(result *MemoryQoS, container api.ContainerSnapshot) {
	for _, protection := range []struct {
		name     string
		boundary QoSBoundary
	}{{"memory.min", result.Min}, {"memory.low", result.Low}} {
		if protection.boundary.State == BoundaryFinite && result.RequestReference.Known && protection.boundary.Bytes == result.RequestReference.Bytes {
			result.Correlations = append(result.Correlations, protection.name+" matches the container request reference")
		}
	}
	if result.PodConfiguredLimit.Known && !container.Context.MemoryLimitKnown {
		result.Correlations = append(result.Correlations, "The container may inherit Pod limits; an unlimited leaf memory.high does not rule out parent throttling")
	}
	if finiteQoSBoundary(result.High) && finiteQoSBoundary(result.Max) && result.High.Bytes > result.Max.Bytes {
		inconsistentQoS(result, "memory.high is above memory.max")
	}
	if finiteQoSBoundary(result.Min) && finiteQoSBoundary(result.Max) && result.Min.Bytes > result.Max.Bytes {
		inconsistentQoS(result, "memory.min is above memory.max")
	}
	if finiteQoSBoundary(result.High) && result.RequestReference.Known && result.High.Bytes < result.RequestReference.Bytes {
		inconsistentQoS(result, "memory.high is below the container request reference")
	}
	if finiteQoSBoundary(result.Max) && result.LimitReference.Known && result.LimitReference.Bytes > 0 && result.Max.Bytes != result.LimitReference.Bytes {
		inconsistentQoS(result, "memory.max differs from the container limit reference; resize, runtime policy or additional allocations may explain it")
	}
	if result.Min.State == BoundaryFinite && finiteQoSBoundary(result.Max) && result.Min.Bytes == result.Max.Bytes && container.Memory.FileCacheBytes() > 0 {
		result.Caveats = append(result.Caveats, "Hard reclaim protection equals the hard limit with file cache present; protected cache can reduce allocation headroom.")
		result.SuggestedChecks = append(result.SuggestedChecks, "Review protected page-cache headroom before changing requests or limits.")
	}
	switch result.Activity {
	case ThrottleCrossed, ThrottleCrossedWithStalls:
		result.Caveats = append(result.Caveats, "Controls are point-in-time values; the recent crossing may predate a boundary change.")
		result.SuggestedChecks = append(result.SuggestedChecks, "Correlate the exact high-event delta window and PSI with workload latency; do not infer a leak or an automatic sizing change.")
	case ThrottleStallsOnly:
		result.Caveats = append(result.Caveats, "PSI without a recent local high crossing can reflect parent pressure or contention; it is not proof of leaf throttling.")
	}
}

func inconsistentQoS(result *MemoryQoS, evidence string) {
	result.State, result.Confidence = QoSInconsistent, ConfidenceLow
	result.Correlations = append(result.Correlations, evidence)
	result.SuggestedChecks = appendUnique(result.SuggestedChecks, "Compare fresh cgroup controls with kubelet-applied resources and resize generations before changing configuration.")
}

func finiteQoSBoundary(boundary QoSBoundary) bool {
	return boundary.State == BoundaryFinite || boundary.State == BoundaryZero
}
