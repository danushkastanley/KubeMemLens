package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
)

func (s *Store) ListWorkloads(now time.Time, ttl time.Duration) []api.WorkloadSnapshot {
	return aggregate.Workloads(s.ListPods(now, ttl))
}
