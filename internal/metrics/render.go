package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type renderer struct {
	b strings.Builder
}

type memoryMetric struct {
	kind  string
	value uint64
}

type eventMetric struct {
	kind  string
	value uint64
}

func newRenderer() *renderer {
	return &renderer{}
}

func (r *renderer) String() string {
	r.b.WriteString("# EOF\n")
	return r.b.String()
}

func (r *renderer) render(source Source, now time.Time, ttl time.Duration, opts Options) {
	containers := source.ListContainers(now, ttl)
	pods := source.ListPods(now, ttl)
	namespaces := source.ListNamespaces(now, ttl)
	debug := source.Debug(now, ttl)

	r.helpType("kubememlens_collector_store_entities", "Current KubeMemLens collector store entity counts.", "gauge")
	for _, item := range []struct {
		kind  string
		value int
	}{
		{"containers", debug.TotalContainers},
		{"stale_containers", debug.StaleContainers},
		{"pods", debug.Pods},
		{"namespaces", debug.Namespaces},
		{"history_series", debug.HistorySeries},
		{"history_points", debug.HistoryPoints},
	} {
		r.sample("kubememlens_collector_store_entities", labels{"kind": item.kind}, float64(item.value))
	}

	r.renderAgentFreshness(source, now)
	r.renderCollectorIngestion(source)
	r.renderDroppedMetrics(len(pods), len(containers), opts)
	if opts.IncludeNamespaceMetrics {
		r.renderNamespaces(namespaces, opts)
	}
	if opts.IncludePodMetrics && len(pods) <= opts.MaxPods {
		r.renderPods(pods, opts)
	}
	if opts.IncludeContainerMetrics && len(containers) <= opts.MaxContainers {
		r.renderContainers(containers, opts)
	}
}

func (r *renderer) renderCollectorIngestion(source Source) {
	ingestionSource, ok := source.(interface {
		IngestionStats() api.CollectorIngestionStats
	})
	if !ok {
		return
	}
	stats := ingestionSource.IngestionStats()
	r.helpType("kubememlens_collector_ingestion_requests_total", "KubeMemLens collector snapshot ingestion requests by result.", "counter")
	results := make([]string, 0, len(stats.Results))
	for result := range stats.Results {
		results = append(results, result)
	}
	sort.Strings(results)
	for _, result := range results {
		r.sample("kubememlens_collector_ingestion_requests_total", labels{"result": result}, float64(stats.Results[result]))
	}
	r.helpType("kubememlens_collector_ingestion_last_duration_seconds", "Duration in seconds of the latest KubeMemLens collector snapshot ingestion request.", "gauge")
	r.sample("kubememlens_collector_ingestion_last_duration_seconds", nil, stats.LastDurationSeconds)
}

