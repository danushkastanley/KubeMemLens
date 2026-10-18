package kube

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type markerLifetime struct {
	id      string
	started time.Time
}
type markerObject struct {
	ref        changemarkers.Object
	controller *metav1.OwnerReference
	created    time.Time
	revision   int64
	markers    []changemarkers.Marker
	containers map[string]markerLifetime
}
type markerQuery struct {
	volumeBindingQuery
	objects map[string]markerObject
	visible map[string][]changemarkers.Object
	pods    map[string]corev1.Pod
	jobs    map[string]workloadObject
}

func objectKey(o changemarkers.Object) string {
	return o.APIVersion + "/" + o.Namespace + "/" + o.Kind + "/" + o.Name + "/" + o.UID
}

func (q *markerQuery) object(ctx context.Context, ref changemarkers.Object) (markerObject, error) {
	if ref.Validate() != nil {
		return markerObject{}, memoryhistory.ErrInvalid
	}
	if value, ok := q.objects[objectKey(ref)]; ok {
		return value, nil
	}
	if len(q.objects) >= memoryhistory.MaxTargets*(changemarkers.MaxOwners+1)+1 {
		return markerObject{}, memoryhistory.ErrBounds
	}
	result := markerObject{ref: ref}
	var meta metav1.ObjectMeta
	if ref.Kind == "Pod" {
		pod, err := q.pod(ctx, ref)
		if err != nil {
			return result, err
		}
		meta = pod.ObjectMeta
		result.containers = map[string]markerLifetime{}
		for _, group := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
			for _, status := range group {
				if status.State.Running == nil {
					continue
				}
				if _, exists := result.containers[status.Name]; exists {
					return result, memoryhistory.ErrInvalid
				}
				at := status.State.Running.StartedAt.Time.UTC()
				result.containers[status.Name] = markerLifetime{status.ContainerID, at}
				if status.RestartCount > 0 {
					m := sourceMarker(ref, changemarkers.Restarted, changemarkers.ContainerState, at)
					m.Container = status.Name
					result.markers = append(result.markers, m)
				}
			}
		}
		for _, condition := range pod.Status.Conditions {
			if (condition.Type == corev1.PodResizePending || condition.Type == corev1.PodResizeInProgress) && condition.Status == corev1.ConditionTrue && !condition.LastTransitionTime.IsZero() {
				m := sourceMarker(ref, changemarkers.Resize, changemarkers.PodCondition, condition.LastTransitionTime.Time.UTC())
				m.ResizeState = "pending"
				if condition.Type == corev1.PodResizeInProgress {
					m.ResizeState = "in-progress"
				}
				result.markers = append(result.markers, m)
			}
		}
	} else {
		resource, _ := volumeWorkloadResource(ref.Kind)
		object, err := q.workload(ctx, resource, ref)
		if err != nil {
			return result, err
		}
		meta = object.ObjectMeta
	}
	return q.remember(ref, result, meta)
}

func (q *markerQuery) remember(ref changemarkers.Object, result markerObject, meta metav1.ObjectMeta) (markerObject, error) {
	if string(meta.UID) != ref.UID || meta.DeletionTimestamp != nil || meta.CreationTimestamp.IsZero() {
		return result, memoryhistory.ErrChanged
	}
	controllers := 0
	for _, owner := range meta.OwnerReferences {
		if owner.Controller != nil && *owner.Controller {
			controllers++
		}
	}
	if controllers > 1 {
		return result, memoryhistory.ErrInvalid
	}
	result.controller = controllerReference(meta.OwnerReferences)
	result.created = meta.CreationTimestamp.Time.UTC()
	if ref.Kind == "ReplicaSet" || ref.Kind == "Deployment" {
		if text := meta.Annotations["deployment.kubernetes.io/revision"]; text != "" {
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil || n <= 0 {
				return result, memoryhistory.ErrInvalid
			}
			result.revision = n
		}
	}
	q.objects[objectKey(ref)] = result
	return result, nil
}

func (q *markerQuery) chain(ctx context.Context, subject markerObject) ([]changemarkers.Object, error) {
	var result []changemarkers.Object
	seen := map[string]bool{subject.ref.UID: true}
	for subject.controller != nil {
		if len(result) >= changemarkers.MaxOwners {
			return nil, memoryhistory.ErrBounds
		}
		owner := subject.controller
		resource, supported := volumeWorkloadResource(owner.Kind)
		if !supported || resource.kind != owner.Kind || resource.version != owner.APIVersion {
			return nil, changemarkers.ErrUnsupported
		}
		ref := changemarkers.Object{APIVersion: owner.APIVersion, Kind: owner.Kind, Namespace: subject.ref.Namespace, Name: owner.Name, UID: string(owner.UID)}
		if ref.Kind == "Pod" || seen[ref.UID] {
			return nil, memoryhistory.ErrInvalid
		}
		current, err := q.object(ctx, ref)
		if err != nil {
			return nil, err
		}
		seen[ref.UID] = true
		result = append(result, ref)
		subject = current
	}
	return result, nil
}

