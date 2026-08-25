package metrics

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func (r *renderer) renderCollectorReliability(source Source, debug api.DebugStore, containers []api.ContainerSnapshot, now time.Time) {
	reliability := debug.Reliability
	r.helpType("kubememlens_collector_state", "Current KubeMemLens collector reliability state.", "gauge")
	for _, state := range []api.CollectorState{
		api.CollectorRebuilding,
		api.CollectorReady,
		api.CollectorDegraded,
		api.CollectorStale,
		api.CollectorUnavailable,
	} {
		r.sample("kubememlens_collector_state", labels{"state": string(state)}, boolMetric(reliability.State == state))
	}

	r.helpType("kubememlens_collector_evidence_nodes", "Kubernetes nodes in the collector evidence set by freshness.", "gauge")
	r.sample("kubememlens_collector_evidence_nodes", labels{"freshness": "fresh"}, float64(reliability.FreshNodes))
	r.sample("kubememlens_collector_evidence_nodes", labels{"freshness": "stale"}, float64(reliability.StaleNodes))
	r.sample("kubememlens_collector_evidence_nodes", labels{"freshness": "missing"}, float64(reliability.MissingNodes))
	r.helpType("kubememlens_collector_expected_nodes", "Selected Kubernetes nodes expected to run the KubeMemLens agent.", "gauge")
	r.sample("kubememlens_collector_expected_nodes", nil, float64(reliability.ExpectedNodes))

	r.helpType("kubememlens_collector_evidence_completeness", "Current KubeMemLens collector evidence completeness.", "gauge")
	for _, completeness := range []api.EvidenceCompleteness{api.EvidenceComplete, api.EvidencePartial} {
		r.sample("kubememlens_collector_evidence_completeness", labels{"completeness": string(completeness)}, boolMetric(reliability.Completeness == completeness))
	}

	r.helpType("kubememlens_collector_user_visible_available", "Whether the collector currently holds user-visible evidence, including evidence marked stale.", "gauge")
	r.sample("kubememlens_collector_user_visible_available", nil, boolMetric(evidenceAvailable(reliability.State)))

	r.helpType("kubememlens_collector_snapshot_ttl_seconds", "Configured collector snapshot freshness window in seconds.", "gauge")
	r.sample("kubememlens_collector_snapshot_ttl_seconds", nil, float64(reliability.SnapshotTTLSeconds))
	r.renderCollectorTimestamps(reliability, now)
	r.renderCollectorHistory(reliability.History)
	r.renderCollectorMapping(source, containers)
}

func (r *renderer) renderCollectorTimestamps(reliability api.CollectorReliability, now time.Time) {
	r.helpType("kubememlens_collector_started_timestamp_seconds", "Unix timestamp when the current collector generation started.", "gauge")
	r.sample("kubememlens_collector_started_timestamp_seconds", nil, unixTimestamp(reliability.StartedAt))
	r.helpType("kubememlens_collector_state_transition_timestamp_seconds", "Unix timestamp of the latest collector reliability state transition.", "gauge")
	r.sample("kubememlens_collector_state_transition_timestamp_seconds", nil, unixTimestamp(reliability.TransitionedAt))
	r.helpType("kubememlens_collector_first_snapshot_timestamp_seconds", "Unix timestamp carried by the earliest snapshot accepted in this collector generation.", "gauge")
	r.sample("kubememlens_collector_first_snapshot_timestamp_seconds", nil, unixTimestamp(reliability.FirstSnapshotAt))
	r.helpType("kubememlens_collector_last_snapshot_timestamp_seconds", "Unix timestamp carried by the newest snapshot accepted in this collector generation.", "gauge")
	r.sample("kubememlens_collector_last_snapshot_timestamp_seconds", nil, unixTimestamp(reliability.LastSnapshotAt))
	r.helpType("kubememlens_collector_ingestion_last_received_timestamp_seconds", "Unix timestamp when the collector last accepted a new agent snapshot.", "gauge")
	r.sample("kubememlens_collector_ingestion_last_received_timestamp_seconds", nil, unixTimestamp(reliability.LastReceivedAt))
	r.helpType("kubememlens_collector_ingestion_last_received_age_seconds", "Age in seconds since the collector last accepted a new agent snapshot.", "gauge")
	r.sample("kubememlens_collector_ingestion_last_received_age_seconds", nil, timestampAge(reliability.LastReceivedAt, now))
	r.helpType("kubememlens_collector_node_inventory_updated_timestamp_seconds", "Unix timestamp of the latest successful selected-Node inventory refresh.", "gauge")
	r.sample("kubememlens_collector_node_inventory_updated_timestamp_seconds", nil, unixTimestamp(reliability.InventoryUpdatedAt))
}

