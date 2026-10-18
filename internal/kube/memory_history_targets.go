package kube

import (
	"context"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func (q *volumeBindingQuery) historyPod(ctx context.Context, namespace, name string) (corev1.Pod, error) {
	var pod corev1.Pod
	if err := q.authorize(ctx, ObjectAccess{Group: api.MemoryAPIGroup, Resource: "pods", Namespace: namespace, Name: name}); err != nil {
		return pod, err
	}
	if err := q.authorisedGet(ctx, ObjectAccess{Resource: "pods", Namespace: namespace, Name: name}, "/api/v1/namespaces/"+namespace+"/pods/"+name, &pod); err != nil {
		return pod, err
	}
	if pod.Kind != "Pod" || pod.APIVersion != "v1" || pod.Namespace != namespace || pod.Name != name || pod.UID == "" || pod.CreationTimestamp.IsZero() || pod.DeletionTimestamp != nil {
		return corev1.Pod{}, memoryhistory.ErrChanged
	}
	return pod, nil
}

func (q *volumeBindingQuery) historyTargets(ctx context.Context, s *memoryhistory.Selection, pod corev1.Pod, nodeIdentity VolumeNodeIdentity) error {
	if pod.Spec.NodeName == "" {
		return memoryhistory.ErrUnavailable
	}
	if len(validation.IsDNS1123Subdomain(pod.Spec.NodeName)) != 0 {
		return memoryhistory.ErrInvalid
	}
	// Reuse the collector's authenticated Node coverage identity. Pod readers
	// do not require an additional cluster-wide Node acquisition permission.
	uid, known := nodeIdentity(pod.Spec.NodeName, time.Now().UTC())
	if !known || uid == "" {
		return memoryhistory.ErrUnavailable
	}
	before := len(s.Targets)
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for _, status := range statuses {
			if status.State.Running == nil || (s.Request.Scope == memoryhistory.Container && status.Name != s.Request.Container) {
				continue
			}
			if status.State.Running.StartedAt.IsZero() || status.State.Running.StartedAt.Before(&pod.CreationTimestamp) {
				return memoryhistory.ErrChanged
			}
			if len(s.Targets) >= memoryhistory.MaxTargets {
				return memoryhistory.ErrBounds
			}
			s.Targets = append(s.Targets, memoryhistory.Target{Namespace: pod.Namespace, Pod: pod.Name, PodUID: string(pod.UID), Container: status.Name, ContainerID: status.ContainerID, Node: pod.Spec.NodeName, NodeUID: uid, StartedAt: status.State.Running.StartedAt.Time, PodCreatedAt: pod.CreationTimestamp.Time})
		}
	}
	if len(s.Targets) == before {
		return memoryhistory.ErrUnavailable
	}
	return nil
}

func (q *volumeBindingQuery) historyNode(ctx context.Context, s *memoryhistory.Selection) error {
	name := s.Request.Name
	var node corev1.Node
	if err := q.authorisedGet(ctx, ObjectAccess{Resource: "nodes", Name: name}, "/api/v1/nodes/"+name, &node); err != nil {
		return err
	}
	if node.Kind != "Node" || node.APIVersion != "v1" || node.Name != name || node.UID == "" || node.DeletionTimestamp != nil {
		return memoryhistory.ErrChanged
	}
	s.UID = string(node.UID)
	s.Targets = []memoryhistory.Target{{Node: node.Name, NodeUID: s.UID, StartedAt: node.CreationTimestamp.Time}}
	return nil
}

func (q *volumeBindingQuery) historyWorkload(ctx context.Context, s *memoryhistory.Selection, nodeIdentity VolumeNodeIdentity) error {
	resource, ok := volumeWorkloadResource(s.Request.WorkloadKind)
	if !ok || s.Request.WorkloadKind != resource.kind {
		return memoryhistory.ErrInvalid
	}
	root, err := q.workloadObject(ctx, resource, s.Request.Namespace, s.Request.Name)
	if err != nil {
		return err
	}
	pods, err := q.workloadPods(ctx, root, resource)
	if err != nil {
		return err
	}
	s.UID, s.Revision = string(root.UID), strconv.FormatInt(root.Generation, 10)
	for _, listed := range pods {
		member, err := q.workloadMember(ctx, root, listed.OwnerReferences)
		if err != nil {
			return err
		}
		if !member {
			continue
		}
		pod, err := q.historyPod(ctx, s.Request.Namespace, listed.Name)
		if err != nil {
			return err
		}
		if pod.UID != listed.UID {
			return memoryhistory.ErrChanged
		}
		member, err = q.workloadMember(ctx, root, pod.OwnerReferences)
		if err != nil {
			return err
		}
		if !member {
			return memoryhistory.ErrChanged
		}
		if err := q.historyTargets(ctx, s, pod, nodeIdentity); err != nil {
			return err
		}
	}
	if err := q.recheckWorkloadParents(ctx); err != nil {
		return err
	}
	current, err := q.workloadObject(ctx, resource, s.Request.Namespace, s.Request.Name)
	if err != nil {
		return err
	}
	if current.UID != root.UID || current.Generation != root.Generation {
		return memoryhistory.ErrChanged
	}
	s.ResolvedAt = time.Now().UTC()
	return nil
}
