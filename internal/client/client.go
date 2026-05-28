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
	DebugStore(ctx context.Context) (api.DebugStore, error)
}