func (r *renderer) renderAgentFreshness(source Source, now time.Time) {
	latest := source.LatestByNode(now)
	nodes := make([]string, 0, len(latest))
	for node := range latest {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	r.helpType("kubememlens_collector_agent_snapshot_age_seconds", "Age in seconds of the newest KubeMemLens agent snapshot seen for a node.", "gauge")
	for _, node := range nodes {
		age := now.Sub(latest[node]).Seconds()
		if age < 0 {
			age = 0
		}
		r.sample("kubememlens_collector_agent_snapshot_age_seconds", labels{"node": node}, age)
	}

	r.helpType("kubememlens_collector_agent_last_seen_timestamp_seconds", "Unix timestamp of the newest KubeMemLens agent snapshot seen for a node.", "gauge")
	for _, node := range nodes {
		r.sample("kubememlens_collector_agent_last_seen_timestamp_seconds", labels{"node": node}, float64(latest[node].Unix()))
	}
}

func (r *renderer) renderDroppedMetrics(podCount int, containerCount int, opts Options) {
	r.helpType("kubememlens_metrics_dropped_entities", "KubeMemLens entities skipped by metrics cardinality guardrails.", "gauge")
	if !opts.IncludePodMetrics {
		r.sample("kubememlens_metrics_dropped_entities", labels{"level": "pod", "reason": "disabled"}, float64(podCount))
	} else if podCount > opts.MaxPods {
		r.sample("kubememlens_metrics_dropped_entities", labels{"level": "pod", "reason": "max_entities_exceeded"}, float64(podCount))
	}
	if !opts.IncludeContainerMetrics {
		r.sample("kubememlens_metrics_dropped_entities", labels{"level": "container", "reason": "disabled"}, float64(containerCount))
	} else if containerCount > opts.MaxContainers {
		r.sample("kubememlens_metrics_dropped_entities", labels{"level": "container", "reason": "max_entities_exceeded"}, float64(containerCount))
	}
}

func (r *renderer) renderNamespaces(items []api.NamespaceSnapshot, opts Options) {
	r.helpType("kubememlens_namespace_memory_bytes", "Kubernetes namespace memory breakdown from KubeMemLens.", "gauge")
	for _, item := range items {
		for _, metric := range memoryMetrics(item.Memory) {
			r.sample("kubememlens_namespace_memory_bytes", labels{"namespace": item.Namespace, "type": metric.kind}, float64(metric.value))
		}
	}
	if opts.IncludeEventMetrics {
		r.helpType("kubememlens_namespace_memory_events", "Latest-snapshot Kubernetes namespace memory pressure events from KubeMemLens. These are gauges because snapshots can reset.", "gauge")
		for _, item := range items {
			for _, metric := range eventMetrics(item.Memory) {
				r.sample("kubememlens_namespace_memory_events", labels{"namespace": item.Namespace, "event": metric.kind}, float64(metric.value))
			}
		}
	}
	if opts.IncludeDiagnosisMetrics {
		r.helpType("kubememlens_namespace_diagnosis", "Current KubeMemLens namespace memory diagnosis.", "gauge")
		for _, item := range items {
			r.sample("kubememlens_namespace_diagnosis", labels{"namespace": item.Namespace, "diagnosis": diagnosis(item.Memory)}, 1)
		}
	}
}

func (r *renderer) renderPods(items []api.PodSnapshot, opts Options) {
	r.helpType("kubememlens_pod_memory_bytes", "Kubernetes pod memory breakdown from KubeMemLens.", "gauge")
	for _, item := range items {
		for _, metric := range memoryMetrics(item.Memory) {
			r.sample("kubememlens_pod_memory_bytes", labels{"namespace": item.Namespace, "pod": item.PodName, "node": item.NodeName, "type": metric.kind}, float64(metric.value))
		}
	}
	if opts.IncludeEventMetrics {
		r.helpType("kubememlens_pod_memory_events", "Latest-snapshot Kubernetes pod memory pressure events from KubeMemLens. These are gauges because snapshots can reset.", "gauge")
		for _, item := range items {
			for _, metric := range eventMetrics(item.Memory) {
				r.sample("kubememlens_pod_memory_events", labels{"namespace": item.Namespace, "pod": item.PodName, "node": item.NodeName, "event": metric.kind}, float64(metric.value))
			}
		}
	}
	if opts.IncludeDiagnosisMetrics {
		r.helpType("kubememlens_pod_diagnosis", "Current KubeMemLens pod memory diagnosis.", "gauge")
		for _, item := range items {
			r.sample("kubememlens_pod_diagnosis", labels{"namespace": item.Namespace, "pod": item.PodName, "node": item.NodeName, "diagnosis": diagnosis(item.Memory)}, 1)
		}
	}
}

func (r *renderer) renderContainers(items []api.ContainerSnapshot, opts Options) {
	r.helpType("kubememlens_container_memory_bytes", "Kubernetes container memory breakdown from KubeMemLens.", "gauge")
	for _, item := range items {
		if item.Namespace == "" || item.PodName == "" || item.ContainerName == "" {
			continue
		}
		for _, metric := range memoryMetrics(item.Memory) {
			r.sample("kubememlens_container_memory_bytes", labels{"namespace": item.Namespace, "pod": item.PodName, "container": item.ContainerName, "node": item.NodeName, "type": metric.kind}, float64(metric.value))
		}
	}
	if opts.IncludeEventMetrics {
		r.helpType("kubememlens_container_memory_events", "Latest-snapshot Kubernetes container memory pressure events from KubeMemLens. These are gauges because snapshots can reset.", "gauge")
		for _, item := range items {
			if item.Namespace == "" || item.PodName == "" || item.ContainerName == "" {
				continue
			}
			for _, metric := range eventMetrics(item.Memory) {
				r.sample("kubememlens_container_memory_events", labels{"namespace": item.Namespace, "pod": item.PodName, "container": item.ContainerName, "node": item.NodeName, "event": metric.kind}, float64(metric.value))
			}
		}
	}
	if opts.IncludeDiagnosisMetrics {
		r.helpType("kubememlens_container_diagnosis", "Current KubeMemLens container memory diagnosis.", "gauge")
		for _, item := range items {
			if item.Namespace == "" || item.PodName == "" || item.ContainerName == "" {
				continue
			}
			r.sample("kubememlens_container_diagnosis", labels{"namespace": item.Namespace, "pod": item.PodName, "container": item.ContainerName, "node": item.NodeName, "diagnosis": diagnosis(item.Memory)}, 1)
		}
	}
}

func (r *renderer) helpType(name string, help string, typ string) {
	fmt.Fprintf(&r.b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(&r.b, "# TYPE %s %s\n", name, typ)
}

func (r *renderer) sample(name string, labels labels, value float64) {
	fmt.Fprintf(&r.b, "%s%s %s\n", name, labels.String(), formatNumber(value))
}

func memoryMetrics(memory model.MemoryBreakdown) []memoryMetric {
	items := []memoryMetric{
		{"total", memory.TotalBytes},
		{"anon", memory.AnonBytes},
		{"file_cache", memory.FileCacheBytes()},
		{"file", memory.FileBytes},
		{"active_file", memory.ActiveFileBytes},
		{"inactive_file", memory.InactiveFileBytes},
		{"shmem", memory.ShmemBytes},
		{"slab", memory.SlabBytes},
		{"slab_reclaimable", memory.SlabReclaimableBytes},
		{"slab_unreclaimable", memory.SlabUnreclaimableBytes},
		{"kernel_other", memory.KernelOtherBytes()},
		{"kernel", memory.KernelBytes},
		{"socket", memory.SocketBytes},
		{"page_tables", memory.PageTableBytes},
		{"file_mapped", memory.FileMappedBytes},
		{"anon_thp", memory.AnonTHPBytes},
		{"file_thp", memory.FileTHPBytes},
		{"shmem_thp", memory.ShmemTHPBytes},
		{"residual", memory.ResidualBytes()},
		{"dirty", memory.DirtyBytes},
		{"writeback", memory.WritebackBytes},
	}
	if memory.PeakKnown {
		items = append(items, memoryMetric{"peak", memory.PeakBytes})
	}
	if memory.MinKnown && !memory.MinUnlimited {
		items = append(items, memoryMetric{"min", memory.MinBytes})
	}
	if memory.LowKnown && !memory.LowUnlimited {
		items = append(items, memoryMetric{"low", memory.LowBytes})
	}
	if memory.HighKnown && !memory.HighUnlimited {
		items = append(items, memoryMetric{"high", memory.HighBytes})
	}
	if memory.MaxKnown && !memory.MaxUnlimited {
		items = append(items, memoryMetric{"max", memory.MaxBytes})
	}
	if memory.SwapCurrentKnown {
		items = append(items, memoryMetric{"swap_current", memory.SwapCurrentBytes})
	}
	if memory.SwapPeakKnown {
		items = append(items, memoryMetric{"swap_peak", memory.SwapPeakBytes})
	}
	if memory.SwapMaxKnown && !memory.SwapMaxUnlimited {
		items = append(items, memoryMetric{"swap_max", memory.SwapMaxBytes})
	}
	return items
}

func eventMetrics(memory model.MemoryBreakdown) []eventMetric {
	items := []eventMetric{
		{"oom", memory.OOMEvents},
		{"oom_kill", memory.OOMKillEvents},
		{"high", memory.HighEvents},
		{"max", memory.MaxEvents},
	}
	if memory.EventDeltasKnown {
		items = append(items,
			eventMetric{"oom_delta", memory.OOMEventsDelta},
			eventMetric{"oom_kill_delta", memory.OOMKillEventsDelta},
			eventMetric{"high_delta", memory.HighEventsDelta},
			eventMetric{"max_delta", memory.MaxEventsDelta},
		)
	}
	if memory.LocalEventsKnown {
		items = append(items,
			eventMetric{"local_oom", memory.LocalOOMEvents},
			eventMetric{"local_oom_kill", memory.LocalOOMKillEvents},
			eventMetric{"local_high", memory.LocalHighEvents},
			eventMetric{"local_max", memory.LocalMaxEvents},
		)
	}
	if memory.LocalEventDeltasKnown {
		items = append(items,
			eventMetric{"local_oom_delta", memory.LocalOOMEventsDelta},
			eventMetric{"local_oom_kill_delta", memory.LocalOOMKillEventsDelta},
			eventMetric{"local_high_delta", memory.LocalHighEventsDelta},
			eventMetric{"local_max_delta", memory.LocalMaxEventsDelta},
		)
	}
	if memory.SwapEventsKnown {
		items = append(items,
			eventMetric{"swap_high", memory.SwapHighEvents},
			eventMetric{"swap_max", memory.SwapMaxEvents},
			eventMetric{"swap_fail", memory.SwapFailEvents},
		)
	}
	if memory.SwapEventDeltasKnown {
		items = append(items,
			eventMetric{"swap_high_delta", memory.SwapHighEventsDelta},
			eventMetric{"swap_max_delta", memory.SwapMaxEventsDelta},
			eventMetric{"swap_fail_delta", memory.SwapFailEventsDelta},
		)
	}
	return items
}

func diagnosis(memory model.MemoryBreakdown) string {
	return string(explain.Analyze(memory).Diagnosis)
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

type labels map[string]string

func (l labels) String() string {
	if len(l) == 0 {
		return ""
	}
	keys := make([]string, 0, len(l))
	for key := range l {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+`="`+escapeLabelValue(l[key])+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}
