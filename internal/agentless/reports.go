package agentless

import (
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
)

func statusReport(scope capability.Scope) observation.SourceReport {
	return observation.SourceReport{Scope: scope, SourceState: capability.SourceState{Source: capability.KubernetesStatus,
		Availability: capability.Available, APIVersion: "v1", Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Stable}}
}

func metricsReport(availability resourcemetrics.Availability, reason resourcemetrics.Reason, version string, scope capability.Scope) observation.SourceReport {
	state := observation.SourceReport{Scope: scope, SourceState: capability.SourceState{Source: capability.KubernetesMetrics,
		Availability: capability.Unavailable, Reason: capability.Reason(reason), APIVersion: version,
		Freshness: capability.UnknownFreshness, Completeness: capability.Partial, Stability: metricsStability(version)}}
	switch availability {
	case resourcemetrics.Available:
		state.Availability, state.Freshness, state.Completeness = capability.Available, capability.Fresh, capability.Complete
	case resourcemetrics.Partial:
		state.Availability = capability.Available
	case resourcemetrics.Stale:
		state.Availability, state.Freshness = capability.Available, capability.Stale
	case resourcemetrics.Forbidden:
		state.Availability, state.Reason = capability.Forbidden, capability.AccessDenied
	case resourcemetrics.MissingProvider:
		state.Availability, state.Reason = capability.Absent, capability.SourceAbsent
	}
	if reason == resourcemetrics.UnsupportedAPI {
		state.Availability = capability.Unsupported
	}
	return state
}

func applyCoverage(report *observation.SourceReport, quantities []observation.WorkingSet) {
	for _, value := range quantities {
		if value.Evidence.Completeness != capability.Complete {
			report.Completeness = capability.Partial
		}
		switch {
		case value.Evidence.Freshness == capability.Stale:
			report.Freshness = capability.Stale
		case value.Evidence.Freshness != capability.Fresh && report.Freshness != capability.Stale:
			report.Freshness = capability.UnknownFreshness
		}
	}
	if len(quantities) == 0 {
		report.Freshness = capability.UnknownFreshness
	}
}
