package extension

import (
	runtimemetrics "runtime/metrics"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
)

func (h *ReadHandler) operationalMetrics() *metrics.Operational {
	usage := h.store.VolumeUsageStats()
	result := &metrics.Operational{UsageEnabled: h.volumeStatsEnabled,
		UsageEntries: usage.Entries, UsageBytes: usage.Bytes, UsageCommits: usage.Commits}
	samples := []runtimemetrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	runtimemetrics.Read(samples)
	if samples[0].Value.Kind() == runtimemetrics.KindUint64 {
		value := samples[0].Value.Uint64()
		result.HeapObjectsBytes = &value
	}
	if h.volumeResolver == nil {
		result.Health = &metrics.HealthOperational{}
		return result
	}
	if reader, ok := h.volumeResolver.(interface{ VolumeHealthStats() kube.VolumeHealthStats }); ok {
		s := reader.VolumeHealthStats()
		result.Health = &metrics.HealthOperational{Enabled: s.Enabled, Entries: s.Entries, Bytes: s.Bytes,
			PayloadWrites: s.PayloadWrites, Reads: s.Reads, Failures: s.Failures, Throttled: s.Throttled}
	}
	return result
}
