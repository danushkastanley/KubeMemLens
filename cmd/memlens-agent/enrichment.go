package main

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/kube"
)

// enrichmentBudget bounds the Kubernetes API lookups made before each scan.
// The lookups add workload and Node context to cgroup evidence; they must not
// spend the scan's own timeout, or a slow API leaves no time to read cgroups.
const enrichmentBudget = 2 * time.Second

// indexEnrichment holds the optional lookups. Either may be nil when the agent
// runs without Kubernetes metadata.
type indexEnrichment struct {
	resolveOwners func(context.Context, kube.PodIndex, time.Time) (kube.PodIndex, int)
	nodeContext   func(context.Context, time.Time) (api.NodeContext, error)
}

// apply adds workload owners and Node context within the budget. Owners that
// could not be resolved in time are counted, and are picked up by later scans
// from the resolver's cache.
func (e indexEnrichment) apply(ctx context.Context, budget time.Duration, index kube.PodIndex) (kube.PodIndex, int, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	// Node context is one read and carries the Node UID that publishing
	// needs, so it goes first and owner lookups share what is left.
	var nodeContext api.NodeContext
	var nodeErr error
	if e.nodeContext != nil {
		nodeContext, nodeErr = e.nodeContext(ctx, time.Now().UTC())
	}
	ownerErrors := 0
	if e.resolveOwners != nil {
		index, ownerErrors = e.resolveOwners(ctx, index, time.Now().UTC())
	}
	index.NodeContext = nodeContext
	return index, ownerErrors, nodeErr
}
