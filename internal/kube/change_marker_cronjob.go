package kube

import (
	"context"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	corev1 "k8s.io/api/core/v1"
)

func (q *markerQuery) cronJobPods(ctx context.Context, root workloadObject) ([]corev1.Pod, error) {
	jobs, err := q.cronJobChildren(ctx, root)
	if err != nil {
		return nil, err
	}
	q.jobs = make(map[string]workloadObject, len(jobs))
	for _, job := range jobs {
		if job.Kind == "" {
			job.Kind = "Job"
		}
		if job.APIVersion == "" {
			job.APIVersion = "batch/v1"
		}
		if job.Kind != "Job" || job.APIVersion != "batch/v1" {
			return nil, memoryhistory.ErrInvalid
		}
		q.jobs[job.Name] = job
	}
	// Different Job selectors form one bounded namespace-list operation. Check
	// its permission once in this phase, not once per identical request. Named
	// Pod and Job permissions remain independent, and Revalidate starts afresh.
	list := ObjectAccess{Resource: "pods", Namespace: root.Namespace, Verb: "list"}
	if err := q.authorize(ctx, list); err != nil {
		return nil, err
	}
	authorize := q.authorize
	q.authorize = func(ctx context.Context, access ObjectAccess) error {
		if access == list {
			return ctx.Err()
		}
		return authorize(ctx, access)
	}
	defer func() { q.authorize = authorize }()
	resource, _ := volumeWorkloadResource("Job")
	var pods []corev1.Pod
	names, identities := map[string]bool{}, map[string]bool{}
	for _, job := range jobs {
		members, err := q.workloadPods(ctx, job, resource)
		if err != nil {
			return nil, err
		}
		for _, pod := range members {
			if names[pod.Name] || identities[string(pod.UID)] {
				return nil, memoryhistory.ErrInvalid
			}
			names[pod.Name] = true
			identities[string(pod.UID)] = true
			pods = append(pods, pod)
			if len(pods) > volumecontext.MaxWorkloadPods {
				return nil, memoryhistory.ErrBounds
			}
		}
	}
	orderWorkloadPods(pods)
	return pods, nil
}
