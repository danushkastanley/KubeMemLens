package kube

import (
	"context"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	storagev1 "k8s.io/api/storage/v1"
)

func (q *healthQuery) backend(ctx context.Context, id volumehealth.Identity) (volumehealth.Observation, error) {
	o := newHealthObservation(id, volumehealth.BackendSource)
	if id.NodeName == "" {
		o.Reason = volumehealth.NotScheduled
		return o, nil
	}
	if id.Driver == "" {
		o.Availability, o.Reason = volumehealth.Unavailable, volumehealth.BindingUnavailable
		return o, nil
	}
	if !validHealthName(id.NodeName) || !validHealthName(id.Driver) {
		return o, invalidHealth()
	}
	var node storagev1.CSINode
	err := q.get(ctx, "/apis/storage.k8s.io/v1/csinodes/"+id.NodeName, &node)
	if err != nil {
		o.Availability, o.Reason = healthFailure(err)
		return o, fatalHealthError(ctx, err)
	}
	if node.Kind != "CSINode" || node.APIVersion != "storage.k8s.io/v1" || node.Name != id.NodeName || node.Namespace != "" || node.UID == "" || len(node.Spec.Drivers) > 128 || len(node.Status.StorageHealth) > 128 {
		return o, invalidHealth()
	}
	registered := false
	for _, driver := range node.Spec.Drivers {
		registered = registered || driver.Name == id.Driver
	}
	if !registered {
		return o, nil
	}
	for _, health := range node.Status.StorageHealth {
		if health.Name != id.Driver {
			continue
		}
		if o.Availability == volumehealth.Reported || len(health.HealthConditions) > volumehealth.MaxConditions {
			return o, invalidHealth()
		}
		o.Availability, o.Reason = volumehealth.Reported, ""
		for _, c := range health.HealthConditions {
			if len(c.Status) == 0 || len(c.Status) > 256 {
				return o, invalidHealth()
			}
			condition := volumehealth.Condition{Status: volumehealth.Status(c.Status), Reason: c.Reason, Message: c.Message, TransitionAt: c.LastTransitionTime.Time}
			if c.AccessMode != nil {
				condition.AccessMode = string(*c.AccessMode)
			}
			if c.VolumeMode != nil {
				condition.VolumeMode = string(*c.VolumeMode)
			}
			o.Conditions = append(o.Conditions, condition)
		}
	}
	return o, nil
}
