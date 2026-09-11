package agentless

import (
	"context"
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
)

func (r *Reader) enrichNodeMetrics(ctx context.Context, nodes []observation.Node, status observation.SourceReport, at time.Time) ([]observation.Node, observation.SourceReport) {
	var report resourcemetrics.NodeReport
	var err error
	if r.namespace == "" {
		report, err = r.metrics.ListNodes(ctx)
	} else {
		names := make([]string, 0, len(nodes))
		for _, node := range nodes {
			names = append(names, node.Name)
		}
		report, err = r.metrics.ReadNodes(ctx, names)
	}
	if err != nil && report.Availability == "" {
		report.Availability, report.Reason = resourcemetrics.Unavailable, resourcemetrics.RequestFailed
	}
	at = r.opts.Now().UTC()
	byName := map[string]int{}
	for i := range nodes {
		byName[nodes[i].Name] = i
		nodes[i].WorkingSet = missingSet(report.Availability, report.Reason, report.APIVersion, capability.NodeScope, at)
	}
	for _, value := range report.Observations {
		i, exists := byName[value.NodeName]
		if !exists {
			if r.namespace != "" {
				continue
			}
			if len(nodes) >= r.opts.MaxNodes {
				report.Availability, report.Reason = resourcemetrics.Partial, resourcemetrics.LimitReached
				break
			}
			i = len(nodes)
			availability, reason := status.Availability, status.Reason
			if availability == capability.Available {
				availability, reason = capability.Unreported, capability.NotObserved
			}
			nodes = append(nodes, unavailableNode(value.NodeName, at, availability, reason))
			byName[value.NodeName] = i
		}
		node := &nodes[i]
		if (node.UID != "" && value.NodeUID != "" && node.UID != value.NodeUID) || value.Timestamp.Before(node.CreatedAt) {
			node.WorkingSet = missingSet(resourcemetrics.Unavailable, resourcemetrics.InvalidResponse, value.APIVersion, capability.NodeScope, at)
			node.WorkingSet.Reason = observation.IdentityMismatch
			continue
		}
		sample := metricSample{bytes: value.MemoryWorkingSetBytes, version: value.APIVersion, at: value.Timestamp, window: value.Window, freshness: value.Freshness}
		if node.UID == "" || value.NodeUID == "" {
			sample.identityReason = observation.IdentityUnconfirmed
		}
		node.WorkingSet = sampledSet(sample, capability.NodeScope, at)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	state := metricsReport(report.Availability, report.Reason, report.APIVersion, capability.NodeScope)
	quantities := make([]observation.WorkingSet, 0, len(nodes))
	for _, node := range nodes {
		quantities = append(quantities, node.WorkingSet)
	}
	applyCoverage(&state, quantities)
	return nodes, state
}
