package kube

import (
	"context"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	corev1 "k8s.io/api/core/v1"
)

// Workload reads already require bounded, caller-authorised Pod membership reads.
// Reuse their current snapshots, retaining every named permission check.
// Query and Revalidate get separate snapshots; no evidence crosses requests.
func (q *markerQuery) preparePods(ctx context.Context, request memoryhistory.Request, uid string) error {
	if request.Scope != memoryhistory.Workload {
		return nil
	}
	resource, ok := volumeWorkloadResource(request.WorkloadKind)
	if !ok || resource.kind != request.WorkloadKind {
		return memoryhistory.ErrInvalid
	}
	root, err := q.workloadObject(ctx, resource, request.Namespace, request.Name)
	if err != nil {
		return err
	}
	ref := changemarkers.Object{APIVersion: resource.version, Kind: resource.kind, Namespace: request.Namespace, Name: request.Name, UID: uid}
	if _, err := q.remember(ref, markerObject{ref: ref}, root.ObjectMeta); err != nil {
		return err
	}
	var pods []corev1.Pod
	if resource.kind == "CronJob" {
		pods, err = q.cronJobPods(ctx, root)
	} else {
		pods, err = q.workloadPods(ctx, root, resource)
	}
	if err != nil {
		return err
	}
	q.pods = make(map[string]corev1.Pod, len(pods))
	for _, pod := range pods {
		// The verified PodList/v1 envelope fixes the item type when its redundant
		// per-item TypeMeta is omitted by the Kubernetes list representation.
		if pod.Kind == "" {
			pod.Kind = "Pod"
		}
		if pod.APIVersion == "" {
			pod.APIVersion = "v1"
		}
		if pod.Kind != "Pod" || pod.APIVersion != "v1" {
			return memoryhistory.ErrInvalid
		}
		q.pods[pod.Name] = pod
	}
	return nil
}

func (q *markerQuery) workload(ctx context.Context, resource workloadResource, ref changemarkers.Object) (workloadObject, error) {
	if q.jobs == nil || ref.Kind != "Job" {
		return q.workloadObject(ctx, resource, ref.Namespace, ref.Name)
	}
	if err := q.authorize(ctx, ObjectAccess{Group: "batch", Resource: "jobs", Namespace: ref.Namespace, Name: ref.Name}); err != nil {
		return workloadObject{}, err
	}
	object, exists := q.jobs[ref.Name]
	if !exists || string(object.UID) != ref.UID || object.Namespace != ref.Namespace {
		return workloadObject{}, memoryhistory.ErrChanged
	}
	return object, nil
}

func (q *markerQuery) pod(ctx context.Context, ref changemarkers.Object) (corev1.Pod, error) {
	if q.pods == nil {
		return q.historyPod(ctx, ref.Namespace, ref.Name)
	}
	for _, group := range []string{api.MemoryAPIGroup, ""} {
		if err := q.authorize(ctx, ObjectAccess{Group: group, Resource: "pods", Namespace: ref.Namespace, Name: ref.Name}); err != nil {
			return corev1.Pod{}, err
		}
	}
	pod, exists := q.pods[ref.Name]
	if !exists || pod.Namespace != ref.Namespace || string(pod.UID) != ref.UID {
		return corev1.Pod{}, memoryhistory.ErrChanged
	}
	return pod, nil
}
