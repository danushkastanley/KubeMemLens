# Metrics

KubeMemLens exposes Prometheus/OpenMetrics-compatible content through the aggregated `metrics/current` resource. The production chart requires a separate metrics-reader binding and does not expose a direct collector `/metrics` route or `ServiceMonitor`.

The [support and compatibility contract](compatibility.md#data-and-metadata-exposure) is the canonical source for metric metadata visibility, retention ownership and tenant-boundary requirements.

Each agent also exposes low-cardinality operational metrics at `/metrics` on loopback port `8082` by default. These metrics contain scan and mapping counts only; they do not contain namespace, Pod, container, cgroup or node identifiers.

The endpoint renders latest in-memory snapshots. Memory event values are gauges, not counters, because snapshots can reset when pods restart, agents restart, or collector state expires.

Collector reliability metrics describe user-visible evidence state, freshness, ingestion rejection and history loss. They follow the [reliability contract](reliability.md). Alert response must verify a newer collection timestamp, not only a green Pod.

## Defaults

- Namespace metrics: enabled.
- Pod metrics: enabled.
- Container metrics: disabled.
- Diagnosis metrics: enabled.
- Memory event metrics: enabled.
- Maximum pods before pod metrics are dropped: `2000`.
- Maximum containers before container metrics are dropped: `5000`.

Container metrics are disabled by default to avoid high-cardinality series in busy clusters.

## Labels

Namespace metrics use:

- `namespace`
- `type`, `event`, or `diagnosis`

Pod metrics use:

- `namespace`
- `pod`
- `node`
- `type`, `event`, or `diagnosis`

Container metrics use:

- `namespace`
- `pod`
- `container`
- `node`
- `type`, `event`, or `diagnosis`

KubeMemLens intentionally does not export pod UID, container ID, image, cgroup path, file path, owner references, or arbitrary Kubernetes labels in the alpha release.

## Collector Metrics

`kubememlens_collector_store_entities`

- Type: gauge.
- Labels: `kind`.
- Kind values: `containers`, `stale_containers`, `pods`, `namespaces`, `history_series`, `history_points`.

`kubememlens_collector_agent_snapshot_age_seconds`

- Type: gauge.
- Labels: `node`.
- Age of the newest agent snapshot seen for the node.

`kubememlens_collector_agent_last_seen_timestamp_seconds`

- Type: gauge.
- Labels: `node`.
- Unix timestamp of the newest agent snapshot seen for the node.

`kubememlens_collector_ingestion_requests_total`

- Type: counter.
- Labels: `result`.
- Result values include `accepted`, `unsupported_media_type`, `payload_too_large`, `invalid_json`, `invalid_snapshot`, `out_of_order`, and `store_error`.

`kubememlens_collector_ingestion_last_duration_seconds`

- Type: gauge.
- Duration of the latest snapshot ingestion request.

## Reliability metrics

| Metric | Type | Meaning |
| --- | --- | --- |
| `kubememlens_collector_state{state}` | gauge | One-hot state for `rebuilding`, `ready`, `degraded`, `stale` and `unavailable`. An unreachable metrics resource is absent rather than a reported unavailable sample. |
| `kubememlens_collector_evidence_nodes{freshness}` | gauge | Selected Nodes split into `fresh`, `stale` and `missing`. |
| `kubememlens_collector_expected_nodes` | gauge | Nodes matching the agent scheduling scope in the latest successful inventory. |
| `kubememlens_collector_evidence_completeness{completeness}` | gauge | One-hot `complete` or `partial` current evidence. |
| `kubememlens_collector_user_visible_available` | gauge | `1` when ready, degraded or stale evidence exists. `0` while rebuilding. |
| `kubememlens_collector_snapshot_ttl_seconds` | gauge | Configured freshness TTL. |
| `kubememlens_collector_started_timestamp_seconds` | gauge | Start of the current in-memory collector generation. |
| `kubememlens_collector_state_transition_timestamp_seconds` | gauge | Latest reliability state transition. |
| `kubememlens_collector_first_snapshot_timestamp_seconds` | gauge | Earliest source collection time accepted in this generation. |
| `kubememlens_collector_last_snapshot_timestamp_seconds` | gauge | Newest source collection time accepted in this generation. |
| `kubememlens_collector_ingestion_last_received_timestamp_seconds` | gauge | Collector receive time of the latest accepted snapshot. |
| `kubememlens_collector_ingestion_last_received_age_seconds` | gauge | Age since that collector receive time. |
| `kubememlens_collector_node_inventory_updated_timestamp_seconds` | gauge | Latest successful Linux Node inventory refresh. |
| `kubememlens_collector_history_reset_timestamp_seconds` | gauge | History reset time for the current generation. |
| `kubememlens_collector_history_available_from_timestamp_seconds` | gauge | Oldest retained history point. |
| `kubememlens_collector_history_last_loss_timestamp_seconds` | gauge | Latest series rejection or per-series point eviction in this generation. |
| `kubememlens_collector_history_completeness{completeness}` | gauge | One-hot `complete` or `partial` history state. |
| `kubememlens_collector_history_loss_total{reason}` | counter | Cumulative loss in this generation. Reasons are `dropped_series` and `evicted_points`. |
| `kubememlens_collector_mapping_containers{result}` | gauge | Current collector evidence counts for `found`, `mapped` and `unmapped` container cgroups. |

Source collection time and collector receive time are separate. Use the source time to judge evidence age. Use receive time to diagnose the ingestion path.

`kubememlens_metrics_dropped_entities`

- Type: gauge.
- Labels: `level`, `reason`.
- Level values: `pod`, `container`.
- Reason values: `disabled`, `max_entities_exceeded`.

## Agent Metrics

- `kubememlens_agent_scans_total{result="success|failure"}`
- `kubememlens_agent_snapshot_posts_total{result="success|failure"}`
- `kubememlens_agent_last_scan_timestamp_seconds`
- `kubememlens_agent_last_scan_duration_seconds`
- `kubememlens_agent_last_scan_containers{kind="found|mapped|unmapped"}`
- `kubememlens_agent_metadata_cache_pods`

The chart binds agent metrics to `127.0.0.1:8082` inside each agent Pod. It does not declare a metrics container port or add Prometheus scrape annotations, so another workload cannot scrape node-local density counts through the Pod network by default. Set `agent.metrics.enabled=false` to disable even this loopback diagnostics endpoint.

For an explicitly authorised local diagnostic, forward one agent Pod and inspect it locally:

```sh
agent_pod=$(kubectl get pod -n kube-memlens -l app.kubernetes.io/name=kube-memlens-agent -o jsonpath='{.items[0].metadata.name}')
kubectl port-forward -n kube-memlens "pod/${agent_pod}" 18082:8082
curl http://127.0.0.1:18082/metrics
```

Running the agent binary with an explicit non-loopback value such as `--metrics-listen=:8082` widens access and is intended only for a separately reviewed local environment. The metrics contain aggregate container, mapping and Pod-cache counts, so NetworkPolicy must not be their only confidentiality control.

The [local scale gate](scale-qualification.md) keeps this listener on loopback. It forwards to one agent at a time, records aggregate scan, scan-failure and post-failure counters, then closes the forward. Component working set, memory limit and interval cgroup CPU counters come from the local kind runtime observer. API-server `429` and `5xx` counters and Node MemoryPressure come from Kubernetes. None of these qualification sources changes the production scrape contract.

## Memory Metrics

`kubememlens_namespace_memory_bytes`

- Type: gauge.
- Labels: `namespace`, `type`.

`kubememlens_pod_memory_bytes`

- Type: gauge.
- Labels: `namespace`, `pod`, `node`, `type`.

`kubememlens_container_memory_bytes`

- Type: gauge.
- Labels: `namespace`, `pod`, `container`, `node`, `type`.
- Disabled by default.

Memory `type` values:

- `total`
- `anon`
- `file_cache`
- `file`
- `active_file`
- `inactive_file`
- `shmem`
- `slab`
- `slab_reclaimable`
- `slab_unreclaimable`
- `kernel_other`
- `kernel`
- `socket`
- `page_tables`
- `file_mapped`
- `anon_thp`
- `file_thp`
- `shmem_thp`
- `residual`
- `dirty`
- `writeback`
- `peak` when `memory.peak` is available
- `min`, `low`, `high`, and `max` when the corresponding boundary is finite
- `swap_current` and `swap_peak` when available
- `swap_max` when the boundary is finite

The mutually exclusive primary composition is `anon`, `file_cache`, `shmem`, and `residual`; those four values partition `total`, subject to zero-flooring when independently sampled cgroup files briefly disagree. `residual` includes kernel memory and every other charge outside the first three buckets.

Raw `file` includes shmem/tmpfs; `file_cache` is raw `file` minus `shmem`, floored at zero. Raw `kernel` includes slab; `kernel_other` is raw `kernel` minus `slab`, floored at zero. Active/inactive file, kernel, slab, socket, page-table, mapped-file, THP, dirty, and writeback metrics are overlapping secondary detail and must not be summed with the primary composition.

## Event Metrics

`kubememlens_namespace_memory_events`

- Type: gauge.
- Labels: `namespace`, `event`.

`kubememlens_pod_memory_events`

- Type: gauge.
- Labels: `namespace`, `pod`, `node`, `event`.

`kubememlens_container_memory_events`

- Type: gauge.
- Labels: `namespace`, `pod`, `container`, `node`, `event`.
- Disabled by default because container metrics are disabled by default.

Event values:

- `oom`
- `oom_kill`
- `high`
- `max`
- `oom_delta`
- `oom_kill_delta`
- `high_delta`
- `max_delta`
- `local_oom`, `local_oom_kill`, `local_high`, and `local_max`
- local-event `_delta` variants
- `swap_high`, `swap_max`, and `swap_fail`
- swap-event `_delta` variants

Raw values are the cumulative cgroup counters from the latest snapshot. Delta values compare the latest two snapshots for the same container ID. The first observation establishes a baseline and reports zero deltas, so historical events do not keep an `oom-risk` diagnosis active forever. Diagnoses prefer `memory.events.local` when the kernel exposes it, while retaining hierarchical counters for inspection.

## Diagnosis Metrics

`kubememlens_namespace_diagnosis`

- Type: gauge.
- Labels: `namespace`, `diagnosis`.
- Value is `1` for the current diagnosis.

`kubememlens_pod_diagnosis`

- Type: gauge.
- Labels: `namespace`, `pod`, `node`, `diagnosis`.
- Value is `1` for the current diagnosis.

`kubememlens_container_diagnosis`

- Type: gauge.
- Labels: `namespace`, `pod`, `container`, `node`, `diagnosis`.
- Disabled by default because container metrics are disabled by default.

Diagnosis values:

- `cache-heavy`
- `rss-heavy`
- `tmpfs-heavy`
- `dirty-writeback-heavy`
- `slab-heavy`
- `oom-risk`
- `memory-pressure`
- `limit-risk`
- `mixed`
- `normal`

## Helm Values

```yaml
metrics:
  enabled: true
  includeNamespaces: true
  includePods: true
  includeContainers: false
  includeDiagnosis: true
  includeEvents: true
  maxPods: 2000
  maxContainers: 5000
  prometheusRule:
    enabled: false
    labels: {}
    runbookBaseURL: https://github.com/danushkastanley/kube-memlens/blob/main/docs/runbooks
  grafanaDashboard:
    enabled: false
    labels:
      grafana_dashboard: "1"
```

The optional `PrometheusRule` includes `KubeMemLensCollectorUnavailable`, `KubeMemLensCollectorRecoveryDelayed`, `KubeMemLensCollectorEvidenceDegraded`, `KubeMemLensIngestionRejectionsHigh` and `KubeMemLensHistoryLoss`. It also retains stale-agent, mapping-coverage and Pod memory alerts. Reliability alerts link to the [collector reliability runbook](runbooks/reliability.md). The optional dashboard ConfigMap contains a small four-panel dashboard for top Pods, selected-Pod composition, diagnosis state, and agent freshness. Both resources require the operator's existing Prometheus/Grafana discovery setup and are disabled by default.

`maxPods` and `maxContainers` are upper bounds. The collector also derives a conservative entity ceiling from `collector.maxResponseBytes`; a tighter response budget takes precedence. Namespace, Pod or container series that exceed these bounds are omitted and counted by `kubememlens_metrics_dropped_entities`. Store counts, agent freshness and ingestion outcomes remain available without materialising the full container view.

Enable container metrics:

```sh
helm upgrade --install kube-memlens ./charts/kube-memlens \
  -n kube-memlens \
  --set metrics.includeContainers=true
```

Disable pod metrics:

```sh
helm upgrade --install kube-memlens ./charts/kube-memlens \
  -n kube-memlens \
  --set metrics.includePods=false
```

## PromQL Examples

Top namespaces by total memory:

```promql
topk(10, sum by (namespace) (kubememlens_namespace_memory_bytes{type="total"}))
```

Namespaces where file cache dominates:

```promql
(
  kubememlens_namespace_memory_bytes{type="file_cache"}
  /
  kubememlens_namespace_memory_bytes{type="total"}
) > 0.4
```

Top pods by RSS/anon:

```promql
topk(20, kubememlens_pod_memory_bytes{type="anon"})
```

Top pods by file cache:

```promql
topk(20, kubememlens_pod_memory_bytes{type="file_cache"})
```

Pods likely cache-heavy:

```promql
kubememlens_pod_diagnosis{diagnosis="cache-heavy"} == 1
```

Pods with OOM-risk diagnosis:

```promql
kubememlens_pod_diagnosis{diagnosis="oom-risk"} == 1
```

## Smoke Test

```sh
kubectl get --raw '/apis/memory.kubememlens.io/v1alpha1/metrics/current' | jq -r '.content' | head -40
```
