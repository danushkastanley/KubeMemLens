package observation

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func FromDeepNamespace(value api.NamespaceSnapshot, receivedAt time.Time) Group {
	return Group{Name: value.Namespace, Kind: "Namespace", PodCount: value.PodCount,
		Cgroup: &Cgroup{Memory: value.Memory, Evidence: deepEnvelope(value.CapturedAt, receivedAt, value.Freshness, value.Completeness, capability.NamespaceScope)}}
}

func FromDeepWorkload(value api.WorkloadSnapshot, receivedAt time.Time) Group {
	return Group{Namespace: value.Namespace, Name: value.Name, Kind: value.Kind, PodCount: value.PodCount,
		Cgroup: &Cgroup{Memory: value.Memory, Evidence: deepEnvelope(value.CapturedAt, receivedAt, value.Freshness, value.Completeness, capability.WorkloadScope)}}
}

func FromDeepNode(value api.NodeSnapshotStatus, receivedAt time.Time) Node {
	return Node{Name: value.NodeName, DeepStatus: &value}
}
