package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
)

// ReadScope is fixed by the authorised Kubernetes resource path. An empty
// namespace is a cluster-scoped view and must only be supplied after a
// cluster-scoped SubjectAccessReview has allowed the request.
type ReadScope struct {
	Namespace string
}

func (s *Store) ListContainersScoped(scope ReadScope, now time.Time, ttl time.Duration) []api.ContainerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]api.ContainerSnapshot, 0)
	for node, snapshot := range s.nodes {
		if isStale(snapshot.capturedAt, now, ttl) {
			s.containerCount -= len(snapshot.containers)
			snapshot.containers = nil
			s.nodes[node] = snapshot
			continue
		}
		for _, container := range snapshot.containers {
			if scope.Namespace == "" || container.Namespace == scope.Namespace {
				items = append(items, container)
			}
		}
	}
	sortContainers(items)
	return items
}

func (s *Store) ListPodsScoped(scope ReadScope, now time.Time, ttl time.Duration) []api.PodSnapshot {
	return aggregate.Pods(s.ListContainersScoped(scope, now, ttl))
}

func (s *Store) ListNamespacesScoped(scope ReadScope, now time.Time, ttl time.Duration) []api.NamespaceSnapshot {
	return aggregate.Namespaces(s.ListPodsScoped(scope, now, ttl))
}

func (s *Store) ListWorkloadsScoped(scope ReadScope, now time.Time, ttl time.Duration) []api.WorkloadSnapshot {
	return aggregate.Workloads(s.ListPodsScoped(scope, now, ttl))
}

func (s *Store) GetPod(namespace, name string, now time.Time, ttl time.Duration) (api.PodSnapshot, bool) {
	containers := s.ListContainersScoped(ReadScope{Namespace: namespace}, now, ttl)
	matching := make([]api.ContainerSnapshot, 0)
	for _, container := range containers {
		if container.PodName == name {
			matching = append(matching, container)
		}
	}

	pods := aggregate.Pods(matching)
	if len(pods) == 0 {
		return api.PodSnapshot{}, false
	}
	latest := pods[0]
	for _, pod := range pods[1:] {
		if pod.CapturedAt.After(latest.CapturedAt) ||
			(pod.CapturedAt.Equal(latest.CapturedAt) && pod.PodUID < latest.PodUID) {
			latest = pod
		}
	}
	return latest, true
}

func (s *Store) GetNode(name string, now time.Time, ttl time.Duration) (api.NodeSnapshotStatus, bool) {
	for _, node := range s.ListNodes(now, ttl) {
		if node.NodeName == name {
			return node, true
		}
	}
	return api.NodeSnapshotStatus{}, false
}
