package kube

import "github.com/danushkastanley/kube-memlens/internal/api"

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
