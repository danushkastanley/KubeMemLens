package kube

import (
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PodIndex struct {
	ByPodUID                 map[string][]PodRef
	ByContainerID            map[string]PodRef
	NodeContext              api.NodeContext
	WorkloadContextAvailable bool
	WorkloadContextErrors    int
}

func EmptyPodIndex() PodIndex {
	return PodIndex{
		ByPodUID:      map[string][]PodRef{},
		ByContainerID: map[string]PodRef{},
	}
}

func NormalizeContainerID(raw string) string {
	value := strings.TrimSpace(raw)
	for _, prefix := range []string{"containerd://", "docker://", "cri-o://", "crio://"} {
		value = strings.TrimPrefix(value, prefix)
	}
	return strings.ToLower(value)
}

func BuildPodIndexFromPods(pods []corev1.Pod) PodIndex {
	idx := EmptyPodIndex()

	for _, pod := range pods {
		statuses := make([]corev1.ContainerStatus, 0,
			len(pod.Status.InitContainerStatuses)+
				len(pod.Status.ContainerStatuses)+
				len(pod.Status.EphemeralContainerStatuses),
		)
		statuses = append(statuses, pod.Status.InitContainerStatuses...)
		statuses = append(statuses, pod.Status.ContainerStatuses...)
		statuses = append(statuses, pod.Status.EphemeralContainerStatuses...)

		for _, status := range statuses {
			containerID := NormalizeContainerID(status.ContainerID)
			if containerID == "" {
				continue
			}

			ref := PodRef{
				Namespace:     pod.Namespace,
				PodName:       pod.Name,
				PodUID:        normalizePodUID(string(pod.UID)),
				ContainerName: status.Name,
				NodeName:      pod.Spec.NodeName,
				ContainerID:   containerID,
				Runtime:       ContainerRuntime(status.ContainerID),
				Context:       containerContext(pod, status),
			}
			idx.ByContainerID[containerID] = ref
			idx.ByPodUID[ref.PodUID] = append(idx.ByPodUID[ref.PodUID], ref)
		}
	}

	return idx
}

func containerContext(pod corev1.Pod, status corev1.ContainerStatus) api.ContainerContext {
	context := api.ContainerContext{
		QoSClass:         string(pod.Status.QOSClass),
		RestartCount:     status.RestartCount,
		PodPhase:         string(pod.Status.Phase),
		PodCreatedAt:     pod.CreationTimestamp.Time,
		RuntimeClassName: runtimeClassName(pod),
		Labels:           copyLabels(pod.Labels),
	}
	context.MemoryEmptyDirCount, context.MemoryEmptyDirLimited, context.MemoryEmptyDirLimitBytes = memoryEmptyDirContext(pod)
	if owner := controllerOwner(pod); owner != nil {
		context.OwnerKind = owner.Kind
		context.OwnerName = owner.Name
		if owner.Kind != "ReplicaSet" && owner.Kind != "Job" {
			context.WorkloadKind = owner.Kind
			context.WorkloadName = owner.Name
		}
	}
	if resources, ok := containerResources(pod, status.Name); ok {
		if request, exists := resources.Requests[corev1.ResourceMemory]; exists {
			context.MemoryRequestKnown = true
			context.MemoryRequestBytes = quantityBytes(request)
		}
		if limit, exists := resources.Limits[corev1.ResourceMemory]; exists {
			context.MemoryLimitKnown = true
			context.MemoryLimitBytes = quantityBytes(limit)
		}
	}
	if status.LastTerminationState.Terminated != nil {
		terminated := status.LastTerminationState.Terminated
		context.LastTerminationKnown = true
		context.LastTerminationReason = terminated.Reason
		context.LastTerminationExitCode = terminated.ExitCode
		context.LastTerminationFinishedAt = terminated.FinishedAt.Time
	}
	return context
}

func runtimeClassName(pod corev1.Pod) string {
	if pod.Spec.RuntimeClassName == nil {
		return ""
	}
	return strings.TrimSpace(*pod.Spec.RuntimeClassName)
}

func memoryEmptyDirContext(pod corev1.Pod) (count, limited int, limitBytes uint64) {
	for _, volume := range pod.Spec.Volumes {
		if volume.EmptyDir == nil || volume.EmptyDir.Medium != corev1.StorageMediumMemory {
			continue
		}
		count++
		if volume.EmptyDir.SizeLimit != nil {
			limited++
			limitBytes += quantityBytes(*volume.EmptyDir.SizeLimit)
		}
	}
	return count, limited, limitBytes
}

func copyLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	copy := make(map[string]string, len(labels))
	for key, value := range labels {
		copy[key] = value
	}
	return copy
}

func containerResources(pod corev1.Pod, name string) (corev1.ResourceRequirements, bool) {
	for _, container := range pod.Spec.InitContainers {
		if container.Name == name {
			return container.Resources, true
		}
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == name {
			return container.Resources, true
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if container.Name == name {
			return container.Resources, true
		}
	}
	return corev1.ResourceRequirements{}, false
}

func controllerOwner(pod corev1.Pod) *metav1.OwnerReference {
	return preferredOwner(pod.OwnerReferences)
}

func preferredOwner(owners []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range owners {
		if owners[i].Controller != nil && *owners[i].Controller {
			return &owners[i]
		}
	}
	if len(owners) > 0 {
		return &owners[0]
	}
	return nil
}

func quantityBytes(value resource.Quantity) uint64 {
	bytes := value.Value()
	if bytes <= 0 {
		return 0
	}
	return uint64(bytes)
}

func ContainerRuntime(raw string) string {
	value := strings.TrimSpace(raw)
	for _, item := range []struct {
		prefix  string
		runtime string
	}{
		{prefix: "containerd://", runtime: "containerd"},
		{prefix: "docker://", runtime: "docker"},
		{prefix: "cri-o://", runtime: "cri-o"},
		{prefix: "crio://", runtime: "cri-o"},
	} {
		if strings.HasPrefix(value, item.prefix) {
			return item.runtime
		}
	}
	return "unknown"
}

func (idx PodIndex) Lookup(containerID string, podUID string) (PodRef, bool) {
	normalizedID := NormalizeContainerID(containerID)
	if normalizedID != "" {
		if ref, ok := idx.ByContainerID[normalizedID]; ok {
			return ref, true
		}

		if ref, ok := idx.lookupByPrefix(normalizedID); ok {
			return ref, true
		}
	}

	normalizedPodUID := normalizePodUID(podUID)
	if normalizedID == "" && normalizedPodUID != "" {
		refs := idx.ByPodUID[normalizedPodUID]
		if len(refs) == 1 {
			return refs[0], true
		}
	}

	return PodRef{}, false
}

func normalizePodUID(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
}

func (idx PodIndex) lookupByPrefix(containerID string) (PodRef, bool) {
	if len(containerID) < 12 {
		return PodRef{}, false
	}

	var match PodRef
	matches := 0
	for knownID, ref := range idx.ByContainerID {
		if len(knownID) < 12 {
			continue
		}
		if strings.HasPrefix(knownID, containerID) || strings.HasPrefix(containerID, knownID) {
			match = ref
			matches++
			if matches > 1 {
				return PodRef{}, false
			}
		}
	}

	if matches == 1 {
		return match, true
	}
	return PodRef{}, false
}
