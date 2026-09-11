package incident

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

type NodeCaptureReader interface {
	client.NodeEvidenceReader
	client.NodeHistoryReader
}

type NodeCaptureOptions struct {
	Rank             nodeanalysis.Metric
	Limit            int
	IncludeHistory   bool
	IncludeSensitive bool
	ToolVersion      string
}

// CollectNode performs a new authorised read for every export attempt, including
// overwrite retries. No screen cache or previously granted scope enters it.
func CollectNode(ctx context.Context, reader NodeCaptureReader, name string, opts NodeCaptureOptions) (NodeBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var history *api.NodeContextHistory
	if opts.IncludeHistory {
		value, err := client.ReadNodeHistory(ctx, reader, name)
		if err != nil {
			return NodeBundle{}, err
		}
		history = &value
	}
	evidence, err := client.ReadNodeEvidence(ctx, reader, name, opts.Rank, opts.Limit)
	if err != nil {
		return NodeBundle{}, err
	}
	return NewNode(evidence, history, opts.ToolVersion, time.Now().UTC(), opts.IncludeSensitive)
}
