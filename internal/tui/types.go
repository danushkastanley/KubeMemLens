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
	viewNamespaces viewMode = iota
	viewWorkloads
	viewPods
	viewContainers
	viewDetail
)

type sortMode int

const (
	sortTotal sortMode = iota
	sortRSS
	sortCache
	sortShmem
	sortName
)

type snapshotData struct {
	Namespaces []api.NamespaceSnapshot
	Workloads  []api.WorkloadSnapshot
	Pods       []api.PodSnapshot
	Containers []api.ContainerSnapshot
	FetchedAt  time.Time
}

func (v viewMode) String() string {
	switch v {
	case viewNamespaces:
		return "namespaces"
	case viewPods:
		return "pods"
	case viewWorkloads:
		return "workloads"
	case viewContainers:
		return "containers"
	case viewDetail:
		return "pod detail"
	default:
		return "unknown"
	}
}

func (s sortMode) String() string {
	switch s {
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
	case sortTotal:
		return sortRSS
	case sortRSS:
		return sortCache
	case sortCache:
		return sortShmem
	case sortShmem:
		return sortName
	default:
		return sortTotal
	}
}
