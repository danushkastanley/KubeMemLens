package incident

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

type TopologyCaptureReader interface {
	NodeCaptureReader
	client.NodeTopologyReader
}

func CollectTopology(ctx context.Context, reader TopologyCaptureReader, name string, opts NodeCaptureOptions) (TopologyBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var history *api.NodeContextHistory
	if opts.IncludeHistory {
		h, err := client.ReadNodeHistory(ctx, reader, name)
		if err != nil {
			return TopologyBundle{}, err
		}
		history = &h
	}
	evidence, err := client.ReadNodeEvidence(ctx, reader, name, opts.Rank, opts.Limit)
	if err != nil {
		return TopologyBundle{}, err
	}
	topology, err := reader.NodeTopology(ctx, name)
	if err != nil {
		return TopologyBundle{}, err
	}
	node, err := NewNode(evidence, history, opts.ToolVersion, time.Now().UTC(), true)
	if err != nil {
		return TopologyBundle{}, err
	}
	return NewTopology(node, topology, opts.IncludeSensitive)
}
