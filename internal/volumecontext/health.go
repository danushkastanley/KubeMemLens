package volumecontext

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func namedHealth(input volumehealth.Observation) HealthReport {
	h := HealthReport{Observation: input, TransitionAt: input.TransitionAt}
	h.Observation.Identity = volumehealth.Identity{}
	h.Observation.Conditions = nil
	h.Observation.TransitionAt = time.Time{}
	for _, c := range input.Conditions {
		h.Conditions = append(h.Conditions, Condition{Status: c.Status, Reason: c.Reason, TransitionAt: c.TransitionAt, AccessMode: c.AccessMode, VolumeMode: c.VolumeMode})
	}
	return h
}

// SanitiseHealth validates and removes messages before a status payload enters
// retention. Identity remains private until the authorised join/export boundary.
func SanitiseHealth(input volumehealth.Observation, now time.Time) (volumehealth.Observation, error) {
	if input.ObservedAt.IsZero() || input.ObservedAt.After(now.Add(FutureSkew)) {
		return volumehealth.Observation{}, ErrInvalid
	}
	o := volumehealth.Evaluate(input, now)
	if err := validateHealth(o); err != nil {
		return volumehealth.Observation{}, err
	}
	for i := range o.Conditions {
		o.Conditions[i].Message = ""
	}
	return o, nil
}

func evaluateHealth(input HealthObservation, scope PodScope, binding Binding, now time.Time) (HealthObservation, error) {
	value, err := SanitiseHealth(input.Observation, now)
	if err != nil {
		return HealthObservation{}, err
	}
	result := HealthObservation{Observation: value, NodeUID: input.NodeUID}
	if input.LastGood == nil {
		return result, nil
	}
	last := *input.LastGood
	if (input.Availability != volumehealth.Unavailable && input.Availability != volumehealth.Unreported) || last.Availability != volumehealth.Reported || last.Source != input.Source || last.Scope != input.Scope || last.Identity != input.Identity || last.ObservedAt.After(input.ObservedAt) {
		return HealthObservation{}, ErrInvalid
	}
	if last.ObservedAt.IsZero() || last.ObservedAt.After(now.Add(FutureSkew)) {
		return HealthObservation{}, ErrInvalid
	}
	if last.Source != volumehealth.BackendSource && last.ObservedAt.Before(scope.CreatedAt) {
		return HealthObservation{}, ErrScope
	}
	if last.Source == volumehealth.ControllerSource && last.ObservedAt.Before(binding.PVCCreatedAt) {
		return HealthObservation{}, ErrScope
	}
	if now.Sub(last.ObservedAt) > ExpireAfter {
		return result, nil
	}
	last, err = SanitiseHealth(last, now)
	if err != nil {
		return HealthObservation{}, err
	}
	// A failed/missing current source cannot promote prior evidence to current.
	last.State, last.ObservationFreshness = volumehealth.StateStale, volumehealth.Stale
	result.LastGood = &last
	return result, nil
}
