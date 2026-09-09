package kube

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func (r *volumeHealthReader) Query(ctx context.Context, podName string) (volumehealth.Report, error) {
	if !validHealthName(podName) {
		return volumehealth.Report{}, invalidHealth()
	}
	ctx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancel()
	q := healthQuery{reader: r, remaining: maxHealthQueryBytes}
	var pod corev1.Pod
	err := q.get(ctx, "/api/v1/namespaces/"+r.opts.Namespace+"/pods/"+podName, &pod)
	if err != nil {
		return volumehealth.Report{}, err
	}
	if pod.Kind != "Pod" || pod.APIVersion != "v1" || pod.Namespace != r.opts.Namespace || pod.Name != podName || pod.UID == "" || len(pod.UID) > 128 || len(pod.Spec.Volumes) > maxHealthVolumes || len(pod.Status.VolumeHealth) > maxHealthVolumes {
		return volumehealth.Report{}, invalidHealth()
	}
	if r.opts.PodCache != nil {
		pod = r.opts.PodCache.volumeHealthPod(pod)
	}
	observations := make([]volumehealth.Observation, 0, len(pod.Spec.Volumes)*3)
	seen := map[string]bool{}
	for _, v := range pod.Spec.Volumes {
		if !validHealthName(v.Name) || seen[v.Name] {
			return volumehealth.Report{}, invalidHealth()
		}
		seen[v.Name] = true
		if v.CSI == nil && v.PersistentVolumeClaim == nil && v.Ephemeral == nil {
			continue
		}
		rows, err := q.volume(ctx, &pod, v)
		if err != nil {
			return volumehealth.Report{}, err
		}
		observations = append(observations, rows...)
	}
	for _, h := range pod.Status.VolumeHealth {
		if !seen[h.Name] {
			return volumehealth.Report{}, invalidHealth()
		}
	}
	now := time.Now().UTC()
	for i := range observations {
		observations[i] = volumehealth.Evaluate(observations[i], now)
	}
	return volumehealth.NewReport(observations), nil
}

func validHealthName(name string) bool { return len(validation.IsDNS1123Subdomain(name)) == 0 }

func (q *healthQuery) volume(ctx context.Context, pod *corev1.Pod, v corev1.Volume) ([]volumehealth.Observation, error) {
	id := volumehealth.Identity{Namespace: pod.Namespace, PodName: pod.Name, PodUID: string(pod.UID), VolumeName: v.Name, NodeName: pod.Spec.NodeName}
	podHealth, err := podVolumeHealth(pod, id)
	if err != nil {
		return nil, err
	}
	rows := []volumehealth.Observation{podHealth}
	if v.CSI != nil {
		id.Driver = v.CSI.Driver
	} else {
		controller, driver, err := q.claim(ctx, pod, v, id)
		if err != nil {
			return nil, err
		}
		rows = append(rows, controller)
		id = controller.Identity
		id.Driver = driver.name
		if driver.name == "" {
			backend := newHealthObservation(id, volumehealth.BackendSource)
			backend.Availability, backend.Reason = driver.availability, driver.reason
			return append(rows, backend), nil
		}
	}
	backend, err := q.backend(ctx, id)
	if err != nil {
		return nil, err
	}
	return append(rows, backend), nil
}

func newHealthObservation(id volumehealth.Identity, source volumehealth.Source) volumehealth.Observation {
	scope := volumehealth.VolumeScope
	if source == volumehealth.BackendSource {
		scope = volumehealth.BackendScope
	}
	return volumehealth.Observation{Identity: id, Source: source, Scope: scope, Availability: volumehealth.Unreported, Reason: volumehealth.NoReport, ObservedAt: time.Now().UTC()}
}

func podVolumeHealth(pod *corev1.Pod, id volumehealth.Identity) (volumehealth.Observation, error) {
	o := newHealthObservation(id, volumehealth.PodSource)
	for _, h := range pod.Status.VolumeHealth {
		if h.Name != id.VolumeName {
			continue
		}
		if o.Availability == volumehealth.Reported {
			return o, invalidHealth()
		}
		conditions, err := volumeConditions(h.HealthConditions)
		if err != nil {
			return o, err
		}
		o.Availability, o.Reason = volumehealth.Reported, ""
		o.TransitionAt, o.Conditions = h.LastTransitionTime.Time, conditions
	}
	return o, nil
}

func volumeConditions(input []corev1.VolumeHealthCondition) ([]volumehealth.Condition, error) {
	if len(input) > volumehealth.MaxConditions {
		return nil, invalidHealth()
	}
	conditions := make([]volumehealth.Condition, 0, len(input))
	for _, c := range input {
		if len(c.Status) == 0 || len(c.Status) > 256 {
			return nil, invalidHealth()
		}
		conditions = append(conditions, volumehealth.Condition{Status: volumehealth.Status(c.Status), Reason: c.Reason, Message: c.Message})
	}
	return conditions, nil
}
