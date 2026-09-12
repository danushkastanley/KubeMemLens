package kube

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

type claimHealthSeed struct {
	controller volumehealth.Observation
	backend    volumehealth.Availability
	reason     volumehealth.Reason
}

// NewHealthVolumeResolver explicitly owns the status cache and its cleanup.
// Live bindings and every caller's authorisation remain outside cached status.
func NewHealthVolumeResolver(ctx context.Context, config *rest.Config, authorize VolumeAuthorizer, nodeUID VolumeNodeIdentity) (VolumeResolver, error) {
	r, err := newVolumeResolver(config, authorize, nodeUID)
	if err != nil {
		return nil, err
	}
	r.health = newHealthCache()
	r.queries = make(chan struct{}, 4)
	r.calls = rate.NewLimiter(20, 40)
	go func() { r.health.run(ctx); r.reader.client.CloseIdleConnections() }()
	return r, nil
}

func claimHealth(pvc corev1.PersistentVolumeClaim, pv corev1.PersistentVolume, pvAccess error) claimHealthSeed {
	o := newHealthObservation(volumehealth.Identity{}, volumehealth.ControllerSource)
	if h := pvc.Status.HealthStatus; h != nil {
		conditions, err := volumeConditions(h.HealthConditions)
		if err != nil {
			o.Availability, o.Reason = volumehealth.Unavailable, volumehealth.InvalidResponse
		} else {
			o.Availability, o.Reason, o.Conditions, o.TransitionAt = volumehealth.Reported, "", conditions, h.LastTransitionTime.Time
		}
	}
	seed := claimHealthSeed{controller: safeHealth(o), backend: volumehealth.Reported}
	if pvAccess != nil {
		seed.backend, seed.reason = healthFailure(pvAccess)
	} else if pv.Spec.CSI == nil {
		seed.backend, seed.reason = volumehealth.Unsupported, volumehealth.NotCSIVolume
	}
	return seed
}

func (r *volumeResolver) resolveHealth(ctx context.Context, q *volumeBindingQuery, pod *corev1.Pod, resolved ResolvedVolumes) []volumecontext.HealthObservation {
	rows := []volumecontext.HealthObservation{}
	for _, b := range resolved.Bindings {
		if b.Configuration.Kind != volumecontext.PersistentClaim && b.Configuration.Kind != volumecontext.EphemeralClaim && b.Configuration.Kind != volumecontext.InlineCSI {
			continue
		}
		id := volumehealth.Identity{Namespace: resolved.Scope.Namespace, PodName: resolved.Scope.PodName, PodUID: resolved.Scope.PodUID, NodeName: resolved.Scope.NodeName, VolumeName: b.VolumeName, PVCName: b.PVCName, PVCUID: b.PVCUID, Driver: b.Driver}
		sources := []volumehealth.Source{volumehealth.PodSource, volumehealth.BackendSource}
		if b.Configuration.Kind != volumecontext.InlineCSI {
			sources = append(sources, volumehealth.ControllerSource)
		}
		for _, source := range sources {
			o := newHealthObservation(id, source)
			if r.health == nil {
				o.Availability, o.Reason = volumehealth.Disabled, ""
				rows = append(rows, volumecontext.HealthObservation{Observation: o, NodeUID: resolved.Scope.NodeUID})
				continue
			}
			key := healthKey(resolved.Scope, b, source)
			var result volumecontext.HealthObservation
			switch source {
			case volumehealth.PodSource:
				value, err := podVolumeHealth(pod, id)
				if err != nil {
					value = o
					value.Availability, value.Reason = volumehealth.Unavailable, volumehealth.InvalidResponse
				}
				result = r.retainHealth(key, safeHealth(value))
			case volumehealth.ControllerSource:
				if b.ClaimAvailability != volumehealth.Reported {
					result = unboundHealth(o, b, resolved.Scope.NodeUID)
				} else {
					result = r.retainHealth(key, q.healthSeeds[b.VolumeName].controller)
				}
			case volumehealth.BackendSource:
				result = r.backendHealth(ctx, q, key, o, b)
			}
			result.Identity = id
			if result.LastGood != nil {
				result.LastGood.Identity = id
			}
			rows = append(rows, result)
		}
	}
	return rows
}

func healthKey(scope volumecontext.PodScope, b volumecontext.Binding, source volumehealth.Source) healthCacheKey {
	k := healthCacheKey{source: source, nodeName: scope.NodeName, nodeUID: scope.NodeUID, driver: b.Driver}
	if source != volumehealth.BackendSource {
		k.namespace, k.podUID, k.volumeName = scope.Namespace, scope.PodUID, b.VolumeName
		k.pvcUID = b.PVCUID
	}
	return k
}

func (r *volumeResolver) retainHealth(key healthCacheKey, o volumehealth.Observation) volumecontext.HealthObservation {
	now := time.Now().UTC()
	result, err := r.health.observe(key, safeHealth(o), now)
	if err == nil {
		return result
	}
	return r.health.unavailable(key, now)
}

func (r *volumeResolver) backendHealth(ctx context.Context, q *volumeBindingQuery, key healthCacheKey, o volumehealth.Observation, b volumecontext.Binding) volumecontext.HealthObservation {
	if b.Configuration.Kind != volumecontext.InlineCSI {
		if b.ClaimAvailability != volumehealth.Reported {
			return unboundHealth(o, b, key.nodeUID)
		}
		seed := q.healthSeeds[b.VolumeName]
		if seed.backend != volumehealth.Reported {
			o.Availability, o.Reason = seed.backend, seed.reason
			return volumecontext.HealthObservation{Observation: o, NodeUID: key.nodeUID}
		}
	}
	if err := q.authorize(ctx, VolumeAccess{Group: "storage.k8s.io", Resource: "csinodes", Name: o.Identity.NodeName}); err != nil {
		o.Availability, o.Reason = healthFailure(err)
		return volumecontext.HealthObservation{Observation: o, NodeUID: key.nodeUID}
	}
	result, err := r.health.backend(ctx, key, func(ctx context.Context) (volumehealth.Observation, error) {
		return q.healthQuery.backend(ctx, o.Identity)
	}, time.Now().UTC())
	if err == nil {
		return result
	}
	return r.health.unavailable(key, time.Now().UTC())
}

func unboundHealth(o volumehealth.Observation, b volumecontext.Binding, nodeUID string) volumecontext.HealthObservation {
	o.Availability = b.ClaimAvailability
	o.Reason = map[volumehealth.Availability]volumehealth.Reason{volumehealth.Forbidden: volumehealth.AccessDenied, volumehealth.Unreported: volumehealth.NoReport, volumehealth.Unavailable: volumehealth.BindingUnavailable}[b.ClaimAvailability]
	return volumecontext.HealthObservation{Observation: o, NodeUID: nodeUID}
}

func safeHealth(o volumehealth.Observation) volumehealth.Observation {
	clean, err := volumecontext.SanitiseHealth(o, time.Now().UTC())
	if err == nil {
		return clean
	}
	failure := newHealthObservation(o.Identity, o.Source)
	failure.Availability, failure.Reason = volumehealth.Unavailable, volumehealth.InvalidResponse
	return failure
}
