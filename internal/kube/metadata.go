package kube

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildContainerContext exposes the same resource and resize interpretation to
// direct Kubernetes readers, including containers without a runtime ID.
func BuildContainerContext(pod corev1.Pod, status corev1.ContainerStatus) api.ContainerContext {
	return containerContext(pod, status)
}

func PodOwnerReference(pod corev1.Pod) *metav1.OwnerReference {
	owner := controllerOwner(pod)
	if owner == nil {
		return nil
	}
	copy := *owner
	return &copy
}

// PodRef is the Kubernetes metadata that future node-agent snapshots will use
// to connect a cgroup memory sample back to a pod/container.
type PodRef struct {
	Namespace     string
	PodName       string
	PodUID        string
	ContainerName string
	NodeName      string
	ContainerID   string
	Runtime       string
	Running       bool
	Context       api.ContainerContext
}
