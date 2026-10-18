package collector

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

// EnableReplicaComparisons enables continuity tracking within existing history.
// Call at startup only for the explicit replica-comparison profile.
func (s *Store) EnableReplicaComparisons() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replicasEnabled = true
}

// ReplicaEvidence projects only the resolved authorised members, under one store
// read lock. It never discovers peers from labels or stored workload names.
func (s *Store) ReplicaEvidence(ctx context.Context, selection replicabaseline.Selection, now time.Time) (replicabaseline.Input, error) {
	result := replicabaseline.Input{Workload: selection.Workload, ObservedAt: now, Peers: []replicabaseline.Peer{}}
	if selection.Workload.Validate() != nil || len(selection.Members) > replicabaseline.MaxPeers {
		return result, replicabaseline.ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, member := range selection.Members {
		if err := ctx.Err(); err != nil {
			return replicabaseline.Input{}, err
		}
		if member.Object.Namespace != selection.Workload.Namespace {
			return replicabaseline.Input{}, replicabaseline.ErrScope
		}
		peer := replicabaseline.Peer{Object: member.Object, WorkloadUID: selection.Workload.UID, Revision: member.Revision, Shape: member.Shape, Lifecycle: member.Lifecycle,
			StableSince: member.StableSince, Changes: member.Changes, SampleState: replicabaseline.Unreported, HistoryState: replicabaseline.Unreported, Values: map[replicabaseline.Metric]replicabaseline.Value{}}
		node, found := s.nodes[member.Node]
		if !found || member.NodeUID == "" || node.uid != member.NodeUID {
			result.Peers = append(result.Peers, peer)
			continue
		}
		peer.CapturedAt = node.capturedAt
		containers, complete := replicaContainers(member, node)
		if !complete {
			peer.SampleState = replicabaseline.Partial
			result.Peers = append(result.Peers, peer)
			continue
		}
		peer.SampleState = replicabaseline.Available
		if now.Sub(node.capturedAt) > replicabaseline.FreshFor {
			peer.SampleState = replicabaseline.Stale
		}
		peer.Shape = replicaMeasuredShape(member.Shape, containers)
		peer.Values, peer.DeltaStartedAt = replicaValues(containers, node.capturedAt)
		s.replicaHistory(&peer, member, node, containers)
		result.Peers = append(result.Peers, peer)
	}
	return result, nil
}

func replicaContainers(member replicabaseline.Member, node nodeSnapshot) ([]api.ContainerSnapshot, bool) {
	if nodeCompleteness(node) != api.EvidenceComplete {
		return nil, false
	}
	if len(member.Containers) == 0 || len(member.Containers) > replicabaseline.MaxContainers {
		return nil, false
	}
	expected := map[string]replicabaseline.Container{}
	for _, c := range member.Containers {
		// Completed init/ephemeral containers remain part of the revision shape,
		// but are not contributors to current charge.
		if !c.EndedAt.IsZero() {
			continue
		}
		if c.ID == "" || c.StartedAt.IsZero() || c.StartedAt.After(node.capturedAt) {
			return nil, false
		}
		expected[c.Name] = c
	}
	result := make([]api.ContainerSnapshot, 0, len(expected))
	for _, c := range node.containers {
		if c.Namespace != member.Object.Namespace || c.PodName != member.Object.Name || c.PodUID != member.Object.UID {
			continue
		}
		wanted, found := expected[c.ContainerName]
		if !found || c.ContainerID != wanted.ID || c.Completeness == api.EvidencePartial || !replicaResourcesMatch(wanted, c.Context) {
			return nil, false
		}
		delete(expected, c.ContainerName)
		result = append(result, c)
	}
	return result, len(expected) == 0 && len(result) > 0
}

func replicaResourcesMatch(wanted replicabaseline.Container, got api.ContainerContext) bool {
	configured := model.MemoryResourceBudget{Request: model.ResourceValue{Bytes: got.MemoryRequestBytes, Known: got.MemoryRequestKnown}, Limit: model.ResourceValue{Bytes: got.MemoryLimitBytes, Known: got.MemoryLimitKnown}}
	if configured != wanted.Configured {
		return false
	}
	// Older snapshots omit unchanged resource context. Any reported allocation
	// or application disagreement is still a mixed resize observation.
	actual := got.Resources
	if !actual.IsZero() {
		return actual.Pod.Configured == wanted.Resources.Pod.Configured && actual.Pod.AllocatedRequest == wanted.Resources.Pod.AllocatedRequest && actual.Pod.Applied == wanted.Resources.Pod.Applied && actual.AllocatedRequest == wanted.Resources.AllocatedRequest && actual.Applied == wanted.Resources.Applied
	}
	return wanted.Resources.Pod.Configured == (model.MemoryResourceBudget{}) &&
		(!wanted.Resources.Applied.Limit.Known || wanted.Resources.Applied.Limit == configured.Limit) &&
		(!wanted.Resources.Applied.Request.Known || wanted.Resources.Applied.Request == configured.Request) &&
		(!wanted.Resources.AllocatedRequest.Known || wanted.Resources.AllocatedRequest == configured.Request)
}

func (s *Store) replicaHistory(peer *replicabaseline.Peer, member replicabaseline.Member, node nodeSnapshot, containers []api.ContainerSnapshot) {
	key := historyKey(member.Object.Namespace, member.Object.Name, member.Object.UID, member.Node)
	coverage, series := s.history.coverage[key], s.history.series[key]
	if coverage == nil || series == nil || coverage.replica.fingerprint != replicaFingerprint(node.uid, containers) || coverage.replica.since.IsZero() {
		return
	}
	if coverage.replica.since.After(peer.StableSince) {
		peer.StableSince = coverage.replica.since
	}
	cutoff := node.capturedAt.Add(-replicabaseline.HistoryLookback)
	if peer.StableSince.After(cutoff) {
		cutoff = peer.StableSince
	}
	peer.HistoryState = replicabaseline.Available
	for _, point := range series.Points {
		if point.CapturedAt.Before(cutoff) || point.CapturedAt.After(node.capturedAt) {
			continue
		}
		if len(peer.History) == replicabaseline.MaxHistoryPoints {
			peer.History = nil
			peer.HistoryState = replicabaseline.Partial
			return
		}
		peer.History = append(peer.History, replicabaseline.Point{At: point.CapturedAt, Bytes: point.TotalBytes})
	}
}
