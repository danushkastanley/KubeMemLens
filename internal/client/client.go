package client

import (
	"context"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

type SnapshotReader interface {
	Health(ctx context.Context) error
	Containers(ctx context.Context) ([]api.ContainerSnapshot, error)
	Pods(ctx context.Context) ([]api.PodSnapshot, error)
	Namespaces(ctx context.Context) ([]api.NamespaceSnapshot, error)
	Nodes(ctx context.Context) ([]api.NodeSnapshotStatus, error)
	Workloads(ctx context.Context) ([]api.WorkloadSnapshot, error)
	PodHistory(ctx context.Context, namespace, podName string) ([]api.PodHistory, error)
	DebugStore(ctx context.Context) (api.DebugStore, error)
}

type PodReader interface {
	Pod(ctx context.Context, namespace, podName string) (api.PodSnapshot, error)
}

type MetricsReader interface {
	Metrics(ctx context.Context) (api.Metrics, error)
}
