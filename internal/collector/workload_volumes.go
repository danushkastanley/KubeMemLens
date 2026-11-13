package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

const MaxWorkloadVolumeContainers = 1024

// WorkloadVolumePods scans existing immutable shards once, selecting only the
// live Pod instances already checked by the workload authorisation boundary.
func (s *Store) WorkloadVolumePods(namespace string, expected map[string]string, now time.Time, ttl time.Duration) ([]api.PodSnapshot, error) {
	if namespace == "" || len(expected) > volumecontext.MaxWorkloadPods {
		return nil, ErrReadPageTooLarge
	}
	var containers []api.ContainerSnapshot
	visitScopedShards(s.readShards(now, ttl), ReadScope{Namespace: namespace}, func(container api.ContainerSnapshot) {
		// One record past the ceiling is enough to refuse the read below.
		if uid, selected := expected[container.PodName]; selected && container.PodUID == uid && len(containers) <= MaxWorkloadVolumeContainers {
			containers = append(containers, container)
		}
	})
	if len(containers) > MaxWorkloadVolumeContainers {
		return nil, ErrReadPageTooLarge
	}
	return aggregate.Pods(containers), nil
}
