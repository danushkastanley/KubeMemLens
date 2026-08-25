package collector

import (
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func newGeneration(startedAt time.Time) string {
	return strconv.FormatInt(startedAt.UnixNano(), 36)
}

func freshness(capturedAt, now time.Time, ttl time.Duration) api.EvidenceFreshness {
	if isStale(capturedAt, now, ttl) {
		return api.EvidenceFreshnessStale
	}
	return api.EvidenceFreshnessFresh
}

func (s *Store) Reliability(now time.Time, ttl time.Duration) api.CollectorReliability {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history.prune(now)
	return s.reliabilityLocked(now, ttl)
}

func (s *Store) HistoryReliability(now time.Time) api.HistoryReliability {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history.prune(now)
	return s.history.reliability(now)
}

func (s *Store) Generation() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

func (s *Store) reliabilityLocked(now time.Time, ttl time.Duration) api.CollectorReliability {
	freshNodes, staleNodes := 0, 0
	partial := false
	for _, snapshot := range s.nodes {
		if isStale(snapshot.capturedAt, now, ttl) {
			staleNodes++
		} else {
			freshNodes++
		}
		partial = partial || nodeEvidencePartial(snapshot)
	}
	missingNodes := 0
	if s.inventoryKnown {
		for node := range s.expectedNodes {
			if _, exists := s.nodes[node]; !exists {
				missingNodes++
			}
		}
	}

	state := api.CollectorReady
	completeness := api.EvidenceComplete
	switch {
	case len(s.nodes) == 0:
		state, completeness = api.CollectorRebuilding, api.EvidencePartial
	case freshNodes == 0 && staleNodes > 0:
		state, completeness = api.CollectorStale, api.EvidencePartial
	case staleNodes > 0 || missingNodes > 0 || partial:
		state, completeness = api.CollectorDegraded, api.EvidencePartial
	}
	if state != s.state {
		s.state = state
		s.transitionedAt = now
	}

	return api.CollectorReliability{
		State: state, Generation: s.generation, StartedAt: s.startedAt,
		TransitionedAt: s.transitionedAt, FirstSnapshotAt: s.firstSnapshotAt,
		LastSnapshotAt: s.lastSnapshotAt, LastReceivedAt: s.lastReceivedAt,
		FreshNodes: freshNodes, StaleNodes: staleNodes, MissingNodes: missingNodes,
		ExpectedNodes: len(s.expectedNodes), InventoryUpdatedAt: s.inventoryUpdatedAt,
		Completeness: completeness, SnapshotTTLSeconds: int64(ttl.Seconds()),
		History: s.history.reliability(now),
	}
}

func (s *Store) ReconcileExpectedNodes(nodes []string, observedAt time.Time) error {
	expected := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node != "" {
			expected[node] = struct{}{}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(expected) > s.limits.MaxNodes {
		return ErrStoreCapacity
	}
	for node, snapshot := range s.nodes {
		if _, exists := expected[node]; exists {
			continue
		}
		s.containerCount -= len(snapshot.containers)
		delete(s.nodes, node)
	}
	s.expectedNodes = expected
	s.inventoryKnown = true
	s.inventoryUpdatedAt = observedAt
	return nil
}

func nodeEvidencePartial(snapshot nodeSnapshot) bool {
	environment := snapshot.environment
	if environment.CgroupReadErrors > 0 || environment.WorkloadContextErrors > 0 ||
		!environment.NodeContextAvailable || !environment.WorkloadContextAvailable {
		return true
	}
	for _, container := range snapshot.containers {
		if container.Namespace == "" || container.PodName == "" || container.ContainerName == "" {
			return true
		}
	}
	return false
}

func nodeCompleteness(snapshot nodeSnapshot) api.EvidenceCompleteness {
	if nodeEvidencePartial(snapshot) {
		return api.EvidencePartial
	}
	return api.EvidenceComplete
}
