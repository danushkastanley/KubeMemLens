package metrics

import "fmt"

// Operational carries fixed-cardinality process/store measurements. The optional
// heap value is omitted when the runtime does not expose the requested metric.
type Operational struct {
	HeapObjectsBytes         *uint64
	UsageEnabled             bool
	UsageEntries, UsageBytes int
	UsageCommits             uint64
	Health                   *HealthOperational
}

type HealthOperational struct {
	Enabled                                   bool
	Entries, Bytes                            int
	PayloadWrites, Reads, Failures, Throttled uint64
}

func (r *renderer) operational(s Operational) {
	if s.HeapObjectsBytes != nil {
		r.operation("collector_heap_objects_bytes", "Collector Go heap objects, including objects not yet reclaimed by GC; not RSS or cgroup charge.", "gauge", *s.HeapObjectsBytes)
	}
	r.operation("volume_usage_enabled", "Whether volume usage ingestion is configured.", "gauge", enabledNumber(s.UsageEnabled))
	r.operation("volume_usage_entries", "Retained volume usage entries, without identities.", "gauge", s.UsageEntries)
	r.operation("volume_usage_retained_bytes", "Accounted current and historical volume usage bytes including indexes.", "gauge", s.UsageBytes)
	r.operation("volume_usage_commits_total", "Accepted in-memory volume entry replacements; not Kubernetes or disk writes.", "counter", s.UsageCommits)
	if s.Health == nil {
		return
	}
	h := s.Health
	r.operation("volume_health_enabled", "Whether the collector health cache is enabled; not a storage health state.", "gauge", enabledNumber(h.Enabled))
	r.operation("volume_health_entries", "Retained health cache entries, without identities.", "gauge", h.Entries)
	r.operation("volume_health_retained_bytes", "Accounted current and historical health bytes including indexes.", "gauge", h.Bytes)
	r.operation("volume_health_payload_writes_total", "Changed sanitised health payloads retained; unchanged observations do not increment this counter.", "counter", h.PayloadWrites)
	r.operation("volume_health_backend_reads_total", "Backend health acquisition attempts.", "counter", h.Reads)
	r.operation("volume_health_backend_failures_total", "Failed backend health acquisition attempts.", "counter", h.Failures)
	r.operation("volume_health_backend_throttled_total", "Backend health acquisition attempts rejected by bounded admission.", "counter", h.Throttled)
}

func (r *renderer) operation(suffix, help, kind string, value any) {
	name := "kubememlens_" + suffix
	fmt.Fprintf(&r.b, "# HELP %s %s\n# TYPE %s %s\n%s %v\n", name, help, name, kind, name, value)
}

func enabledNumber(value bool) int {
	if value {
		return 1
	}
	return 0
}
