package kube

import (
	"context"
	"slices"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

// Context acquisition and disclosure each call Resolve with fresh snapshots.
// Reuse the bounded membership evidence within one phase, checking each named
// Pod and owner permission, rather than reading every listed object twice.
func (q *volumeBindingQuery) historyContextWorkload(ctx context.Context, s *memoryhistory.Selection, nodeIdentity VolumeNodeIdentity) error {
	resource, ok := volumeWorkloadResource(s.Request.WorkloadKind)
	if !ok || resource.kind != s.Request.WorkloadKind {
		return memoryhistory.ErrInvalid
	}
	root, err := q.workloadObject(ctx, resource, s.Request.Namespace, s.Request.Name)
	if err != nil {
		return err
	}
	members := markerQuery{volumeBindingQuery: *q, objects: map[string]markerObject{}}
	if err := members.preparePods(ctx, s.Request, string(root.UID)); err != nil {
		return err
	}
	ref := changemarkers.Object{APIVersion: resource.version, Kind: resource.kind, Namespace: root.Namespace, Name: root.Name, UID: string(root.UID)}
	s.UID, s.Revision = string(root.UID), strconv.FormatInt(root.Generation, 10)
	names := make([]string, 0, len(members.pods))
	for name := range members.pods {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		pod := members.pods[name]
		object, err := members.object(ctx, changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name, UID: string(pod.UID)})
		if err != nil {
			return err
		}
		owners, err := members.chain(ctx, object)
		if err != nil {
			return err
		}
		if !slices.Contains(owners, ref) {
			continue
		}
		if err := q.historyTargets(ctx, s, pod, nodeIdentity); err != nil {
			return err
		}
	}
	return nil
}
