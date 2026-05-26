package kube

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

type PodIndex struct {
	ByPodUID      map[string][]PodRef
	ByContainerID map[string]PodRef
}

func NormalizeContainerID(raw string) string {
	value := strings.TrimSpace(raw)
	for _, prefix := range []string{"containerd://", "docker://", "cri-o://", "crio://"} {
		value = strings.TrimPrefix(value, prefix)
	}
	return strings.ToLower(value)
}

func BuildPodIndexFromPods(pods []corev1.Pod) PodIndex {
	idx := PodIndex{
		ByPodUID:      map[string][]PodRef{},
		ByContainerID: map[string]PodRef{},
	}

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
			}
			idx.ByContainerID[containerID] = ref
			idx.ByPodUID[ref.PodUID] = append(idx.ByPodUID[ref.PodUID], ref)
		}
	}

	return idx
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
