package collector

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

var ErrSnapshotOutOfOrder = errors.New("snapshot is older than the latest snapshot for this node")
var ErrStoreCapacity = errors.New("snapshot would exceed collector storage capacity")

type StoreLimits struct {
	MaxNodes      int
	MaxContainers int
}

func DefaultStoreLimits() StoreLimits {
	return StoreLimits{MaxNodes: 5000, MaxContainers: 100000}
}

type Store struct {
	mu                    sync.RWMutex
	nodes                 map[string]nodeSnapshot
	ingestionResults      map[string]uint64
	lastIngestionDuration time.Duration
	history               *historyStore
	limits                StoreLimits
	containerCount        int
}

type nodeSnapshot struct {
	capturedAt  time.Time
	environment api.NodeEnvironment
	containers  []api.ContainerSnapshot
}

func NewStore() *Store {
	return newStore(DefaultHistoryOptions(), DefaultStoreLimits())
}

func newStore(historyOpts HistoryOptions, limits StoreLimits) *Store {
	defaults := DefaultStoreLimits()
	if limits.MaxNodes <= 0 {
		limits.MaxNodes = defaults.MaxNodes
	}
	if limits.MaxContainers <= 0 {
		limits.MaxContainers = defaults.MaxContainers
	}
	return &Store{
		nodes:            map[string]nodeSnapshot{},
		ingestionResults: map[string]uint64{},
		history:          newHistoryStore(historyOpts),
		limits:           limits,
	}
}

// ReplaceNodeSnapshot atomically replaces everything previously reported by a
// node. This prevents terminated containers and old container IDs from being
// counted until TTL expiry. An empty snapshot deliberately clears the node.
func (s *Store) ReplaceNodeSnapshot(snapshot api.AgentSnapshot) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previousNode := s.nodes[snapshot.NodeName]
	if previousNode.capturedAt.IsZero() && len(s.nodes) >= s.limits.MaxNodes {
		return 0, ErrStoreCapacity
	}
	projectedContainers := s.containerCount - len(previousNode.containers) + len(snapshot.Containers)
	if projectedContainers > s.limits.MaxContainers {
		return 0, ErrStoreCapacity
	}
	if !previousNode.capturedAt.IsZero() {
		if snapshot.CapturedAt.Before(previousNode.capturedAt) {
			return 0, ErrSnapshotOutOfOrder
		}
		if snapshot.CapturedAt.Equal(previousNode.capturedAt) {
			return len(previousNode.containers), nil
		}
	}
	previousByContainer := map[string]model.MemoryBreakdown{}
	for _, container := range previousNode.containers {
		previousByContainer[container.ContainerID] = container.Memory
	}
	containers := make([]api.ContainerSnapshot, 0, len(snapshot.Containers))
	for _, container := range snapshot.Containers {
		container.CapturedAt = snapshot.CapturedAt
		container.NodeName = snapshot.NodeName
		container.DeltaStartedAt = time.Time{}
		container.DeltaWindowKnown = false
		previous, ok := previousByContainer[container.ContainerID]
		container.Memory = model.WithEventDeltas(container.Memory, previous, ok)
		if ok {
			container.DeltaStartedAt = previousNode.capturedAt
			container.DeltaWindowKnown = true
		}
		containers = append(containers, container)
	}
	s.nodes[snapshot.NodeName] = nodeSnapshot{
		capturedAt:  snapshot.CapturedAt,
		environment: snapshot.Environment,
		containers:  containers,
	}
	s.containerCount = projectedContainers
	s.history.record(snapshot.CapturedAt, containers)
	return len(containers), nil
}

func NewStoreWithHistory(opts HistoryOptions) *Store {
	return newStore(opts, DefaultStoreLimits())
}

func NewStoreWithHistoryAndLimits(historyOpts HistoryOptions, limits StoreLimits) *Store {
	return newStore(historyOpts, limits)
}

func (s *Store) ListPodHistory(namespace, podName, nodeName string, now time.Time) []api.PodHistory {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.history.list(namespace, podName, nodeName, now)
}

func (s *Store) ListContainers(now time.Time, ttl time.Duration) []api.ContainerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := []api.ContainerSnapshot{}
	for node, snapshot := range s.nodes {
		if isStale(snapshot.capturedAt, now, ttl) {
			// Preserve the lightweight last-seen record for freshness metrics,
			// but release stale per-container data.
			s.containerCount -= len(snapshot.containers)
			snapshot.containers = nil
			s.nodes[node] = snapshot
			continue
		}
		items = append(items, snapshot.containers...)
	}
	sortContainers(items)
	return items
}

