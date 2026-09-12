package kube

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
)

func (q *volumeBindingQuery) binding(ctx context.Context, pod *corev1.Pod, v corev1.Volume, mounts mountCounts) (volumecontext.Binding, error) {
	b := volumecontext.Binding{VolumeName: v.Name, Configuration: volumecontext.Configuration{Kind: volumecontext.Other, MountCount: mounts.total, ReadOnlyMountCount: mounts.readOnly}}
	switch {
	case v.EmptyDir != nil:
		b.Configuration.Kind = volumecontext.EmptyDir
		b.Configuration.MemoryBacked = v.EmptyDir.Medium == corev1.StorageMediumMemory
		if v.EmptyDir.SizeLimit != nil {
			value, ok := resourcemetrics.QuantityBytes(v.EmptyDir.SizeLimit.String())
			if !ok {
				return b, invalidHealth()
			}
			b.Configuration.SizeLimitBytes = &value
		}
	case v.CSI != nil:
		b.Configuration.Kind = volumecontext.InlineCSI
		b.Driver = v.CSI.Driver
		if v.CSI.ReadOnly != nil && *v.CSI.ReadOnly {
			b.Configuration.ReadOnlyMountCount = b.Configuration.MountCount
		}
	case v.PersistentVolumeClaim != nil || v.Ephemeral != nil:
		b.Configuration.Kind = volumecontext.PersistentClaim
		name := pod.Name + "-" + v.Name
		if v.PersistentVolumeClaim != nil {
			name = v.PersistentVolumeClaim.ClaimName
			if v.PersistentVolumeClaim.ReadOnly {
				b.Configuration.ReadOnlyMountCount = b.Configuration.MountCount
			}
		} else {
			b.Configuration.Kind = volumecontext.EphemeralClaim
		}
		return q.claimBinding(ctx, pod, v, b, name)
	}
	return b, nil
}

func (q *volumeBindingQuery) claimBinding(ctx context.Context, pod *corev1.Pod, v corev1.Volume, b volumecontext.Binding, name string) (volumecontext.Binding, error) {
	if !validHealthName(name) {
		return b, invalidHealth()
	}
	b.ClaimAvailability = volumehealth.Unavailable
	var pvc corev1.PersistentVolumeClaim
	err := q.authorisedGet(ctx, VolumeAccess{Resource: "persistentvolumeclaims", Namespace: pod.Namespace, Name: name}, "/api/v1/namespaces/"+pod.Namespace+"/persistentvolumeclaims/"+name, &pvc)
	if err != nil {
		availability, _ := healthFailure(err)
		b.ClaimAvailability = availability
		return b, fatalHealthError(ctx, err)
	}
	if pvc.Kind != "PersistentVolumeClaim" || pvc.APIVersion != "v1" || pvc.Name != name || pvc.Namespace != pod.Namespace || pvc.UID == "" || len(pvc.UID) > volumecontext.MaxUIDBytes || pvc.CreationTimestamp.IsZero() {
		return b, invalidHealth()
	}
	if v.Ephemeral != nil && !ephemeralClaimOwnedBy(&pvc, pod) {
		return b, nil
	}
	if pvc.Spec.VolumeName == "" {
		b.ClaimAvailability = volumehealth.Unreported
		return b, nil
	}
	if !validHealthName(pvc.Spec.VolumeName) {
		return b, invalidHealth()
	}
	var pv corev1.PersistentVolume
	// The collector verifies claim ownership privately. Namespace PVC viewers
	// do not need PV disclosure permission merely to see their own usage.
	if err := q.get(ctx, "/api/v1/persistentvolumes/"+pvc.Spec.VolumeName, &pv); err != nil {
		return b, fatalHealthError(ctx, err)
	}
	claim := pv.Spec.ClaimRef
	if pv.Kind != "PersistentVolume" || pv.APIVersion != "v1" || pv.Name != pvc.Spec.VolumeName || pv.Namespace != "" || pv.UID == "" || len(pv.UID) > volumecontext.MaxUIDBytes {
		return b, invalidHealth()
	}
	if claim == nil || claim.Namespace != pvc.Namespace || claim.Name != pvc.Name || claim.UID != pvc.UID {
		return b, nil
	}
	b.ClaimAvailability = volumehealth.Reported
	b.PVCName = pvc.Name
	b.PVCUID = string(pvc.UID)
	b.PVCCreatedAt = pvc.CreationTimestamp.Time
	var pvAccess error
	if pv.Spec.CSI != nil || q.healthSeeds != nil {
		pvAccess = q.authorize(ctx, VolumeAccess{Resource: "persistentvolumes", Name: pv.Name})
		if ctx.Err() != nil {
			return b, &HealthReadError{Reason: volumehealth.ReadFailed, cause: ctx.Err()}
		}
	}
	if pv.Spec.CSI != nil {
		if !validHealthName(pv.Spec.CSI.Driver) {
			return b, invalidHealth()
		}
		if pvAccess == nil {
			b.Driver = pv.Spec.CSI.Driver
		}
	}
	if q.healthSeeds != nil {
		q.healthSeeds[v.Name] = claimHealth(pvc, pv, pvAccess)
	}
	// Do not retain upstream objects, backend handles, paths or driver text.
	if b.PVCCreatedAt.After(time.Now().UTC().Add(volumecontext.FutureSkew)) {
		return b, invalidHealth()
	}
	return b, nil
}
