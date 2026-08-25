# Architecture

KubeMemLens is designed as a terminal-first tool that can start locally and grow into a Kubernetes-aware inspector without changing the core memory model.

## Current Components

- `kubectl-memlens`: CLI and future kubectl plugin entrypoint.
- `internal/cgroup`: cgroup v2 file parser.
- `internal/model`: memory breakdown and formatting helpers.
- `internal/explain`: diagnosis heuristics and incident-friendly explanations.
- `examples/cgroup-v2`: local sample data for development and tests.
- `memlens-agent`: DaemonSet binary that scans node cgroups and posts snapshots.
- `memlens-collector`: in-memory collector plus authenticated extension server for latest container snapshots and bounded Pod trends.
- `internal/client`: collector readers for direct HTTP and Kubernetes API service proxy modes.
- `internal/tui`: Bubble Tea dashboard for risk-oriented node, namespace, workload, Pod and container investigation.
- `internal/incident`: shared redacted incident-bundle writing used by the CLI and TUI.
- `internal/metrics`: Prometheus/OpenMetrics text renderer for conservative collector metrics.

## Cluster Flow

1. The agent runs on each node as a DaemonSet.
2. The agent walks `/host/sys/fs/cgroup`, parses composition, peak, boundaries, PSI, swap, local events, and reclaim signals, and returns only container-looking cgroups.
3. The agent performs one node-filtered Pod list, follows changes through a Kubernetes watch, and builds a local pod/container index.
4. The agent maps container cgroup IDs to namespace, Pod, container, node, resource request/limit, QoS, restart/termination, phase, creation-time, runtime class, memory-backed `emptyDir` counts/limits, direct owner, and top-level workload metadata. Its cached own-node GET adds MemoryPressure and allocatable memory. ReplicaSet and Job owner GETs are cached and bounded.
5. The agent uses its rotating, Pod-bound ServiceAccount token to read the collector epoch and create a node snapshot through the Kubernetes aggregated API.
6. The collector stores the latest container snapshots plus a bounded rolling Pod history in memory, deriving event and scan/steal/refault/major-fault deltas from consecutive container instances. It defaults to 5,000 node records, 100,000 current containers, and 16 MiB per encoded read response. Current clients request at most 500 records per keyset page; the server reduces a page further when its encoded body would reach the byte ceiling. Clients deterministically rebuild Pod, namespace and workload views, while legacy array endpoints remain during the pre-1.0 compatibility window. History defaults to 15 minutes, 180 points per Pod instance, 1,000 total series, and 20 returned instances per request.
7. The CLI and TUI query the read-only collector listener on port `8080` through one of two connection modes:
   - HTTP direct mode, usually via a user-managed port-forward.
   - Kubernetes API service proxy mode, which is the default when no collector URL is provided.
8. Prometheus can scrape collector `/metrics` for namespace and pod memory gauges. Container metrics are opt-in.

Pod and namespace totals are sums of mapped container cgroups only. Parent pod cgroups are intentionally not added to avoid double-counting.

The default data flow keeps the collector cluster-internal:

```text
agent -> Kubernetes API server -> authenticated APIService -> collector
CLI/TUI -> Kubernetes API service proxy -> collector read API
```

HTTP fallback mode still uses:

```text
agent -> collector service -> port-forward -> CLI/TUI
```

The alpha metrics flow is:

```text
agent -> collector store -> /metrics -> Prometheus
```

## Future Components

### Authenticated Kubernetes API migration

Agent ingestion now uses the aggregated Kubernetes API at `memory.kubememlens.io/v1alpha1`. Kubernetes authenticates the Pod-bound ServiceAccount token and applies RBAC. The extension server accepts request-header identity only from the validated aggregation proxy and delegates the exact resource request through `SubjectAccessReview` before mutating the store. PROD-004 moves reads to the same boundary.

Namespace Roles cover Pod, container, workload and history resources. Cluster views, workload-labelled metrics and agent writes use separate least-privilege bindings. Agent writes must match authenticated Pod UID, node name and node UID claims plus the current collector epoch and a strictly increasing sequence. Secure-profile charts disable legacy direct workload routes. See [ADR 0004](adr/0004-use-kubernetes-aggregation-for-authentication.md) and the [endpoint policy](security/authentication-and-authorisation.md).

### CLI / kubectl plugin

The CLI remains the primary interface. Once installed as `kubectl-memlens`, users will be able to run it through kubectl as:

```sh
kubectl memlens
```

### Terminal dashboard

The Bubble Tea TUI provides node, namespace, top-level workload, Pod, container and detail views. A reusable viewport owns selection and virtualised windows, while layout, risk presentation, filters, selected history and incident actions remain separate modules. Compact and standard terminals render one focused surface. At 150×30 and larger, the Pod view renders a master-detail memory dashboard with namespace and node context; Tab changes table/detail focus rather than changing entity view.

Snapshot views refresh concurrently within one timeout. Selected-Pod history has its own generation-keyed state: only one request is in flight for the selection, late responses are discarded, the last good series survives a refresh error and pause stops automatic updates without disabling manual refresh. Pod detail combines a bounded trend with cgroup limit, PSI/event, Kubernetes context, confidence and safe next commands. Container detail explicitly labels parent-Pod history because container-level history is not retained.

Incident actions call typed internal interfaces rather than spawning the CLI. Recommendations and comparisons are read-only; capture reuses `internal/incident` for redaction, atomic mode-`0600` writes and explicit overwrite confirmation. The alpha TUI reads through the shared client layer, so HTTP and Kubernetes API service-proxy modes retain the same behaviour. The v1 client will use Kubernetes discovery and the aggregated resources without widening the selected principal's server-authorised scope.

### Node-local agent

The agent runs as a DaemonSet. It reads cgroup memory files from the node, maps cgroup paths to containers through a node-filtered informer cache, and posts snapshots to the collector.

### Collector

The collector receives recent snapshots from node agents and makes them available to the CLI, TUI, and `/metrics`. Metrics are rendered from the same latest in-memory snapshots as the API, so pod and namespace totals stay consistent. A compact rolling history retains Pod composition, swap, PSI, peak, and event deltas for short incident review. Age, point, series, and response limits bound its memory and API cost; it is deliberately ephemeral and is not long-term monitoring storage.

### Kubernetes metadata mapper

The Kubernetes package maps container IDs from Pod status to cgroup paths and attaches context available from the same Pod object, including runtime class and bounded aggregate facts about memory-backed `emptyDir` volumes without collecting volume names. Continuous agent mode uses a filtered informer cache; one-shot mode performs one direct list before exiting. Each agent also caches a GET of its own Node every 30 seconds for MemoryPressure and allocatable memory. A separate bounded resolver follows ReplicaSet to Deployment and Job to CronJob with cached `get` calls; StatefulSet, DaemonSet, and other direct top-level owners need no extra lookup.

### Optional eBPF attribution

A future eBPF mode may attribute short-lived file and page-cache behaviour more precisely. It is deferred as a separately installed, on-demand extension because it changes the node and multi-tenant trust boundaries. See the [design gate](ebpf/OPTIONAL_EBPF_DESIGN.md), [threat model](security/KubeMemLens-threat-model.md), [benchmark protocol](ebpf/BENCHMARK_PROTOCOL.md), and [ADR 0001](adr/0001-defer-ebpf-until-security-and-benchmark-gates.md). The standard agent will not load BPF programmes.
