package agentless

import (
	"math"
	"sort"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
)

func validateResources(pod corev1.Pod) error {
	lists := []corev1.ResourceList{pod.Status.AllocatedResources}
	add := func(resources *corev1.ResourceRequirements) {
		if resources != nil {
			lists = append(lists, resources.Requests, resources.Limits)
		}
	}
	add(pod.Spec.Resources)
	add(pod.Status.Resources)
	for _, container := range pod.Spec.Containers {
		add(&container.Resources)
	}
	for _, container := range pod.Spec.InitContainers {
		add(&container.Resources)
	}
	for _, container := range pod.Spec.EphemeralContainers {
		add(&container.Resources)
	}
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for _, status := range statuses {
			lists = append(lists, status.AllocatedResources)
			add(status.Resources)
		}
	}
	maximum := resource.NewQuantity(math.MaxInt64, resource.DecimalSI)
	for _, list := range lists {
		for name, value := range list {
			if name != corev1.ResourceMemory && !strings.HasPrefix(string(name), "hugepages-") {
				continue
			}
			if len(validation.IsQualifiedName(string(name))) != 0 || value.Sign() < 0 || value.Cmp(*maximum) > 0 {
				return queryFailure(capability.InvalidResponse, nil)
			}
		}
	}
	var requests, limits uint64
	addBudget := func(resources corev1.ResourceRequirements) bool {
		request := resources.Requests[corev1.ResourceMemory]
		limit := resources.Limits[corev1.ResourceMemory]
		return addResourceBytes(&requests, uint64(request.Value())) && addResourceBytes(&limits, uint64(limit.Value()))
	}
	for _, container := range pod.Spec.Containers {
		if !addBudget(container.Resources) {
			return queryFailure(capability.InvalidResponse, nil)
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if !addBudget(container.Resources) {
			return queryFailure(capability.InvalidResponse, nil)
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if !addBudget(container.Resources) {
			return queryFailure(capability.InvalidResponse, nil)
		}
	}
	var emptyDir uint64
	for _, volume := range pod.Spec.Volumes {
		if volume.EmptyDir == nil || volume.EmptyDir.SizeLimit == nil || volume.EmptyDir.Medium != corev1.StorageMediumMemory {
			continue
		}
		value := volume.EmptyDir.SizeLimit
		if value.Sign() < 0 || value.Cmp(*maximum) > 0 || !addResourceBytes(&emptyDir, uint64(value.Value())) {
			return queryFailure(capability.InvalidResponse, nil)
		}
	}
	var restarts int64
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for _, status := range statuses {
			restarts += int64(status.RestartCount)
			if status.RestartCount < 0 || restarts > math.MaxInt32 {
				return queryFailure(capability.InvalidResponse, nil)
			}
		}
	}
	return nil
}

func addResourceBytes(total *uint64, value uint64) bool {
	if value > math.MaxInt64-*total {
		return false
	}
	*total += value
	return true
}

func hugepageResources(resources corev1.ResourceRequirements) []observation.Hugepages {
	values := map[string]observation.Hugepages{}
	for name, value := range resources.Requests {
		if !strings.HasPrefix(string(name), "hugepages-") {
			continue
		}
		bytes := uint64(value.Value())
		values[string(name)] = observation.Hugepages{Resource: string(name), RequestBytes: &bytes}
	}
	for name, value := range resources.Limits {
		if !strings.HasPrefix(string(name), "hugepages-") {
			continue
		}
		bytes := uint64(value.Value())
		item := values[string(name)]
		item.Resource, item.LimitBytes = string(name), &bytes
		values[string(name)] = item
	}
	result := make([]observation.Hugepages, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Resource < result[j].Resource })
	return result
}
