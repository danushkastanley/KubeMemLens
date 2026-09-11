package agentless

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type ownerTask struct {
	namespace string
	reference metav1.OwnerReference
	pods      []int
}

func (r *Reader) enrichOwners(ctx context.Context, inventory []corev1.Pod, pods []observation.Pod) {
	tasks := map[string]*ownerTask{}
	for i, pod := range inventory {
		pods[i].OwnerEvidence = pods[i].StatusEvidence
		owner := kube.PodOwnerReference(pod)
		if owner == nil {
			continue
		}
		if len(validation.IsDNS1123Subdomain(owner.Name)) != 0 || len(owner.Kind) > 64 || len(owner.UID) > 128 {
			pods[i].OwnerAvailability, pods[i].OwnerReason = capability.Unavailable, capability.InvalidResponse
			pods[i].OwnerEvidence.Completeness = capability.Partial
			continue
		}
		if owner.Kind != "ReplicaSet" && owner.Kind != "Job" {
			continue
		}
		pods[i].OwnerAvailability, pods[i].OwnerReason = capability.Unreported, capability.NotObserved
		key := strings.Join([]string{pod.Namespace, owner.Kind, owner.Name, string(owner.UID)}, "\x00")
		if tasks[key] == nil {
			tasks[key] = &ownerTask{namespace: pod.Namespace, reference: *owner}
		}
		tasks[key].pods = append(tasks[key].pods, i)
	}
	keys := make([]string, 0, len(tasks))
	for key := range tasks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// A fresh resolver per refresh prevents cached owner permissions or identities
	// from crossing refreshes. Its own cache still deduplicates this batch.
	resolver := kube.NewWorkloadOwnerResolver(r.client, time.Minute)
	for index, key := range keys {
		task := tasks[key]
		var kind, name string
		var err error
		switch {
		case index >= r.opts.MaxOwnerReads:
			err = queryFailure(limitReached, nil)
		case ctx.Err() != nil:
			err = readFailure(ctx.Err())
		default:
			kind, name, err = resolver.ResolveReference(ctx, task.namespace, task.reference, r.opts.Now())
		}
		availability, reason := capability.Available, capability.Reason("")
		if err != nil {
			availability, reason = fieldFailure(err)
		}
		if err == nil && task.reference.UID == "" {
			reason = observation.IdentityUnconfirmed
		}
		for _, i := range task.pods {
			pods[i].OwnerAvailability, pods[i].OwnerReason = availability, reason
			pods[i].OwnerEvidence = statusEnvelope(r.opts.Now().UTC(), capability.WorkloadScope)
			pods[i].OwnerEvidence.APIVersion = "apps/v1"
			if task.reference.Kind == "Job" {
				pods[i].OwnerEvidence.APIVersion = "batch/v1"
			}
			if reason != "" {
				pods[i].OwnerEvidence.Completeness = capability.Partial
			}
			if err != nil {
				pods[i].OwnerEvidence.Freshness = capability.UnknownFreshness
			}
			if err == nil {
				pods[i].Context.WorkloadKind, pods[i].Context.WorkloadName = safeText(kind, 64), safeText(name, 253)
				for c := range pods[i].Containers {
					pods[i].Containers[c].Context.WorkloadKind = pods[i].Context.WorkloadKind
					pods[i].Containers[c].Context.WorkloadName = pods[i].Context.WorkloadName
				}
			}
		}
	}
}

func fieldFailure(err error) (capability.Availability, capability.Reason) {
	if errors.Is(err, kube.ErrOwnerReplaced) {
		return capability.Unavailable, observation.IdentityMismatch
	}
	if errors.Is(err, kube.ErrOwnerAPI) {
		return capability.Unsupported, capability.UnsupportedAPI
	}
	var failure *capability.SelectionError
	if !errors.As(err, &failure) {
		errors.As(readFailure(err), &failure)
	}
	switch failure.Reason {
	case capability.AccessDenied:
		return capability.Forbidden, failure.Reason
	case capability.SourceAbsent:
		return capability.Unreported, capability.NotObserved
	default:
		return capability.Unavailable, failure.Reason
	}
}