func (r *renderer) renderCollectorHistory(history api.HistoryReliability) {
	r.helpType("kubememlens_collector_history_reset_timestamp_seconds", "Unix timestamp when in-memory collector history last reset.", "gauge")
	r.sample("kubememlens_collector_history_reset_timestamp_seconds", nil, unixTimestamp(history.ResetAt))
	r.helpType("kubememlens_collector_history_available_from_timestamp_seconds", "Unix timestamp of the oldest retained collector history point.", "gauge")
	r.sample("kubememlens_collector_history_available_from_timestamp_seconds", nil, unixTimestamp(history.AvailableFrom))
	r.helpType("kubememlens_collector_history_last_loss_timestamp_seconds", "Unix timestamp of the latest collector history capacity loss.", "gauge")
	r.sample("kubememlens_collector_history_last_loss_timestamp_seconds", nil, unixTimestamp(history.LastLossAt))
	r.helpType("kubememlens_collector_history_completeness", "Current in-memory collector history completeness.", "gauge")
	for _, completeness := range []api.EvidenceCompleteness{api.EvidenceComplete, api.EvidencePartial} {
		r.sample("kubememlens_collector_history_completeness", labels{"completeness": string(completeness)}, boolMetric(history.Completeness == completeness))
	}
	r.helpType("kubememlens_collector_history_loss_total", "Collector history data loss in the current process generation by reason.", "counter")
	r.sample("kubememlens_collector_history_loss_total", labels{"reason": "dropped_series"}, float64(history.DroppedSeries))
	r.sample("kubememlens_collector_history_loss_total", labels{"reason": "evicted_points"}, float64(history.EvictedPoints))
}

func (r *renderer) renderCollectorMapping(source Source, containers []api.ContainerSnapshot) {
	mapped, unmapped := mappingCounts(containers)
	if counts, ok := source.(interface{ MetricsMappingCounts() (int, int) }); ok {
		mapped, unmapped = counts.MetricsMappingCounts()
	}
	r.helpType("kubememlens_collector_mapping_containers", "Current workload container cgroups in collector evidence by mapping result.", "gauge")
	r.sample("kubememlens_collector_mapping_containers", labels{"result": "found"}, float64(mapped+unmapped))
	r.sample("kubememlens_collector_mapping_containers", labels{"result": "mapped"}, float64(mapped))
	r.sample("kubememlens_collector_mapping_containers", labels{"result": "unmapped"}, float64(unmapped))
}

func mappingCounts(containers []api.ContainerSnapshot) (mapped, unmapped int) {
	for _, container := range containers {
		if container.Namespace != "" && container.PodName != "" && container.ContainerName != "" {
			mapped++
		} else {
			unmapped++
		}
	}
	return mapped, unmapped
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func evidenceAvailable(state api.CollectorState) bool {
	switch state {
	case api.CollectorReady, api.CollectorDegraded, api.CollectorStale:
		return true
	default:
		return false
	}
}

func unixTimestamp(value time.Time) float64 {
	if value.IsZero() {
		return 0
	}
	return float64(value.Unix())
}

func timestampAge(value, now time.Time) float64 {
	if value.IsZero() {
		return 0
	}
	age := now.Sub(value).Seconds()
	if age < 0 {
		return 0
	}
	return age
}
