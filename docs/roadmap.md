# Roadmap

KubeMemLens explains Pod memory problems from kernel cgroup v2 evidence. The
current release is `v1.0.4`. This page lists what is planned next and why; later
releases extend the same evidence model to CPU throttling and node pressure.

Plans can change with feedback. Order matters more than dates, and a version
only ships when its evidence and release gates pass. To suggest a change, open a
[GitHub issue](https://github.com/danushkastanley/KubeMemLens/issues/new/choose).

## Principles for every release

- **No new install cost.** Each feature reuses the existing agent, cgroup v2
  data path and RBAC. A feature that needs more access must justify it in a
  reviewed design before it ships.
- **Read-only, always.** KubeMemLens never changes workloads or cluster state.
- **Diagnoses, not raw dumps.** Findings carry evidence, confidence, caveats
  and next checks.
- **One contract everywhere.** Each feature appears in the CLI and terminal
  dashboard and in versioned machine-readable output.
- **No telemetry and no outbound calls.** Nothing leaves your cluster or
  terminal.

## Planned releases

| Version | Focus | Why it matters |
| --- | --- | --- |
| `v1.1.0` | Stable versioned JSON for every user-facing command, published JSON Schemas, and a local MCP server (`kubectl memlens mcp`) that uses your own kubeconfig and RBAC | Scripts and AI agents can diagnose Pod memory with a few well-defined calls instead of many raw `kubectl` commands |
| `v1.2.0` | OOMKill and eviction post-mortems that keep bounded evidence after the Pod is gone | Most cgroup evidence disappears with the container unless someone captures it first |
| `v1.3.0` | CPU throttling diagnosis from cgroup `cpu.stat` and `cpu.max` | "Slow despite low CPU usage" is as common as an OOMKill and uses the same agent |
| `v1.4.0` | Node pressure from PSI, ranking the Pods that contribute most | Answers "which Pods are hurting this node?" without claiming more than the evidence shows |
| `v1.5.0` | Right-sizing estimates with cost, using prices you supply | Adds a budget reason alongside the incident reason; prices are never fetched |

Each planned release starts with a public design document and stays read-only.
Patch releases carry fixes and dependency updates between them.

## Already available

Some requests are already covered by `v1.0.4`:

- Machine-readable `explain`, `recommend`, `top`, `node`, `volumes` and
  `history trends` output; see the [explanation schema](explanation-schema.md).
  `history pod` prints text only until `v1.1.0`.
- A Prometheus/OpenMetrics resource with cardinality guardrails; see
  [metrics](metrics.md).
- Read-only `recommend pod|workload` with composition-aware findings.
- Recent OOMKilled termination evidence in `explain`, and incident capture
  with offline replay; see the [Pod OOM risk runbook](runbooks/pod-oom-risk.md).

## Not planned

- Logs, distributed tracing or network monitoring. Other tools do these well;
  KubeMemLens complements them.
- A web dashboard, hosted service or product telemetry.
- Automatic changes to requests, limits or workloads.

## Pre-1.0 milestones

These labels described capability sequencing before `v1.0.0`. They are not
published release tags.

### v0.1: Local Parser And Sample CLI

- Parse cgroup v2 memory files.
- Model RSS/anon, file cache, tmpfs, slab, dirty/writeback, and pressure events.
- Explain local sample data.
- Keep Kubernetes mode as honest placeholders.

### v0.2: Kubernetes Cgroup Mapping And Collector Snapshots

- Walk container cgroups on each node.
- Map container IDs to pod/container metadata.
- Post agent snapshots to an in-memory collector.
- Add collector-backed `top pods`, `top ns`, and `explain pod` commands.

### v0.3: Terminal Dashboard

- Add a Bubble Tea terminal dashboard.
- Browse namespaces, pods, and containers.
- Search and sort tables.
- Show pod detail and explanation view.
- Keep the interface terminal-first and incident-friendly.

### v0.4: Kubectl-Native Collector Connectivity

- Use the tenant-scoped Kubernetes aggregated API by default.
- Keep direct HTTP and Service-proxy code only for controlled pre-v1 rollback, outside the production chart.
- Add collector discovery flags for namespace, service, port, kubeconfig, and context.
- Add `status` for collector health and latest snapshot counts.
- Fail closed without a direct HTTP fallback when the authenticated API is unavailable.

### v0.5: Prometheus/OpenMetrics Export

- Expose recent memory bucket metrics through a separately authorised aggregated resource.
- Keep labels controlled to avoid cardinality surprises.
- Add cardinality guardrails for pod and container metrics.
- Use the separately authorised aggregated metrics resource as the only production chart scrape path.
- Document metric names, labels, Helm values, and PromQL examples.

### v0.6: Cgroup Mapping Hardening And Informer Cache

- Measure and optimise informer-backed Pod mapping and cgroup scan cost.
- Harden cgroup layout support across container runtimes and distros.
- Improve unmapped cgroup diagnostics without noisy logs.

### v0.7: Memory Pressure And Trend History In The TUI

- Add short rolling history from collector snapshots.
- Show pressure changes without adding long-term persistence.
- Keep trend views bounded and terminal-friendly.
- Add virtualised tables and detail views for compact, standard and wide terminals.
- Add first-class node/workload/Pod/container navigation, risk filtering and sorting.
- Add the wide observed-Pod-charge dashboard and read-only incident actions.
- Verify the minimum-size workflow and wide layout against a disposable 20-Pod kind cluster.

### v0.8: Optional eBPF File Attribution

- Qualify bounded file/page-cache tracing on compatible self-managed, GKE, EKS and AKS Linux nodes.
- Keep eBPF separately installed, on demand, and outside the standard agent/chart.
- Pass the documented multi-tenant threat model, provider matrix, benchmark thresholds, licence review, and independent security review before implementation.
- Fail preflight on restricted/serverless nodes rather than enabling a privileged fallback.

### v0.9: On-Demand Pod Tracing

- If the v0.8 gates pass, expose short-lived tracing for one authorised Pod/container.
- Keep raw paths behind explicit confirmation and out of metrics, logs, captures, and persistence.
- Enforce duration, event, output, CPU, memory, and concurrency limits.
