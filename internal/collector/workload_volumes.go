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
	for _, shard := range s.readShards(now, ttl) {
		for _, container := range shard {
			uid, selected := expected[container.PodName]
			if container.Namespace != namespace || !selected || container.PodUID != uid {
				continue
			}
			if len(containers) >= MaxWorkloadVolumeContainers {
				return nil, ErrReadPageTooLarge
			}
			containers = append(containers, container)
		}
	}
	return aggregate.Pods(containers), nil
}