func (q *markerQuery) selectedMarkers(ctx context.Context, selected memoryhistory.Selection, now func() time.Time) ([]changemarkers.Marker, error) {
	chains := map[string][]changemarkers.Object{}
	for _, target := range selected.Targets {
		ref := changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: target.Namespace, Name: target.Pod, UID: target.PodUID}
		pod, err := q.object(ctx, ref)
		if err != nil {
			return nil, err
		}
		lifetime, ok := pod.containers[target.Container]
		if !ok || lifetime.id != target.ContainerID || !lifetime.started.Equal(target.StartedAt) {
			return nil, memoryhistory.ErrChanged
		}
		owners, err := q.chain(ctx, pod)
		if err != nil {
			return nil, err
		}
		chains[objectKey(ref)] = owners
		for i, owner := range owners {
			chains[objectKey(owner)] = owners[i+1:]
		}
	}
	if selected.Request.Scope == memoryhistory.Workload {
		r, ok := volumeWorkloadResource(selected.Request.WorkloadKind)
		if !ok {
			return nil, memoryhistory.ErrInvalid
		}
		ref := changemarkers.Object{APIVersion: r.version, Kind: r.kind, Namespace: selected.Request.Namespace, Name: selected.Request.Name, UID: selected.UID}
		root, err := q.object(ctx, ref)
		if err != nil {
			return nil, err
		}
		owners, err := q.chain(ctx, root)
		if err != nil {
			return nil, err
		}
		chains[objectKey(ref)] = owners
		for _, target := range selected.Targets {
			chain := chains[objectKey(changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: target.Namespace, Name: target.Pod, UID: target.PodUID})]
			if !slices.Contains(chain, ref) {
				return nil, memoryhistory.ErrChanged
			}
		}
	}
	keys := make([]string, 0, len(q.objects))
	for key := range q.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result []changemarkers.Marker
	q.visible = map[string][]changemarkers.Object{}
	for _, key := range keys {
		object := q.objects[key]
		if selected.Request.Scope != memoryhistory.Workload && object.ref.Kind != "Pod" {
			continue
		}
		owners := chains[key]
		if selected.Request.Scope == memoryhistory.Workload && !markerObjectInWorkload(object.ref, owners, selected) {
			continue
		}
		q.visible[key] = owners
		created := sourceMarker(object.ref, changemarkers.Created, changemarkers.APICreation, object.created)
		created.Owners = owners
		result = append(result, created)
		for _, m := range object.markers {
			if selected.Request.Scope == memoryhistory.Container && m.Container != "" && m.Container != selected.Request.Container {
				continue
			}
			m.Owners = owners
			result = append(result, m)
		}
		source := object
		if object.ref.Kind == "Pod" {
			for _, owner := range owners {
				candidate := q.objects[objectKey(owner)]
				if candidate.revision > 0 {
					source = candidate
					break
				}
			}
		}
		if source.revision > 0 {
			m := sourceMarker(object.ref, changemarkers.Revision, changemarkers.Observation, now().UTC())
			m.SourceUID = source.ref.UID
			m.Revision = source.revision
			m.Uncertain = true
			m.Owners = owners
			result = append(result, m)
		}
	}
	return result, nil
}

func markerObjectInWorkload(subject changemarkers.Object, owners []changemarkers.Object, selected memoryhistory.Selection) bool {
	chain := append([]changemarkers.Object{subject}, owners...)
	return slices.ContainsFunc(chain, func(o changemarkers.Object) bool {
		return o.UID == selected.UID && o.Name == selected.Request.Name && o.Kind == selected.Request.WorkloadKind
	})
}

func sourceMarker(subject changemarkers.Object, kind changemarkers.Kind, clock changemarkers.Clock, at time.Time) changemarkers.Marker {
	return changemarkers.Marker{Kind: kind, Clock: clock, At: at, Until: at, SourceUID: subject.UID, Subject: subject, Count: 1}
}

func sameMarkerOwner(owner *metav1.OwnerReference, ref changemarkers.Object) bool {
	return owner != nil && owner.Controller != nil && *owner.Controller && owner.APIVersion == ref.APIVersion && owner.Kind == ref.Kind && owner.Name == ref.Name && string(owner.UID) == ref.UID
}
