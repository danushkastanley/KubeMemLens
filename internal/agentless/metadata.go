package agentless

import (
	"context"
	"maps"
	"strings"
	"time"
	"unicode"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	corev1 "k8s.io/api/core/v1"
)

func podMetadata(ctx context.Context, pod corev1.Pod, receivedAt time.Time) (observation.Pod, error) {
	if err := validateResources(pod); err != nil {
		return observation.Pod{}, err
	}
	result := observation.Pod{Namespace: pod.Namespace, Name: pod.Name, UID: string(pod.UID), NodeName: pod.Spec.NodeName,
		StatusEvidence: statusEnvelope(receivedAt, capability.PodScope), OwnerAvailability: capability.Available, Containers: []observation.Container{}}
	statuses := map[string]corev1.ContainerStatus{}
	for _, group := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for _, status := range group {
			if _, duplicate := statuses[status.Name]; duplicate {
				return observation.Pod{}, queryFailure(capability.InvalidResponse, nil)
			}
			statuses[status.Name] = status
		}
	}
	// Labels belong to the Pod. Duplicating its entire map into every container
	// can multiply a bounded API response into an unbounded application result.
	contextPod := pod
	contextPod.Labels = nil
	contexts := make([]api.ContainerContext, 0, len(pod.Spec.Containers))
	appendContainer := func(name, kind string, resources corev1.ResourceRequirements) error {
		if ctx.Err() != nil {
			return readFailure(ctx.Err())
		}
		status, reported := statuses[name]
		status.Name = name
		context := kube.BuildContainerContext(contextPod, status)
		context.QoSClass = safeText(context.QoSClass, 64)
		context.RuntimeClassName = safeText(context.RuntimeClassName, 253)
		context.LastTerminationReason = safeText(context.LastTerminationReason, 128)
		context.PodPhase = safeText(context.PodPhase, 64)
		context.OwnerKind = safeText(context.OwnerKind, 64)
		context.OwnerName = safeText(context.OwnerName, 253)
		context.WorkloadKind = safeText(context.WorkloadKind, 64)
		context.WorkloadName = safeText(context.WorkloadName, 253)
		container := observation.Container{Name: name, Kind: kind, State: "unreported", Context: context, Hugepages: hugepageResources(resources)}
		switch {
		case status.State.Running != nil:
			container.State, container.StartedAt = "running", status.State.Running.StartedAt.Time
		case status.State.Terminated != nil:
			container.State, container.StartedAt = "terminated", status.State.Terminated.StartedAt.Time
			container.StateReason = safeText(status.State.Terminated.Reason, 128)
			exitCode := status.State.Terminated.ExitCode
			container.ExitCode = &exitCode
		case status.State.Waiting != nil:
			container.State = "waiting"
			container.StateReason = safeText(status.State.Waiting.Reason, 128)
		case reported:
			container.State = "unknown"
		}
		result.Containers = append(result.Containers, container)
		contexts = append(contexts, context)
		return nil
	}
	for _, container := range pod.Spec.Containers {
		if err := appendContainer(container.Name, "application", container.Resources); err != nil {
			return observation.Pod{}, err
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if err := appendContainer(container.Name, "init", container.Resources); err != nil {
			return observation.Pod{}, err
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if err := appendContainer(container.Name, "ephemeral", container.Resources); err != nil {
			return observation.Pod{}, err
		}
	}
	result.Context = aggregate.ContextForContainers(contexts)
	result.Context.Labels = maps.Clone(pod.Labels)
	if pod.Spec.Resources != nil {
		result.Hugepages = hugepageResources(*pod.Spec.Resources)
	}
	return result, nil
}

func statusEnvelope(receivedAt time.Time, scope capability.Scope) capability.Envelope {
	// Object creation/condition transition times are not sample timestamps.
	return capability.Envelope{Source: capability.KubernetesStatus, APIVersion: "v1", ReceivedAt: receivedAt, Scope: scope,
		Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Stable}
}

func safeText(value string, limit int) string {
	var output strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if output.Len()+len(string(r)) > limit {
			break
		}
		output.WriteRune(r)
	}
	return output.String()
}
