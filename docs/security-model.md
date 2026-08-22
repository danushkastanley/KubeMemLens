# Security Model

KubeMemLens is privacy-first and local-first.

## Current alpha

The current alpha reads local cgroup sample files, node cgroup v2 memory files, Kubernetes pod/node metadata, and collector snapshots from the in-cluster collector. It does not send telemetry and does not include SaaS behaviour.

## Host Access

DaemonSet mode uses a read-only hostPath mount for `/sys/fs/cgroup`. This path is sensitive because it exposes node-level cgroup and container metadata. The chart keeps the root filesystem read-only by default and does not mount host `/proc`.

## Permissions

The alpha is intended to stay cgroup-read focused. It should not require privileged containers unless a specific environment requires different host access. The agent ServiceAccount needs `get`, `list`, and `watch` access for Pods across namespaces so it can map cgroups to Kubernetes metadata. It also has `get` on Nodes so each agent can read only its scheduled node's MemoryPressure condition; the client caches that GET for 30 seconds. Kubernetes RBAC cannot restrict a ClusterRole's `get` verb dynamically to the Pod's node name, so a compromised agent token could read another Node object. The code never lists or watches Nodes, and the collector has no API token.

Top-level workload resolution adds `get` only for ReplicaSets and Jobs. The agent follows only direct owner references from Pods already scheduled on its node: ReplicaSet to Deployment and Job to CronJob. Successful lookups are cached for five minutes in a bounded 2,000-entry cache. It does not list or watch these workload resources. Kubernetes RBAC cannot constrain these `get` permissions to only names referenced by local Pods, so this is an explicit metadata-read trade-off surfaced by `doctor`.

The agent and collector use separate ServiceAccounts. The collector token is not mounted because the collector does not call the Kubernetes API. Both workloads run as non-root with the runtime-default seccomp profile, privilege escalation disabled, all Linux capabilities dropped, a read-only root filesystem by default, and explicit memory limits.

Pod watch data supplies requests, limits, QoS, restart/termination state, phase, creation time, runtime class, labels, memory-backed `emptyDir` counts/aggregate limits, and direct controller ownership to local explanations. Volume names are not collected. Label maps are capped at 64 entries and bounded again by snapshot request limits. They support local Kubernetes label selection but are not emitted as Prometheus labels or included in default redacted incident bundles. The cached own-node GET adds MemoryPressure and allocatable-memory context. Bounded owner GETs add the top-level workload, and `doctor` reports both Node and workload-owner permission failures.

CLI kube-proxy mode uses the user's Kubernetes credentials to access the collector service through the Kubernetes API server. That access is governed by Kubernetes RBAC. Users need `get` on `services` and `services/proxy` in the collector namespace, which is `kube-memlens` by default.

The collector remains cluster-internal. KubeMemLens does not expose the collector through an external load balancer, add collector auth, or create a port-forward automatically in the alpha release.

Collector reads, metrics, and health checks listen on port `8080`. Snapshot ingestion listens separately on port `8081`. The default NetworkPolicy permits read-only port access for Kubernetes API service proxy, port-forwarding, and Prometheus, but permits writable ingestion only from KubeMemLens agent Pods in the release namespace. This restriction requires a CNI that enforces Kubernetes NetworkPolicy.

The collector defaults to at most 5,000 node records, 100,000 current container snapshots, 1,000 history series, and 16 MiB of encoded JSON per read response. A capacity breach is rejected and counted rather than silently dropping or growing state. `doctor` reports these bounds and warns near a storage ceiling. Operators should size them below the collector Pod's tested memory budget.

## Metrics Exposure

The `/metrics` endpoint exposes namespace names, pod names, container names only when container metrics are enabled, node names, memory buckets, memory event counts, and KubeMemLens diagnoses.

Agent metrics contain only scan outcome, duration, mapping totals, post outcomes, and metadata-cache size. They do not contain workload or node identifiers.

The endpoint intentionally does not export pod UID, container ID, cgroup path, image, file path, owner references, or arbitrary Kubernetes labels in the alpha release. Container metrics are disabled by default to reduce metric cardinality.

Metrics are served by the collector service inside the cluster. Access depends on Kubernetes network policy, service exposure, Prometheus scrape configuration, and RBAC when using the Kubernetes API service proxy.

## Telemetry

There is no telemetry by default and the CLI does not make external network calls. The alpha metrics endpoint is local to the user's cluster. Any future export path should remain explicit and local to the user's infrastructure unless the project intentionally adds a separate hosted product.

## Collector Storage

The collector stores latest snapshots and bounded recent Pod history in memory only. It has no database or persistent long-term retention.

## Build and supply chain

CI verifies modules, runs `govulncheck`, reports Go statement coverage, scans committed configuration and secret patterns, builds the actual scratch runtime image, and fails on high or critical image vulnerabilities. Release tags repeat the runtime-image vulnerability scan before publishing the multi-architecture image.

The scanner itself is part of the threat model. Trivy `0.72.0` is invoked as an official container pinned by SHA-256 digest rather than through a mutable action tag. This follows Aqua Security's [2026 supply-chain incident guidance](https://github.com/aquasecurity/trivy/security/advisories/GHSA-69fq-xp46-6x23), which identifies digest-pinned images as unaffected. Scanner version or digest changes require normal dependency review.

## Incident Bundles

`capture` writes schema-versioned JSON with mode `0600` and refuses to replace an existing file unless `--force` is explicit. Bundles redact Pod UIDs, container IDs, cgroup paths, and bounded selector label maps by default; KubeMemLens does not collect images or file names. `--include-sensitive` is an explicit opt-in for local debugging. Recent history is opt-in and limited to 100 selected Pods per capture. `replay` makes no cluster connection, rejects unknown/trailing JSON, enforces a 64 MiB input limit and entity caps, and recomputes the explanation from the captured evidence.

## Future eBPF Mode

An eBPF attribution mode may require elevated capabilities and kernel helpers. It will not be added to the standard agent or default chart. The proposed separately installed profile, managed GKE/EKS/AKS boundary, multi-tenant controls and raw-path policy are documented in the [optional eBPF design](ebpf/OPTIONAL_EBPF_DESIGN.md), [threat model](security/KubeMemLens-threat-model.md), [benchmark protocol](ebpf/BENCHMARK_PROTOCOL.md), and [ADR 0001](adr/0001-defer-ebpf-until-security-and-benchmark-gates.md). Prototype measurements and an independent security review are required before implementation or a support claim.
