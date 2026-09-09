package kube

import (
	"context"
	"errors"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
)

type healthDriver struct {
	name         string
	availability volumehealth.Availability
	reason       volumehealth.Reason
}

func missingHealthDriver() healthDriver {
	return healthDriver{availability: volumehealth.Unavailable, reason: volumehealth.BindingUnavailable}
}

func (q *healthQuery) claim(ctx context.Context, pod *corev1.Pod, v corev1.Volume, id volumehealth.Identity) (volumehealth.Observation, healthDriver, error) {
	name := pod.Name + "-" + v.Name
	if v.PersistentVolumeClaim != nil {
		name = v.PersistentVolumeClaim.ClaimName
	}
	o := newHealthObservation(id, volumehealth.ControllerSource)
	if !validHealthName(name) {
		return o, missingHealthDriver(), invalidHealth()
	}
	var pvc corev1.PersistentVolumeClaim
	err := q.get(ctx, "/api/v1/namespaces/"+pod.Namespace+"/persistentvolumeclaims/"+name, &pvc)
	if err != nil {
		o.Availability, o.Reason = healthFailure(err)
		return o, missingHealthDriver(), fatalHealthError(ctx, err)
	}
	if pvc.Kind != "PersistentVolumeClaim" || pvc.APIVersion != "v1" || pvc.Name != name || pvc.Namespace != pod.Namespace || pvc.UID == "" || len(pvc.UID) > 128 {
		return o, missingHealthDriver(), invalidHealth()
	}
	if v.Ephemeral != nil && !ephemeralClaimOwnedBy(&pvc, pod) {
		o.Availability, o.Reason = volumehealth.Unavailable, volumehealth.BindingUnavailable
		return o, missingHealthDriver(), nil
	}
	o.Identity.PVCName, o.Identity.PVCUID = pvc.Name, string(pvc.UID)
	if h := pvc.Status.HealthStatus; h != nil {
		conditions, err := volumeConditions(h.HealthConditions)
		if err != nil {
			return o, missingHealthDriver(), err
		}
		o.Availability, o.Reason = volumehealth.Reported, ""
		o.Conditions, o.TransitionAt = conditions, h.LastTransitionTime.Time
	}
	driver, err := q.boundDriver(ctx, &pvc)
	if err != nil {
		driver.availability, driver.reason = healthFailure(err)
		return o, driver, fatalHealthError(ctx, err)
	}
	return o, driver, nil
}

func (q *healthQuery) boundDriver(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (healthDriver, error) {
	if pvc.Spec.VolumeName == "" {
		return missingHealthDriver(), nil
	}
	if !validHealthName(pvc.Spec.VolumeName) {
		return missingHealthDriver(), invalidHealth()
	}
	var pv corev1.PersistentVolume
	err := q.get(ctx, "/api/v1/persistentvolumes/"+pvc.Spec.VolumeName, &pv)
	if err != nil {
		return missingHealthDriver(), err
	}
	if pv.Kind != "PersistentVolume" || pv.APIVersion != "v1" || pv.Name != pvc.Spec.VolumeName || pv.Namespace != "" || pv.UID == "" {
		return missingHealthDriver(), invalidHealth()
	}
	claim := pv.Spec.ClaimRef
	if claim == nil || claim.Namespace != pvc.Namespace || claim.Name != pvc.Name || claim.UID != pvc.UID {
		return missingHealthDriver(), nil
	}
	if pv.Spec.CSI == nil {
		return healthDriver{availability: volumehealth.Unsupported, reason: volumehealth.NotCSIVolume}, nil
	}
	if !validHealthName(pv.Spec.CSI.Driver) {
		return missingHealthDriver(), invalidHealth()
	}
	return healthDriver{name: pv.Spec.CSI.Driver}, nil
}

func ephemeralClaimOwnedBy(pvc *corev1.PersistentVolumeClaim, pod *corev1.Pod) bool {
	for _, owner := range pvc.OwnerReferences {
		if owner.APIVersion == "v1" && owner.Kind == "Pod" && owner.Name == pod.Name && owner.UID == pod.UID && owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}

func fatalHealthError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return &HealthReadError{volumehealth.ReadFailed, ctx.Err()}
	}
	var readErr *HealthReadError
	if errors.As(err, &readErr) && readErr.Reason == volumehealth.InvalidResponse {
		return err
	}
	return nil
}
