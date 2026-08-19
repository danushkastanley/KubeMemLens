package tui

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

type Options struct {
	ConnectionOptions     client.Options
	SnapshotReader        client.SnapshotReader
	ConnectionDescription string
	RefreshInterval       time.Duration
	Namespace             string
	AllNamespaces         bool
}

type viewMode int

const (
	viewNodes viewMode = iota
	viewNamespaces
	viewWorkloads
	viewPods
	viewContainers
	viewDetail
	viewModeCount
)

type sortMode int

const (
	sortRisk sortMode = iota
	sortTotal
	sortRSS
	sortCache
	sortShmem
	sortName
)

type snapshotData struct {
	Nodes      []api.NodeSnapshotStatus
	Namespaces []api.NamespaceSnapshot
	Workloads  []api.WorkloadSnapshot
	Pods       []api.PodSnapshot
	Containers []api.ContainerSnapshot
	FetchedAt  time.Time
}

func (v viewMode) String() string {
	switch v {
	case viewNodes:
		return "nodes"
	case viewNamespaces:
		return "namespaces"
	case viewPods:
		return "pods"
	case viewWorkloads:
		return "workloads"
	case viewContainers:
		return "containers"
	case viewDetail:
		return "detail"
	default:
		return "unknown"
	}
}

type entityKind int

const (
	entityNone entityKind = iota
	entityNode
	entityNamespace
	entityWorkload
	entityPod
	entityContainer
)

type entityRef struct {
	kind          entityKind
	namespace     string
	name          string
	workloadKind  string
	podName       string
	containerName string
	nodeName      string
}

func (s sortMode) String() string {
	switch s {
	case sortRisk:
		return "risk desc"
	case sortTotal:
		return "total desc"
	case sortRSS:
		return "rss desc"
	case sortCache:
		return "cache desc"
	case sortShmem:
		return "shmem desc"
	case sortName:
		return "name asc"
	default:
		return "unknown"
	}
}

func nextSort(s sortMode) sortMode {
	switch s {
	case sortRisk:
		return sortTotal
	case sortTotal:
		return sortRSS
	case sortRSS:
		return sortCache
	case sortCache:
		return sortShmem
	case sortShmem:
		return sortName
	default:
		return sortRisk
	}
}
