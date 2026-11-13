package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// shardView is one node's current containers as a reader sees them. The slice
// is the store's own: a node's containers are replaced as a whole and never
// written in place, so a reader can keep it after the lock is released without
// copying it. Freshness and completeness belong to the read, not the record,
// and are stamped onto each container as it is handed out.
type shardView struct {
	containers   []api.ContainerSnapshot
	freshness    api.EvidenceFreshness
	completeness api.EvidenceCompleteness
}

func (s *Store) readShards(now time.Time, ttl time.Duration) []shardView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	shards := make([]shardView, 0, len(s.nodes))
	for _, snapshot := range s.nodes {
		if len(snapshot.containers) > 0 {
			shards = append(shards, shardView{
				containers:   snapshot.containers,
				freshness:    freshness(snapshot.capturedAt, now, ttl),
				completeness: nodeCompleteness(snapshot),
			})
		}
	}
	return shards
}

// visitScopedShards hands each container in scope to visit by value, so the
// stored record is never exposed for writing.
func visitScopedShards(shards []shardView, scope ReadScope, visit func(api.ContainerSnapshot)) {
	for _, shard := range shards {
		for index := range shard.containers {
			if scope.Namespace != "" && shard.containers[index].Namespace != scope.Namespace {
				continue
			}
			container := shard.containers[index]
			container.Freshness, container.Completeness = shard.freshness, shard.completeness
			visit(container)
		}
	}
}
