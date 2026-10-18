package kube

import (
	"context"
	"slices"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

func replicaChangeContext(ctx context.Context, q *markerQuery, result *replicabaseline.Selection, now time.Time) error {
	events, state, truncated, err := q.eventsForNamespace(ctx, result.Workload.Namespace)
	if err != nil {
		return err
	}
	result.EventState, result.EventsTruncated = state, truncated
	for _, event := range events {
		if event.Until.After(now) {
			return replicabaseline.ErrInvalid
		}
		for i := range result.Members {
			member := &result.Members[i]
			if event.Subject != member.Object && !slices.Contains(member.Owners, event.Subject) {
				continue
			}
			if event.Until.After(member.StableSince) {
				member.StableSince = event.Until
			}
			if now.Sub(event.Until) < replicabaseline.StabilityWindow {
				member.Changes = replicabaseline.RecentChange
			}
		}
	}
	// Even a non-empty retained Event page is partial history. Do not upgrade
	// an unknown change history to a proof of stability merely because it is quiet.
	return nil
}

type replicaWorkloadDetails struct {
	workloadObject
	Status *struct {
		ObservedGeneration int64 `json:"observedGeneration"`
	} `json:"status"`
}

func replicaRootDetails(ctx context.Context, q *markerQuery, root changemarkers.Object) (replicaWorkloadDetails, error) {
	resource, _ := volumeWorkloadResource(root.Kind)
	var value replicaWorkloadDetails
	if err := q.authorisedGet(ctx, ObjectAccess{Group: resource.group, Resource: resource.resource, Namespace: root.Namespace, Name: root.Name}, resource.path(root.Namespace)+"/"+root.Name, &value); err != nil {
		return value, err
	}
	if value.Kind != root.Kind || value.APIVersion != root.APIVersion || value.Namespace != root.Namespace || value.Name != root.Name || string(value.UID) != root.UID || value.Generation < 0 || value.DeletionTimestamp != nil {
		return value, replicabaseline.ErrInvalid
	}
	return value, nil
}

func rootChangePending(root replicaWorkloadDetails) bool {
	switch root.Kind {
	case "Deployment", "StatefulSet", "DaemonSet":
		return root.Status != nil && root.Status.ObservedGeneration < root.Generation
	default:
		return false
	}
}
