package client

import (
	"context"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

type NodeTopologyReader interface {
	NodeTopology(context.Context, string) (api.NodeMemoryTopology, error)
}

func (c *KubernetesAPIClient) NodeTopology(ctx context.Context, name string) (api.NodeMemoryTopology, error) {
	var result api.NodeMemoryTopology
	path, err := nodeContextPath(name)
	if err != nil {
		return result, err
	}
	if err := c.getBounded(ctx, "get Node topology", path+"/topology", &result, memorytopology.MaxReportBytes); err != nil {
		return result, err
	}
	if err := result.Validate(name); err != nil {
		return api.NodeMemoryTopology{}, readDecodeError("get Node topology", err)
	}
	return result, nil
}
