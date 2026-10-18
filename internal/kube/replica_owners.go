package kube

import (
	"context"
	"errors"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	corev1 "k8s.io/api/core/v1"
)

func replicaRoot(ctx context.Context, q *markerQuery, request memoryhistory.Request) (changemarkers.Object, changemarkers.Object, error) {
	var object markerObject
	var err error
	if request.Scope == memoryhistory.Pod {
		if err = q.authorize(ctx, ObjectAccess{Group: api.MemoryAPIGroup, Resource: "pods", Namespace: request.Namespace, Name: request.Name}); err != nil {
			return object.ref, object.ref, err
		}
		var pod corev1.Pod
		err = q.authorisedGet(ctx, ObjectAccess{Resource: "pods", Namespace: request.Namespace, Name: request.Name}, "/api/v1/namespaces/"+request.Namespace+"/pods/"+request.Name, &pod)
		if err == nil {
			var member replicabaseline.Member
			member, err = replicaPod(pod)
			if err == nil && (pod.Namespace != request.Namespace || pod.Name != request.Name) {
				err = memoryhistory.ErrChanged
			}
			if err == nil {
				object, err = replicaPodObject(pod, member.Object)
			}
		}
	} else {
		resource, supported := volumeWorkloadResource(request.WorkloadKind)
		if !supported || resource.kind != request.WorkloadKind {
			return object.ref, object.ref, memoryhistory.ErrInvalid
		}
		var root workloadObject
		root, err = q.workloadObject(ctx, resource, request.Namespace, request.Name)
		if err == nil {
			ref := changemarkers.Object{APIVersion: resource.version, Kind: resource.kind, Namespace: root.Namespace, Name: root.Name, UID: string(root.UID)}
			object, err = q.remember(ref, markerObject{ref: ref}, root.ObjectMeta)
		}
	}
	if err != nil {
		return object.ref, object.ref, err
	}
	owners, err := q.chain(ctx, object)
	if err != nil {
		return object.ref, object.ref, err
	}
	if len(owners) > 0 {
		return object.ref, owners[len(owners)-1], nil
	}
	if request.Scope == memoryhistory.Pod {
		return object.ref, object.ref, changemarkers.ErrUnsupported
	}
	return object.ref, object.ref, nil
}

func replicaPodObject(pod corev1.Pod, ref changemarkers.Object) (markerObject, error) {
	count := 0
	for _, owner := range pod.OwnerReferences {
		if owner.Controller != nil && *owner.Controller {
			count++
		}
	}
	if count > 1 {
		return markerObject{}, replicabaseline.ErrInvalid
	}
	return markerObject{ref: ref, controller: controllerReference(pod.OwnerReferences), created: pod.CreationTimestamp.Time}, nil
}

func replicaRevision(ctx context.Context, q *markerQuery, root changemarkers.Object, pod corev1.Pod, owners []changemarkers.Object, cache map[string]workloadObject) (string, error) {
	switch root.Kind {
	case "Deployment", "CronJob":
		kind := "ReplicaSet"
		if root.Kind == "CronJob" {
			kind = "Job"
		}
		for _, owner := range owners {
			if owner.Kind == kind {
				return owner.UID, nil
			}
		}
		return "", memoryhistory.ErrChanged
	case "Job":
		return root.UID, nil
	case "StatefulSet", "DaemonSet":
		name := pod.Labels["controller-revision-hash"]
		if name == "" {
			return "", nil
		}
		// DaemonSet Pods carry the template hash; StatefulSet Pods carry
		// the complete ControllerRevision name.
		if root.Kind == "DaemonSet" {
			name = root.Name + "-" + name
		}
		if !validHealthName(name) {
			return "", replicabaseline.ErrInvalid
		}
		revision, found := cache[name]
		if !found {
			resource := workloadResource{kind: "ControllerRevision", group: "apps", version: "apps/v1", resource: "controllerrevisions"}
			var err error
			revision, err = q.workloadObject(ctx, resource, root.Namespace, name)
			if err != nil {
				return "", err
			}
			cache[name] = revision
		}
		if revision.DeletionTimestamp != nil || revision.CreationTimestamp.IsZero() || !sameMarkerOwner(controllerReference(revision.OwnerReferences), root) {
			return "", memoryhistory.ErrChanged
		}
		return string(revision.UID), nil
	default:
		// UID/generation alone cannot establish one unchanged Pod template for
		// standalone ReplicaSets or ReplicationControllers.
		return "", nil
	}
}

func replicaSourceError(err error) error {
	for _, known := range []error{replicabaseline.ErrInvalid, replicabaseline.ErrScope, replicabaseline.ErrBounds} {
		if errors.Is(err, known) {
			return known
		}
	}
	return historyReadError(err)
}
