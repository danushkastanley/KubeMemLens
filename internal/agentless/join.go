package agentless

import (
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
)

type metricSample struct {
	bytes          uint64
	version        string
	at             time.Time
	window         time.Duration
	freshness      resourcemetrics.Freshness
	identityReason capability.Reason
}

func joinPodMetrics(pods []observation.Pod, report resourcemetrics.Report, receivedAt time.Time) {
	byContainer := make(map[string]resourcemetrics.Observation, len(report.Observations))
	for _, value := range report.Observations {
		key := value.Identity.Namespace + "/" + value.Identity.PodName + "/" + value.Identity.ContainerName
		byContainer[key] = value
	}
	for p := range pods {
		pod := &pods[p]
		quantities := make([]observation.WorkingSet, 0, len(pod.Containers))
		for c := range pod.Containers {
			container := &pod.Containers[c]
			quantity := missingSet(report.Availability, report.Reason, report.APIVersion, capability.ContainerScope, receivedAt)
			value, exists := byContainer[pod.Namespace+"/"+pod.Name+"/"+container.Name]
			if exists {
				quantity = joinedContainer(*pod, *container, value, receivedAt)
			}
			container.WorkingSet = quantity
			quantities = append(quantities, quantity)
		}
		pod.WorkingSet = observation.SumWorkingSets(quantities, capability.PodScope)
	}
}

func joinedContainer(pod observation.Pod, container observation.Container, value resourcemetrics.Observation, receivedAt time.Time) observation.WorkingSet {
	if (value.Identity.PodUID != "" && value.Identity.PodUID != pod.UID) || value.Timestamp.Before(pod.Context.CreatedAt) || (!container.StartedAt.IsZero() && value.Timestamp.Before(container.StartedAt)) {
		quantity := missingSet(resourcemetrics.Unavailable, resourcemetrics.InvalidResponse, value.APIVersion, capability.ContainerScope, receivedAt)
		quantity.Reason = observation.IdentityMismatch
		return quantity
	}
	sample := metricSample{bytes: value.MemoryWorkingSetBytes, version: value.APIVersion, at: value.Timestamp, window: value.Window, freshness: value.Freshness}
	if value.Identity.PodUID == "" {
		sample.identityReason = observation.IdentityUnconfirmed
	}
	return sampledSet(sample, capability.ContainerScope, receivedAt)
}

func sampledSet(sample metricSample, scope capability.Scope, receivedAt time.Time) observation.WorkingSet {
	bytes := sample.bytes
	quantity := observation.WorkingSet{Bytes: &bytes, Availability: capability.Available, Coverage: observation.Coverage{Reported: 1, Expected: 1, Unit: scope},
		Evidence: capability.Envelope{Source: capability.KubernetesMetrics, APIVersion: sample.version, CapturedAt: sample.at, ReceivedAt: receivedAt,
			Window: sample.window, Scope: scope, Freshness: capability.Fresh, Completeness: capability.Complete, Stability: metricsStability(sample.version)}}
	switch sample.freshness {
	case resourcemetrics.Old:
		quantity.Evidence.Freshness = capability.Stale
	case resourcemetrics.Future:
		quantity.Evidence.Freshness = capability.UnknownFreshness
		quantity.Evidence.Completeness = capability.Partial
		quantity.Evidence.Caveats = append(quantity.Evidence.Caveats, "The provider sample time is in the future.")
	}
	if sample.identityReason != "" {
		quantity.Reason = sample.identityReason
		quantity.Evidence.Completeness = capability.Partial
		quantity.Evidence.Caveats = append(quantity.Evidence.Caveats, "The provider did not confirm the current object UID; a name match does not prove continuity.")
	}
	return quantity
}

func missingSet(availability resourcemetrics.Availability, reason resourcemetrics.Reason, version string, scope capability.Scope, receivedAt time.Time) observation.WorkingSet {
	quantity := observation.WorkingSet{Availability: capability.Unreported, Reason: capability.NotObserved, Coverage: observation.Coverage{Expected: 1, Unit: scope},
		Evidence: capability.Envelope{Source: capability.KubernetesMetrics, APIVersion: version, ReceivedAt: receivedAt, Scope: scope,
			Freshness: capability.UnknownFreshness, Completeness: capability.Partial, Stability: metricsStability(version)}}
	switch availability {
	case resourcemetrics.Forbidden:
		quantity.Availability, quantity.Reason = capability.Forbidden, capability.AccessDenied
	case resourcemetrics.MissingProvider:
		quantity.Availability, quantity.Reason = capability.Absent, capability.SourceAbsent
	case resourcemetrics.Unavailable:
		quantity.Availability, quantity.Reason = capability.Unavailable, capability.Reason(reason)
	case resourcemetrics.Partial:
		if reason == resourcemetrics.LimitReached {
			quantity.Availability, quantity.Reason = capability.Unavailable, limitReached
		}
	}
	return quantity
}

func metricsStability(version string) capability.Stability {
	if strings.HasSuffix(version, "/v1beta1") {
		return capability.Beta
	}
	return capability.Stable
}
