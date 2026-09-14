package admissionkube

import (
	"context"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
)

const maxContainers = 128

type Resolver struct{ core coreclient.CoreV1Interface }

func NewResolver(core coreclient.CoreV1Interface) *Resolver { return &Resolver{core: core} }

func (r *Resolver) Resolve(ctx context.Context, request admission.Request) (admission.Workload, error) {
	return r.resolve(ctx, request.Namespace(), request.Pod(), request.Container())
}

func (r *Resolver) Revalidate(ctx context.Context, previous admission.Workload) error {
	t := previous.Target
	current, err := r.resolve(ctx, t.Namespace, t.PodName, t.ContainerName)
	if err != nil {
		return err
	}
	if current.NodeName != previous.NodeName || current.QoS != previous.QoS || !sameTarget(current.Target, previous.Target) {
		return admission.ErrTargetChanged
	}
	return nil
}

func (r *Resolver) resolve(ctx context.Context, namespace, name, container string) (admission.Workload, error) {
	var result admission.Workload
	if r.core == nil || ctx.Err() != nil {
		return result, admission.ErrUnavailable
	}
	// Empty ResourceVersion requests current state. Retained snapshots, informers
	// and resourceVersion=0 must never establish a tracing lifetime.
	pod, err := r.core.Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return result, admission.ErrNotFound
	}
	if err != nil || pod == nil {
		return result, admission.ErrUnavailable
	}
	if pod.Namespace != namespace || pod.Name != name || pod.UID == "" || pod.Spec.NodeName == "" || pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return result, admission.ErrTargetChanged
	}
	status, err := runningContainer(pod, container)
	if err != nil {
		return result, err
	}
	if !strings.HasPrefix(status.ContainerID, "containerd://") {
		return result, admission.ErrUnavailable
	}
	node, err := r.core.Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil || node == nil {
		return result, admission.ErrUnavailable
	}
	if node.Name != pod.Spec.NodeName || node.UID == "" || node.DeletionTimestamp != nil {
		return result, admission.ErrTargetChanged
	}
	target := trace.TargetIdentity{Namespace: namespace, PodName: name, PodUID: string(pod.UID), ContainerName: container, ContainerID: strings.TrimPrefix(status.ContainerID, "containerd://"), ContainerStartedAt: status.State.Running.StartedAt.Time.UTC(), NodeUID: string(node.UID)}
	if target.ValidateLifetime() != nil {
		return result, admission.ErrUnavailable
	}
	switch pod.Status.QOSClass {
	case corev1.PodQOSGuaranteed, corev1.PodQOSBurstable, corev1.PodQOSBestEffort:
	default:
		return result, admission.ErrUnavailable
	}
	return admission.Workload{Target: target, NodeName: node.Name, QoS: string(pod.Status.QOSClass)}, nil
}

func runningContainer(pod *corev1.Pod, name string) (corev1.ContainerStatus, error) {
	var found corev1.ContainerStatus
	if len(pod.Spec.Containers)+len(pod.Spec.InitContainers)+len(pod.Spec.EphemeralContainers) > maxContainers || len(pod.Status.ContainerStatuses)+len(pod.Status.InitContainerStatuses)+len(pod.Status.EphemeralContainerStatuses) > maxContainers {
		return found, admission.ErrUnavailable
	}
	declared := [3]int{}
	for _, c := range pod.Spec.Containers {
		if c.Name == name {
			declared[0]++
		}
	}
	for _, c := range pod.Spec.InitContainers {
		if c.Name == name {
			declared[1]++
		}
	}
	for _, c := range pod.Spec.EphemeralContainers {
		if c.Name == name {
			declared[2]++
		}
	}
	count := 0
	for category, statuses := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for _, status := range statuses {
			if status.Name == name {
				if declared[category] != 1 {
					return found, admission.ErrTargetChanged
				}
				found = status
				count++
			}
		}
	}
	if declared[0]+declared[1]+declared[2] != 1 || count != 1 || found.State.Running == nil || found.State.Waiting != nil || found.State.Terminated != nil || found.State.Running.StartedAt.IsZero() {
		return corev1.ContainerStatus{}, admission.ErrTargetChanged
	}
	return found, nil
}

func sameTarget(a, b trace.TargetIdentity) bool {
	return a.Namespace == b.Namespace && a.PodName == b.PodName && a.PodUID == b.PodUID && a.ContainerName == b.ContainerName && a.ContainerID == b.ContainerID && a.ContainerStartedAt.Equal(b.ContainerStartedAt) && a.NodeUID == b.NodeUID && a.CgroupID == b.CgroupID
}
