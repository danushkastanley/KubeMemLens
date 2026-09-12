package agentless

import (
	"context"
	"sort"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type podInventory struct {
	pods         []corev1.Pod
	completeness capability.Completeness
	reason       capability.Reason
}

func (r *Reader) readPods(ctx context.Context) (podInventory, error) {
	result := podInventory{pods: []corev1.Pod{}, completeness: capability.Complete}
	seen, cursors := map[string]bool{}, map[string]bool{}
	cursor, version := "", ""
	containers := 0
	for page := 0; page < r.opts.MaxPages; page++ {
		list, err := r.client.CoreV1().Pods(r.namespace).List(ctx, metav1.ListOptions{Limit: int64(r.opts.PageSize), Continue: cursor})
		if err != nil {
			return podInventory{}, readFailure(err)
		}
		if page > 0 && list.ResourceVersion != version {
			return podInventory{}, queryFailure(capability.InvalidResponse, nil)
		}
		version = list.ResourceVersion
		for _, pod := range list.Items {
			if ctx.Err() != nil {
				return podInventory{}, readFailure(ctx.Err())
			}
			key := pod.Namespace + "/" + pod.Name
			if !validPodIdentity(pod, r.namespace) || seen[key] {
				return podInventory{}, queryFailure(capability.InvalidResponse, nil)
			}
			count := len(pod.Spec.Containers) + len(pod.Spec.InitContainers) + len(pod.Spec.EphemeralContainers)
			statuses := len(pod.Status.ContainerStatuses) + len(pod.Status.InitContainerStatuses) + len(pod.Status.EphemeralContainerStatuses)
			if len(result.pods) >= r.opts.MaxPods || count > r.opts.MaxContainers-containers || statuses > r.opts.MaxContainers {
				return limitedPods(result), nil
			}
			if !validContainerNames(pod) {
				return podInventory{}, queryFailure(capability.InvalidResponse, nil)
			}
			containers += count
			seen[key] = true
			result.pods = append(result.pods, pod)
		}
		cursor = list.Continue
		if cursor == "" {
			return sortedPods(result), nil
		}
		if len(cursor) > 4096 || cursors[cursor] || len(list.Items) == 0 {
			return podInventory{}, queryFailure(capability.InvalidResponse, nil)
		}
		cursors[cursor] = true
	}
	return limitedPods(result), nil
}

func validPodIdentity(pod corev1.Pod, namespace string) bool {
	return (namespace == "" || namespace == pod.Namespace) &&
		len(validation.IsDNS1123Label(pod.Namespace)) == 0 && len(validation.IsDNS1123Subdomain(pod.Name)) == 0 &&
		pod.UID != "" && len(pod.UID) <= 128 &&
		(pod.Spec.NodeName == "" || len(validation.IsDNS1123Subdomain(pod.Spec.NodeName)) == 0)
}

func validContainerNames(pod corev1.Pod) bool {
	seen := map[string]bool{}
	add := func(name string) bool {
		if len(validation.IsDNS1123Label(name)) != 0 || seen[name] {
			return false
		}
		seen[name] = true
		return true
	}
	for _, container := range pod.Spec.Containers {
		if !add(container.Name) {
			return false
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if !add(container.Name) {
			return false
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if !add(container.Name) {
			return false
		}
	}
	return len(pod.Spec.Containers) > 0
}

func limitedPods(inventory podInventory) podInventory {
	inventory.completeness, inventory.reason = capability.Partial, limitReached
	return sortedPods(inventory)
}

func sortedPods(inventory podInventory) podInventory {
	sort.Slice(inventory.pods, func(i, j int) bool {
		a, b := inventory.pods[i], inventory.pods[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return inventory
}