func (s *Store) ListPods(now time.Time, ttl time.Duration) []api.PodSnapshot {
	return aggregate.Pods(s.ListContainers(now, ttl))
}

func (s *Store) ListNamespaces(now time.Time, ttl time.Duration) []api.NamespaceSnapshot {
	return aggregate.Namespaces(s.ListPods(now, ttl))
}

func (s *Store) Debug(now time.Time, ttl time.Duration) api.StoreDebug {
	shards := s.readShards(now, ttl)
	totalContainers := 0
	pods := make(map[digestKey]struct{})
	namespaces := make(map[string]struct{})
	hasher := identityBuffer{}
	visitScopedShards(shards, ReadScope{}, func(container api.ContainerSnapshot) {
		totalContainers++
		if key, ok := hasher.pod(container); ok {
			pods[key] = struct{}{}
		}
		if container.Namespace != "" {
			namespaces[container.Namespace] = struct{}{}
		}
	})

	return s.debugWithCounts(now, totalContainers, len(pods), len(namespaces))
}

func (s *Store) debugWithCounts(now time.Time, totalContainers, pods, namespaces int) api.StoreDebug {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history.prune(now)
	historySeries, historyPoints := s.history.stats()
	return api.StoreDebug{
		TotalContainers: totalContainers, StaleContainers: 0, NodeRecords: len(s.nodes),
		MaxNodes: s.limits.MaxNodes, MaxContainers: s.limits.MaxContainers,
		Pods: pods, Namespaces: namespaces, HistorySeries: historySeries, HistoryPoints: historyPoints,
	}
}

func (s *Store) LatestByNode(_ time.Time) map[string]time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	latest := map[string]time.Time{}
	for node, snapshot := range s.nodes {
		if node == "" || snapshot.capturedAt.IsZero() {
			continue
		}
		latest[node] = snapshot.capturedAt
	}
	return latest
}

func (s *Store) ListNodes(now time.Time, ttl time.Duration) []api.NodeSnapshotStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := make([]api.NodeSnapshotStatus, 0, len(s.nodes))
	for node, snapshot := range s.nodes {
		if node == "" || snapshot.capturedAt.IsZero() {
			continue
		}
		nodes = append(nodes, api.NodeSnapshotStatus{
			NodeName:       node,
			CapturedAt:     snapshot.capturedAt,
			ContainerCount: len(snapshot.containers),
			Stale:          isStale(snapshot.capturedAt, now, ttl),
			Environment:    snapshot.environment,
		})
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeName < nodes[j].NodeName
	})
	return nodes
}

func (s *Store) RecordIngestion(result string, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ingestionResults[result]++
	s.lastIngestionDuration = duration
}

func (s *Store) IngestionStats() api.CollectorIngestionStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make(map[string]uint64, len(s.ingestionResults))
	for result, count := range s.ingestionResults {
		results[result] = count
	}
	return api.CollectorIngestionStats{
		Results:             results,
		LastDurationSeconds: s.lastIngestionDuration.Seconds(),
	}
}

func isStale(capturedAt time.Time, now time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	if capturedAt.IsZero() {
		return true
	}
	return capturedAt.Add(ttl).Before(now)
}

func countPods(containers []api.ContainerSnapshot) int {
	pods := map[string]struct{}{}
	for _, container := range containers {
		if container.Namespace == "" || container.PodName == "" {
			continue
		}
		key := strings.Join([]string{container.Namespace, container.PodUID, container.PodName, container.NodeName}, "\x00")
		pods[key] = struct{}{}
	}
	return len(pods)
}

func countNamespaces(containers []api.ContainerSnapshot) int {
	namespaces := map[string]struct{}{}
	for _, container := range containers {
		if container.Namespace != "" {
			namespaces[container.Namespace] = struct{}{}
		}
	}
	return len(namespaces)
}

func sortContainers(items []api.ContainerSnapshot) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.PodName != right.PodName {
			return left.PodName < right.PodName
		}
		if left.ContainerName != right.ContainerName {
			return left.ContainerName < right.ContainerName
		}
		return left.ContainerID < right.ContainerID
	})
}
