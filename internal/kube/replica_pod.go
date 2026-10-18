package kube

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func replicaPod(pod corev1.Pod) (replicabaseline.Member, error) {
	member := replicabaseline.Member{Object: changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name, UID: string(pod.UID)}, Node: pod.Spec.NodeName, StableSince: pod.CreationTimestamp.Time, Changes: replicabaseline.ChangeUnknown, PodBudget: memoryResourceBudget(pod.Spec.Resources)}
	if pod.Kind != "Pod" || pod.APIVersion != "v1" || member.Object.Validate() != nil || pod.CreationTimestamp.IsZero() {
		return member, replicabaseline.ErrInvalid
	}
	count := len(pod.Spec.Containers) + len(pod.Spec.InitContainers) + len(pod.Spec.EphemeralContainers)
	if count == 0 || count > replicabaseline.MaxContainers {
		return member, replicabaseline.ErrBounds
	}
	roles, err := replicaContainerRoles(pod)
	if err != nil {
		return member, err
	}
	member.Lifecycle = replicaLifecycle(pod)
	pending, applying := podResizeObservations(pod.Status)
	if pending.State != model.ResizeNone || applying.State != model.ResizeNone {
		member.Changes = replicabaseline.RecentChange
	}
	seen := map[string]bool{}
	groups := []struct {
		role     string
		statuses []corev1.ContainerStatus
	}{{"container", pod.Status.ContainerStatuses}, {"init", pod.Status.InitContainerStatuses}, {"ephemeral", pod.Status.EphemeralContainerStatuses}}
	for _, group := range groups {
		for _, status := range group.statuses {
			role, declared := roles[status.Name]
			if seen[status.Name] || !declared || status.RestartCount < 0 || (role != group.role && !(role == "sidecar" && group.role == "init")) {
				return member, replicabaseline.ErrInvalid
			}
			seen[status.Name] = true
			configured := configuredContainerMemory(pod, status.Name)
			if configured == nil {
				return member, replicabaseline.ErrInvalid
			}
			resources := memoryResourceContext(pod, status)
			// Keep reported presence even when the legacy snapshot omits an
			// unchanged container-only resource context.
			resources.Pod.Configured = member.PodBudget
			resources.Pod.AllocatedRequest = memoryResourceValue(pod.Status.AllocatedResources)
			resources.Pod.Applied = memoryResourceBudget(pod.Status.Resources)
			resources.AllocatedRequest = memoryResourceValue(status.AllocatedResources)
			resources.Applied = memoryResourceBudget(status.Resources)
			container := replicabaseline.Container{Name: status.Name, Role: role, ID: NormalizeContainerID(status.ContainerID), ImageDigest: replicaImageDigest(status.ImageID), Configured: *configured, Resources: resources}
			if err := container.Resources.Validate(); err != nil {
				return member, replicabaseline.ErrInvalid
			}
			switch {
			case status.State.Running != nil:
				container.StartedAt = status.State.Running.StartedAt.Time
			case status.State.Terminated != nil:
				container.StartedAt = status.State.Terminated.StartedAt.Time
				container.EndedAt = status.State.Terminated.FinishedAt.Time
			}
			if (!container.StartedAt.IsZero() && container.StartedAt.Before(pod.CreationTimestamp.Time)) || (!container.EndedAt.IsZero() && container.EndedAt.Before(container.StartedAt)) {
				return member, replicabaseline.ErrInvalid
			}
			if container.StartedAt.After(member.StableSince) {
				member.StableSince = container.StartedAt
			}
			if status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.FinishedAt.After(member.StableSince) {
				member.StableSince = status.LastTerminationState.Terminated.FinishedAt.Time
			}
			member.Containers = append(member.Containers, container)
		}
	}
	if len(member.Containers) > replicabaseline.MaxContainers {
		return member, replicabaseline.ErrBounds
	}
	slices.SortFunc(member.Containers, func(a, b replicabaseline.Container) int { return strings.Compare(a.Name, b.Name) })
	if len(member.Containers) == count {
		member.Shape = replicaShape(member)
	}
	return member, nil
}

func replicaContainerRoles(pod corev1.Pod) (map[string]string, error) {
	roles := map[string]string{}
	add := func(name, role string) error {
		if len(validation.IsDNS1123Label(name)) != 0 || roles[name] != "" {
			return replicabaseline.ErrInvalid
		}
		roles[name] = role
		return nil
	}
	for _, container := range pod.Spec.Containers {
		if err := add(container.Name, "container"); err != nil {
			return nil, err
		}
	}
	for _, container := range pod.Spec.InitContainers {
		role := "init"
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			role = "sidecar"
		}
		if err := add(container.Name, role); err != nil {
			return nil, err
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if err := add(container.Name, "ephemeral"); err != nil {
			return nil, err
		}
	}
	return roles, nil
}

func replicaLifecycle(pod corev1.Pod) replicabaseline.Lifecycle {
	if pod.DeletionTimestamp != nil {
		return replicabaseline.Terminating
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return replicabaseline.Inactive
	}
	if pod.Status.Phase == corev1.PodRunning {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return replicabaseline.Ready
			}
		}
	}
	return replicabaseline.Unready
}

func replicaImageDigest(value string) string {
	value = strings.TrimPrefix(strings.TrimPrefix(value, "containerd://"), "docker://")
	if _, digest, found := strings.Cut(value, "@sha256:"); found {
		value = "sha256:" + digest
	}
	text, found := strings.CutPrefix(value, "sha256:")
	decoded, err := hex.DecodeString(text)
	if !found || err != nil || len(decoded) != sha256.Size || strings.ToLower(text) != text {
		return ""
	}
	return value
}

func replicaShape(member replicabaseline.Member) string {
	type containerShape struct {
		Name, Role, ImageDigest string
		Configured              model.MemoryResourceBudget
		Allocated               model.ResourceValue
		Applied, PodApplied     model.MemoryResourceBudget
		PodAllocated            model.ResourceValue
	}
	value := struct {
		Pod        model.MemoryResourceBudget
		Containers []containerShape
	}{Pod: member.PodBudget}
	for _, container := range member.Containers {
		if container.ImageDigest == "" || container.ID == "" || container.StartedAt.IsZero() {
			return ""
		}
		value.Containers = append(value.Containers, containerShape{Name: container.Name, Role: container.Role, ImageDigest: container.ImageDigest, Configured: container.Configured, Allocated: container.Resources.AllocatedRequest, Applied: container.Resources.Applied, PodApplied: container.Resources.Pod.Applied, PodAllocated: container.Resources.Pod.AllocatedRequest})
	}
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
