package agentless

import (
	"context"
	"sync"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
)

var _ observation.Reader = (*Reader)(nil)

// Current authorises every refresh through the caller's API transport. Pod
// inventory is required; optional sources cannot erase valid resource rows.
func (r *Reader) Current(parent context.Context) (observation.Batch, error) {
	ctx, cancel := context.WithTimeout(parent, r.opts.Timeout)
	defer cancel()
	select {
	case r.refresh <- struct{}{}:
		defer func() { <-r.refresh }()
	case <-ctx.Done():
		return observation.Batch{}, readFailure(ctx.Err())
	}
	ctx = withReadBudget(ctx)
	inventory, podErr := r.readPods(ctx)
	if podErr != nil {
		return observation.Batch{}, podErr
	}
	if ctx.Err() != nil {
		return observation.Batch{}, readFailure(ctx.Err())
	}
	at := r.opts.Now().UTC()
	batch := observation.Batch{Mode: capability.Restricted, ReceivedAt: at, Completeness: inventory.completeness,
		Pods: make([]observation.Pod, 0, len(inventory.pods)), Nodes: []observation.Node{}}
	for _, pod := range inventory.pods {
		row, err := podMetadata(ctx, pod, at)
		if err != nil {
			return observation.Batch{}, err
		}
		batch.Pods = append(batch.Pods, row)
	}
	metrics, metricErr := r.metrics.Read(ctx)
	if metricErr != nil && metrics.Availability == "" {
		metrics.Availability, metrics.Reason = resourcemetrics.Unavailable, resourcemetrics.RequestFailed
	}
	joinPodMetrics(batch.Pods, metrics, r.opts.Now().UTC())
	var nodeStatus, nodeMetrics observation.SourceReport
	var enrichment sync.WaitGroup
	enrichment.Add(2)
	go func() { defer enrichment.Done(); r.enrichOwners(ctx, inventory.pods, batch.Pods) }()
	// Node selection only reads immutable Pod identities while owners update
	// their own metadata fields.
	go func() {
		defer enrichment.Done()
		batch.Nodes, nodeStatus = r.readNodes(ctx, batch.Pods, at)
		batch.Nodes, nodeMetrics = r.enrichNodeMetrics(ctx, batch.Nodes, nodeStatus, at)
	}()
	enrichment.Wait()
	if parent.Err() != nil {
		return observation.Batch{}, readFailure(parent.Err())
	}
	batch.ReceivedAt = r.opts.Now().UTC()
	podStatus := statusReport(capability.PodScope)
	podStatus.Completeness, podStatus.Reason = inventory.completeness, inventory.reason
	podMetrics := metricsReport(metrics.Availability, metrics.Reason, metrics.APIVersion, capability.PodScope)
	quantities := make([]observation.WorkingSet, 0, len(batch.Pods))
	for _, pod := range batch.Pods {
		quantities = append(quantities, pod.WorkingSet)
		if pod.OwnerEvidence.Completeness != capability.Complete || pod.StatusEvidence.Completeness != capability.Complete {
			batch.Completeness = capability.Partial
		}
	}
	applyCoverage(&podMetrics, quantities)
	batch.Sources = []observation.SourceReport{podStatus, podMetrics, nodeStatus, nodeMetrics}
	for _, source := range batch.Sources {
		if source.Completeness != capability.Complete {
			batch.Completeness = capability.Partial
		}
	}
	batch.Namespaces, batch.Workloads = observation.GroupPods(batch.Pods)
	if inventory.completeness == capability.Partial {
		batch.Caveats = append(batch.Caveats, "The Pod inventory reached a read limit; groups cover only the returned Pods.")
		for i := range batch.Namespaces {
			batch.Namespaces[i].WorkingSet.Evidence.Completeness = capability.Partial
		}
		for i := range batch.Workloads {
			batch.Workloads[i].WorkingSet.Evidence.Completeness = capability.Partial
		}
	}
	if err := checkOutputSize(batch, r.opts.MaxOutputBytes); err != nil {
		return observation.Batch{}, err
	}
	if parent.Err() != nil {
		return observation.Batch{}, readFailure(parent.Err())
	}
	return batch, nil
}
