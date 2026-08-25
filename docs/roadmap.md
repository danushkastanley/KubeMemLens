# Roadmap

The milestone labels below describe capability sequencing. They are not published release tag versions; public pre-release tags use the `v<major>.<minor>.<patch>-alpha.<number>` format.

## v0.1: Local Parser And Sample CLI

- Parse cgroup v2 memory files.
- Model RSS/anon, file cache, tmpfs, slab, dirty/writeback, and pressure events.
- Explain local sample data.
- Keep Kubernetes mode as honest placeholders.

## v0.2: Kubernetes Cgroup Mapping And Collector Snapshots

- Walk container cgroups on each node.
- Map container IDs to pod/container metadata.
- Post agent snapshots to an in-memory collector.
- Add collector-backed `top pods`, `top ns`, and `explain pod` commands.

## v0.3: Terminal Dashboard

- Add a Bubble Tea terminal dashboard.
- Browse namespaces, pods, and containers.
- Search and sort tables.
- Show pod detail and explanation view.
- Keep the interface terminal-first and incident-friendly.

## v0.4: Kubectl-Native Collector Connectivity

- Use the tenant-scoped Kubernetes aggregated API by default.
- Retain direct HTTP and Service-proxy clients only for explicit legacy development mode.
- Add collector discovery flags for namespace, service, port, kubeconfig, and context.
- Add `status` for collector health and latest snapshot counts.
- Fail closed without a direct HTTP fallback when the authenticated API is unavailable.

## v0.5: Prometheus/OpenMetrics Export

- Expose recent memory bucket metrics through a separately authorised aggregated resource.
- Keep labels controlled to avoid cardinality surprises.
- Add cardinality guardrails for pod and container metrics.
- Retain direct ServiceMonitor support only for explicit legacy mode until an authenticated scraper is configured.
- Document metric names, labels, Helm values, and PromQL examples.

## v0.6: Cgroup Mapping Hardening And Informer Cache

- Measure and optimise informer-backed Pod mapping and cgroup scan cost.
- Harden cgroup layout support across container runtimes and distros.
- Improve unmapped cgroup diagnostics without noisy logs.

## v0.7: Memory Pressure And Trend History In The TUI

- Add short rolling history from collector snapshots.
- Show pressure changes without adding long-term persistence.
- Keep trend views bounded and terminal-friendly.
- Add virtualised tables and detail views for compact, standard and wide terminals.
- Add first-class node/workload/Pod/container navigation, risk filtering and sorting.
- Add the wide observed-Pod-charge dashboard and read-only incident actions.
- Verify the minimum-size workflow and wide layout against a disposable 20-Pod kind cluster.

## v0.8: Optional eBPF File Attribution

- Qualify bounded file/page-cache tracing on compatible self-managed, GKE, EKS and AKS Linux nodes.
- Keep eBPF separately installed, on demand, and outside the standard agent/chart.
- Pass the documented multi-tenant threat model, provider matrix, benchmark thresholds, licence review, and independent security review before implementation.
- Fail preflight on restricted/serverless nodes rather than enabling a privileged fallback.

## v0.9: On-Demand Pod Tracing

- If the v0.8 gates pass, expose short-lived tracing for one authorised Pod/container.
- Keep raw paths behind explicit confirmation and out of metrics, logs, captures, and persistence.
- Enforce duration, event, output, CPU, memory, and concurrency limits.
