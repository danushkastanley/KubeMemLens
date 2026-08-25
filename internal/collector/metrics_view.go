package collector

import (
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

const (
	metricsResponseReserve = 1 << 20
	metricsEntityBudget    = 8 << 10
)

// MetricsView contains only the aggregates that can be rendered within the
// configured entity and response budgets. It implements metrics.Source without
// calling the Store's materialising legacy list methods.
type MetricsView struct {
	containers     []api.ContainerSnapshot
	pods           []api.PodSnapshot
	namespaces     []api.NamespaceSnapshot
	debug          api.DebugStore
	latest         map[string]time.Time
	ingestion      api.CollectorIngestionStats
	containerCount int
	podCount       int
	namespaceCount int
}

func (s *Store) BuildMetricsView(now time.Time, ttl time.Duration, opts metrics.Options, maxResponseBytes int) (*MetricsView, metrics.Options) {
	entityLimit := metricsEntityLimit(maxResponseBytes)
	effective := opts
	effective.MaxPods = minPositive(opts.MaxPods, entityLimit)
	effective.MaxContainers = minPositive(opts.MaxContainers, entityLimit)

	shards := s.readShards(now, ttl)
	podSeen := make(map[digestKey]struct{})
	namespaceSeen := make(map[string]struct{})
	podItems := make(map[digestKey]*api.PodSnapshot)
	namespaceItems := make(map[string]*api.NamespaceSnapshot)
	containers := make([]api.ContainerSnapshot, 0)
	hasher := identityBuffer{}
	podsExceeded := !effective.IncludePodMetrics
	namespacesExceeded := !effective.IncludeNamespaceMetrics
	containersExceeded := !effective.IncludeContainerMetrics
	totalContainers := 0

	visitScopedShards(shards, ReadScope{}, func(container api.ContainerSnapshot) {
		totalContainers++
		if !containersExceeded {
			if totalContainers > effective.MaxContainers {
				containers = nil
				containersExceeded = true
			} else {
				containers = append(containers, container)
			}
		}

		if container.Namespace != "" {
			if _, exists := namespaceSeen[container.Namespace]; !exists {
				namespaceSeen[container.Namespace] = struct{}{}
				if !namespacesExceeded {
					if len(namespaceSeen) > entityLimit {
						namespaceItems = nil
						namespacesExceeded = true
					} else {
						namespaceItems[container.Namespace] = &api.NamespaceSnapshot{Namespace: container.Namespace}
					}
				}
			}
			if !namespacesExceeded {
				namespace := namespaceItems[container.Namespace]
				namespace.Memory = model.AddMemory(namespace.Memory, container.Memory)
				namespace.Memory.Name = container.Namespace
				if container.CapturedAt.After(namespace.CapturedAt) {
					namespace.CapturedAt = container.CapturedAt
				}
			}
		}

		key, validPod := hasher.pod(container)
		if !validPod {
			return
		}
		if _, exists := podSeen[key]; !exists {
			podSeen[key] = struct{}{}
			if !podsExceeded {
				if len(podSeen) > effective.MaxPods {
					podItems = nil
					podsExceeded = true
				} else {
					podItems[key] = &api.PodSnapshot{
						Namespace: container.Namespace, PodName: container.PodName,
						PodUID: container.PodUID, NodeName: container.NodeName,
					}
				}
			}
			if !namespacesExceeded {
				namespaceItems[container.Namespace].PodCount++
			}
		}
		if !podsExceeded {
			pod := podItems[key]
			pod.Memory = model.AddMemory(pod.Memory, container.Memory)
			pod.Memory.Name = pod.Namespace + "/" + pod.PodName
			if container.CapturedAt.After(pod.CapturedAt) {
				pod.CapturedAt = container.CapturedAt
			}
		}
	})

	view := &MetricsView{
		containers: containers, pods: sortedMetricPods(podItems), namespaces: sortedMetricNamespaces(namespaceItems),
		debug:  s.debugWithCounts(now, totalContainers, len(podSeen), len(namespaceSeen)),
		latest: s.LatestByNode(now), ingestion: s.IngestionStats(),
		containerCount: totalContainers, podCount: len(podSeen), namespaceCount: len(namespaceSeen),
	}
	return view, effective
}

func metricsEntityLimit(maxResponseBytes int) int {
	usable := maxResponseBytes - metricsResponseReserve
	if usable < metricsEntityBudget {
		return 1
	}
	return usable / metricsEntityBudget
}

func minPositive(configured, ceiling int) int {
	if configured < ceiling {
		return configured
	}
	return ceiling
}

func sortedMetricPods(items map[digestKey]*api.PodSnapshot) []api.PodSnapshot {
	pods := make([]api.PodSnapshot, 0, len(items))
	for _, pod := range items {
		pods = append(pods, *pod)
	}
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].Namespace == pods[j].Namespace {
			return pods[i].PodName < pods[j].PodName
		}
		return pods[i].Namespace < pods[j].Namespace
	})
	return pods
}

func sortedMetricNamespaces(items map[string]*api.NamespaceSnapshot) []api.NamespaceSnapshot {
	namespaces := make([]api.NamespaceSnapshot, 0, len(items))
	for _, namespace := range items {
		namespaces = append(namespaces, *namespace)
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Namespace < namespaces[j].Namespace })
	return namespaces
}

func (v *MetricsView) ListContainers(time.Time, time.Duration) []api.ContainerSnapshot {
	return v.containers
}

func (v *MetricsView) ListPods(time.Time, time.Duration) []api.PodSnapshot {
	return v.pods
}

func (v *MetricsView) ListNamespaces(time.Time, time.Duration) []api.NamespaceSnapshot {
	return v.namespaces
}

func (v *MetricsView) Debug(time.Time, time.Duration) api.DebugStore {
	return v.debug
}

func (v *MetricsView) LatestByNode(time.Time) map[string]time.Time {
	return v.latest
}

func (v *MetricsView) IngestionStats() api.CollectorIngestionStats {
	return v.ingestion
}

func (v *MetricsView) MetricsEntityCounts() (int, int, int) {
	return v.namespaceCount, v.podCount, v.containerCount
}
