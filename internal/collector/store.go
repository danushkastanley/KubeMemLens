package collector

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type Store struct {
	mu    sync.RWMutex
	items map[string]api.ContainerSnapshot
}

func NewStore() *Store {
	return &Store{items: map[string]api.ContainerSnapshot{}}
}

func (s *Store) UpsertSnapshot(snapshot api.AgentSnapshot) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, container := range snapshot.Containers {
		if container.CapturedAt.IsZero() {
			container.CapturedAt = snapshot.CapturedAt
		}
		if container.NodeName == "" {
			container.NodeName = snapshot.NodeName
		}
		s.items[containerKey(container)] = container
		count++
	}
	return count
}

func (s *Store) ListContainers(now time.Time, ttl time.Duration) []api.ContainerSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]api.ContainerSnapshot, 0, len(s.items))
	for _, item := range s.items {
		if isStale(item.CapturedAt, now, ttl) {
			continue
		}
		items = append(items, item)
	}
	sortContainers(items)
	return items
}

func (s *Store) ListPods(now time.Time, ttl time.Duration) []api.PodSnapshot {
	containers := s.ListContainers(now, ttl)
	byPod := map[string]*api.PodSnapshot{}
	order := []string{}

	for _, container := range containers {
		if container.Namespace == "" || container.PodName == "" {
			continue
		}

		key := strings.Join([]string{container.Namespace, container.PodUID, container.PodName, container.NodeName}, "\x00")
		pod, ok := byPod[key]
		if !ok {
			byPod[key] = &api.PodSnapshot{
				Namespace: container.Namespace,
				PodName:   container.PodName,
				PodUID:    container.PodUID,
				NodeName:  container.NodeName,
			}
			order = append(order, key)
			pod = byPod[key]
		}

		pod.Containers = append(pod.Containers, container)
		if container.CapturedAt.After(pod.CapturedAt) {
			pod.CapturedAt = container.CapturedAt
		}
	}

	pods := make([]api.PodSnapshot, 0, len(byPod))
	for _, key := range order {
		pod := *byPod[key]
		memories := make([]model.MemoryBreakdown, 0, len(pod.Containers))
		for _, container := range pod.Containers {
			memories = append(memories, container.Memory)
		}
		pod.Memory = model.SumMemory(pod.Namespace+"/"+pod.PodName, memories)
		sortContainers(pod.Containers)
		pods = append(pods, pod)
	}

	sort.Slice(pods, func(i, j int) bool {
		if pods[i].Namespace == pods[j].Namespace {
			return pods[i].PodName < pods[j].PodName
		}
		return pods[i].Namespace < pods[j].Namespace
	})
	return pods
}

func (s *Store) ListNamespaces(now time.Time, ttl time.Duration) []api.NamespaceSnapshot {
	pods := s.ListPods(now, ttl)
	byNamespace := map[string]*api.NamespaceSnapshot{}
	for _, pod := range pods {
		ns, ok := byNamespace[pod.Namespace]
		if !ok {
			byNamespace[pod.Namespace] = &api.NamespaceSnapshot{Namespace: pod.Namespace}
			ns = byNamespace[pod.Namespace]
		}

		ns.PodCount++
		ns.Memory = model.AddMemory(ns.Memory, pod.Memory)
		ns.Memory.Name = pod.Namespace
		if pod.CapturedAt.After(ns.CapturedAt) {
			ns.CapturedAt = pod.CapturedAt
		}
	}

	namespaces := make([]api.NamespaceSnapshot, 0, len(byNamespace))
	for _, namespace := range byNamespace {
		namespaces = append(namespaces, *namespace)
	}
	sort.Slice(namespaces, func(i, j int) bool {
		return namespaces[i].Namespace < namespaces[j].Namespace
	})
	return namespaces
}

func (s *Store) Debug(now time.Time, ttl time.Duration) api.StoreDebug {
	s.mu.RLock()
	total := len(s.items)
	stale := 0
	for _, item := range s.items {
		if isStale(item.CapturedAt, now, ttl) {
			stale++
		}
	}
	s.mu.RUnlock()

	return api.StoreDebug{
		TotalContainers: total,
		StaleContainers: stale,
		Pods:            len(s.ListPods(now, ttl)),
		Namespaces:      len(s.ListNamespaces(now, ttl)),
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

func containerKey(container api.ContainerSnapshot) string {
	parts := []string{
		container.Namespace,
		container.PodName,
		container.ContainerName,
		container.NodeName,
		container.ContainerID,
	}
	return strings.Join(parts, "\x00")
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
