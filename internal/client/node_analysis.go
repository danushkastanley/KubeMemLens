package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

type NodeAnalysisReader interface {
	NodeAnalysis(context.Context, string, nodeanalysis.Metric, int) (nodeanalysis.Analysis, error)
}

func (c *KubernetesAPIClient) NodeAnalysis(ctx context.Context, name string, rank nodeanalysis.Metric, limit int) (nodeanalysis.Analysis, error) {
	path, err := nodeContextPath(name)
	if err != nil {
		return nodeanalysis.Analysis{}, err
	}
	if limit == 0 {
		limit = nodeanalysis.DefaultContributors
	}
	if limit < 1 || limit > nodeanalysis.MaxContributors {
		return nodeanalysis.Analysis{}, fmt.Errorf("contributor limit must be between 1 and 100")
	}
	query := url.Values{"rank": {string(rank)}, "limit": {strconv.Itoa(limit)}}
	var result api.NodeMemoryAnalysis
	if err := c.get(ctx, "get Node memory analysis", path+"/analysis?"+query.Encode(), &result); err != nil {
		return nodeanalysis.Analysis{}, err
	}
	if result.Analysis.SchemaVersion != nodeanalysis.SchemaVersion {
		return nodeanalysis.Analysis{}, fmt.Errorf("unsupported Node analysis schema")
	}
	return result.Analysis, nil
}
