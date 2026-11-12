package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/kube"
)

// A slow API must cost the scan at most the enrichment budget, and must leave
// the caller's own context, and so the scan's timeout, untouched.
func TestEnrichmentCannotSpendTheScanTimeout(t *testing.T) {
	blockedUntilBudget := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	enrichment := indexEnrichment{
		resolveOwners: func(ctx context.Context, index kube.PodIndex, _ time.Time) (kube.PodIndex, int) {
			_ = blockedUntilBudget(ctx)
			return index, 7
		},
		nodeContext: func(ctx context.Context, _ time.Time) (api.NodeContext, error) {
			return api.NodeContext{}, blockedUntilBudget(ctx)
		},
	}
	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	started := time.Now()
	_, ownerErrors, nodeErr := enrichment.apply(parent, 30*time.Millisecond, kube.EmptyPodIndex())
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("enrichment took %s with a 30ms budget", elapsed)
	}
	if ownerErrors != 7 || !errors.Is(nodeErr, context.DeadlineExceeded) {
		t.Fatalf("owner errors = %d, node error = %v", ownerErrors, nodeErr)
	}
	if parent.Err() != nil {
		t.Fatalf("the caller's context was cancelled: %v", parent.Err())
	}
}

func TestEnrichmentAddsOwnersAndNodeContext(t *testing.T) {
	enrichment := indexEnrichment{
		resolveOwners: func(_ context.Context, index kube.PodIndex, _ time.Time) (kube.PodIndex, int) {
			index.WorkloadContextAvailable = true
			return index, 0
		},
		nodeContext: func(context.Context, time.Time) (api.NodeContext, error) {
			return api.NodeContext{Available: true, MemoryPressureStatus: "False"}, nil
		},
	}
	index, ownerErrors, nodeErr := enrichment.apply(context.Background(), time.Second, kube.EmptyPodIndex())
	if ownerErrors != 0 || nodeErr != nil || !index.WorkloadContextAvailable || !index.NodeContext.Available {
		t.Fatalf("index=%+v errors=%d node=%v", index, ownerErrors, nodeErr)
	}
}

func TestEnrichmentWithoutKubernetesLeavesTheIndexAlone(t *testing.T) {
	index, ownerErrors, nodeErr := indexEnrichment{}.apply(context.Background(), time.Second, kube.EmptyPodIndex())
	if ownerErrors != 0 || nodeErr != nil || index.WorkloadContextAvailable || index.NodeContext.Available {
		t.Fatalf("index=%+v errors=%d node=%v", index, ownerErrors, nodeErr)
	}
}

// Publishing needs the Node UID, so the single Node read must not wait behind
// a long queue of owner lookups.
func TestEnrichmentReadsNodeContextBeforeOwners(t *testing.T) {
	var order []string
	enrichment := indexEnrichment{
		resolveOwners: func(_ context.Context, index kube.PodIndex, _ time.Time) (kube.PodIndex, int) {
			order = append(order, "owners")
			return index, 0
		},
		nodeContext: func(context.Context, time.Time) (api.NodeContext, error) {
			order = append(order, "node")
			return api.NodeContext{Available: true, NodeUID: "node-uid"}, nil
		},
	}
	index, _, _ := enrichment.apply(context.Background(), time.Second, kube.EmptyPodIndex())
	if len(order) != 2 || order[0] != "node" || index.NodeContext.NodeUID != "node-uid" {
		t.Fatalf("order = %v, node context = %+v", order, index.NodeContext)
	}
}
