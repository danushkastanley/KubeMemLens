package kube

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

var ErrVolumeWorkloadBounds = errors.New("workload volume query exceeds bounded coverage; inspect individual Pods")
var ErrVolumeWorkloadNotFound = errors.New("selected workload was not found")

type ResolvedWorkloadVolumes struct {
	Namespace, Kind, Name, UID string
	ObservedAt                 time.Time
	Pods                       []ResolvedVolumes
	Unscheduled                []volumecontext.PodScope
}

type WorkloadVolumeResolver interface {
	ResolveWorkload(context.Context, string, string, string) (ResolvedWorkloadVolumes, error)
}

func (r *volumeResolver) ResolveWorkload(ctx context.Context, namespace, kind, name string) (ResolvedWorkloadVolumes, error) {
	target, ok := volumeWorkloadResource(kind)
	if !ok || len(validation.IsDNS1123Label(namespace)) != 0 || !validHealthName(name) {
		return ResolvedWorkloadVolumes{}, invalidHealth()
	}
	select {
	case r.workloads <- struct{}{}:
		defer func() { <-r.workloads }()
	default:
		return ResolvedWorkloadVolumes{}, &HealthReadError{Reason: volumehealth.ReadFailed}
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	q := r.bindingQuery()
	q.workloadParents = map[string]workloadObject{}
	root, err := q.workloadObject(ctx, target, namespace, name)
	if errors.Is(err, errHealthObjectNotFound) {
		return ResolvedWorkloadVolumes{}, ErrVolumeWorkloadNotFound
	}
	if err != nil {
		return ResolvedWorkloadVolumes{}, err
	}
	pods, err := q.workloadPods(ctx, root, target)
	if err != nil {
		return ResolvedWorkloadVolumes{}, err
	}
	// Select only controller-owned Pods. The selector narrows acquisition but
	// never establishes ownership; reused controller names require matching UIDs.
	selected := make([]corev1.Pod, 0, len(pods))
	var unscheduled []volumecontext.PodScope
	for _, pod := range pods {
		member, err := q.workloadMember(ctx, root, pod.OwnerReferences)
		if err != nil {
			return ResolvedWorkloadVolumes{}, err
		}
		if member {
			if pod.Spec.NodeName == "" {
				unscheduled = append(unscheduled, volumecontext.PodScope{Namespace: namespace, PodName: pod.Name, PodUID: string(pod.UID), CreatedAt: pod.CreationTimestamp.Time})
				continue
			}
			selected = append(selected, pod)
		}
	}
	resolved, err := r.resolveWorkloadPods(ctx, namespace, selected)
	if err != nil {
		return ResolvedWorkloadVolumes{}, err
	}
	count := 0
	for i, pod := range resolved {
		if pod.Scope.PodUID != string(selected[i].UID) {
			return ResolvedWorkloadVolumes{}, invalidHealth()
		}
		member, err := q.workloadMember(ctx, root, pod.OwnerReferences)
		if err != nil {
			return ResolvedWorkloadVolumes{}, err
		}
		if !member {
			return ResolvedWorkloadVolumes{}, &HealthReadError{Reason: volumehealth.BindingUnavailable}
		}
		count += len(pod.Bindings)
		if count > volumecontext.MaxPageRecords {
			return ResolvedWorkloadVolumes{}, ErrVolumeWorkloadBounds
		}
	}
	if err := q.recheckWorkloadParents(ctx); err != nil {
		return ResolvedWorkloadVolumes{}, err
	}
	current, err := q.workloadObject(ctx, target, namespace, name)
	if err != nil {
		return ResolvedWorkloadVolumes{}, err
	}
	if current.UID != root.UID || ctx.Err() != nil {
		return ResolvedWorkloadVolumes{}, &HealthReadError{Reason: volumehealth.BindingUnavailable}
	}
	return ResolvedWorkloadVolumes{Namespace: namespace, Kind: target.kind, Name: name, UID: string(root.UID), ObservedAt: time.Now().UTC(), Pods: resolved, Unscheduled: unscheduled}, nil
}

func (r *volumeResolver) resolveWorkloadPods(ctx context.Context, namespace string, pods []corev1.Pod) ([]ResolvedVolumes, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]ResolvedVolumes, len(pods))
	jobs := make(chan int, len(pods))
	for i := range pods {
		jobs <- i
	}
	close(jobs)
	var wait sync.WaitGroup
	var once sync.Once
	var failure error
	for worker := 0; worker < min(4, len(pods)); worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				value, err := r.Resolve(ctx, namespace, pods[i].Name)
				if err != nil {
					once.Do(func() { failure = err; cancel() })
					continue
				}
				results[i] = value
			}
		}()
	}
	wait.Wait()
	if failure != nil {
		return nil, failure
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return results, nil
}

func orderWorkloadPods(pods []corev1.Pod) {
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
}
