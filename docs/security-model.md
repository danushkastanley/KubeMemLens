# Security Model

KubeMemLens is privacy-first and local-first.

## Current candidate

The approved `v1.0.0-rc.1` candidate reads local cgroup sample files, node cgroup v2 memory files, Kubernetes pod/node metadata, and collector snapshots from the in-cluster collector. It does not send telemetry and does not include SaaS behaviour.

The legacy `v0.0.1-alpha.3` release is not suitable for shared multi-tenant clusters. The candidate authenticates node agents and operator reads through the Kubernetes aggregated API and enforces namespace or explicit cluster scope at the collector. Its local adversarial isolation result does not widen the exact provider and enforcing-CNI claims in the [support and compatibility contract](compatibility.md#multi-tenant-security-boundary).

The implemented v1 security interface validates the aggregation proxy, delegates an uncached exact `SubjectAccessReview`, filters namespace data before aggregation and binds writes to Pod and node identity. It is documented in [ADR 0004](adr/0004-use-kubernetes-aggregation-for-authentication.md), the [authentication and authorisation architecture](security/authentication-and-authorisation.md), the [tenant read runbook](runbooks/tenant-scoped-reads.md) and the [threat model](security/KubeMemLens-threat-model.md).

## Host Access

DaemonSet mode uses a read-only hostPath mount for `/sys/fs/cgroup`. This path is sensitive because it exposes node-level cgroup and container metadata. The chart keeps the root filesystem read-only by default and does not mount host `/proc`.

## Permissions

The candidate stays cgroup-read focused. It does not require privileged containers in its qualified profiles. The agent ServiceAccount needs `get`, `list`, and `watch` access for Pods across namespaces so it can map cgroups to Kubernetes metadata. It also has `get` on Nodes so each agent can read only its scheduled node's MemoryPressure condition; the client caches that GET for 30 seconds. Kubernetes RBAC cannot restrict a ClusterRole's `get` verb dynamically to the Pod's node name, so a compromised agent token could read another Node object. The agent code never lists or watches Nodes. The collector receives a separate projected token that can read the aggregation authentication ConfigMap, create SubjectAccessReviews and list core Nodes for expected-agent coverage. It cannot read Pods, Secrets or workload objects.

Top-level workload resolution adds `get` only for ReplicaSets and Jobs. The agent follows only direct owner references from Pods already scheduled on its node: ReplicaSet to Deployment and Job to CronJob. Successful lookups are cached for five minutes in a bounded 2,000-entry cache. It does not list or watch these workload resources. Kubernetes RBAC cannot constrain these `get` permissions to only names referenced by local Pods, so this is an explicit metadata-read trade-off surfaced by `doctor`.

The agent and collector use separate ServiceAccounts and explicitly projected rotating tokens. The collector token can read aggregation authentication configuration, submit SubjectAccessReviews and list Nodes. Production code retains only Linux Node names from that list; a compromised collector could read the full Node objects allowed by Kubernetes RBAC. It cannot read workload resources or Secrets. Both workloads run as non-root with the runtime-default seccomp profile, privilege escalation disabled, all Linux capabilities dropped, a read-only root filesystem by default, and explicit memory limits.

Pod watch data supplies requests, limits, QoS, restart/termination state, phase, creation time, runtime class, labels, memory-backed `emptyDir` counts/aggregate limits, and direct controller ownership to local explanations. Volume names are not collected. Label maps are capped at 64 entries and bounded again by snapshot request limits. They support local Kubernetes label selection but are not emitted as Prometheus labels or included in default redacted incident bundles. The cached own-node GET adds MemoryPressure and allocatable-memory context. Bounded owner GETs add the top-level workload, and `doctor` reports both Node and workload-owner permission failures.

The CLI and TUI use the user's kubeconfig to access virtual `memory.kubememlens.io` resources through the Kubernetes API server. Namespace RoleBindings grant only that namespace. All-namespace, node and cluster-status reads require the separate cluster-viewer role. Metrics require another explicit role. Direct HTTP and Service-proxy client code remains only for controlled pre-v1 rollback and is not enabled by the production chart.

The collector remains cluster-internal. KubeMemLens does not expose it through an external load balancer or create a port-forward automatically.

Authenticated reads, workload metrics and agent writes enter through the Kubernetes API server and TLS Service port `443`. The collector Pod retains a data-free health listener on port `8080`, but the secure Service exposes neither that port nor plaintext ingestion port `8081`. NetworkPolicy permits control-plane traffic to the extension port because managed control-plane source ranges are not portable; authentication and exact delegated authorisation remain the boundary if NetworkPolicy is absent.

The collector defaults to at most 5,000 node records, 100,000 current container snapshots, 1,000 history series, 500 identities per read page, 16 MiB of encoded JSON per response and four admitted authenticated reads. Keyset selection retains bounded identities instead of copying the full authorised view; Pod and workload nested evidence has a separate byte budget, and aggregate construction is serialised. Cluster status counts immutable shards without copying container records. The metrics view retains only aggregates permitted by both its entity limits and response budget, and reports dropped levels when the response budget is tighter. A capacity breach is rejected and counted rather than silently dropping or growing state. `doctor` reports storage bounds and warns near a ceiling. Operators should size them below the collector Pod's tested memory budget.

## Metrics Exposure

The aggregated metrics resource exposes namespace names, pod names, container names only when container metrics are enabled, node names, memory buckets, memory event counts, and KubeMemLens diagnoses.

Agent metrics contain only scan outcome, duration, mapping totals, post outcomes, and metadata-cache size. They do not contain workload or node identifiers, but their node-local aggregate counts can still reveal workload density. The chart therefore binds this endpoint to Pod-local loopback, does not advertise a metrics port and adds no scrape annotations. An explicit non-loopback CLI override is a reviewed local-development choice, not part of the shared-cluster profile.

The endpoint intentionally does not export pod UID, container ID, cgroup path, image, file path, owner references, or arbitrary Kubernetes labels in the v1 candidate. Container metrics are disabled by default to reduce metric cardinality.

Metrics require `get` on the cluster-scoped KubeMemLens metrics resource. The cluster-viewer role does not include that permission. The production chart exposes no direct collector `/metrics` route or `ServiceMonitor`.

## Telemetry

There is no telemetry by default and the CLI does not make external network calls. The authenticated metrics resource is local to the user's cluster. Any future export path should remain explicit and local to the user's infrastructure unless the project intentionally adds a separate hosted product.

## Collector Storage

The collector stores latest snapshots and bounded recent Pod history in memory only. It has no database or persistent long-term retention.

The [data and metadata exposure contract](compatibility.md#data-and-metadata-exposure) lists the identifiers present in ingestion, reads, metrics, logs and captures. A default redacted capture still contains Kubernetes display names and is not anonymous.

## Build and supply chain

CI verifies modules, runs `govulncheck`, reports Go statement coverage, scans committed configuration and secret patterns, builds the actual scratch runtime image, and fails on high or critical image vulnerabilities. Release tags repeat the runtime-image vulnerability scan before publishing the multi-architecture image.

The scanner itself is part of the threat model. Trivy `0.72.0` is invoked as an official container pinned by SHA-256 digest rather than through a mutable action tag. This follows Aqua Security's [2026 supply-chain incident guidance](https://github.com/aquasecurity/trivy/security/advisories/GHSA-69fq-xp46-6x23), which identifies digest-pinned images as unaffected. Scanner version or digest changes require normal dependency review.

## Operational Logs

Agent scan logs report a bounded failure reason and count rather than the raw cgroup error. This keeps cgroup paths, Pod UIDs and container IDs out of logs even when a filesystem read or parse fails.

## Incident Bundles

`capture` writes schema-versioned JSON with mode `0600` and refuses to replace an existing file unless `--force` is explicit. Bundles redact Pod UIDs, container IDs, cgroup paths, and bounded selector label maps by default; KubeMemLens does not collect images or file names. `--include-sensitive` is an explicit opt-in for local debugging. Recent history is opt-in and limited to 100 selected Pods per capture. `replay` makes no cluster connection, rejects unknown/trailing JSON, enforces a 64 MiB input limit and entity caps, and recomputes the explanation from the captured evidence.

## Future eBPF Mode

An eBPF attribution mode may require elevated capabilities and kernel helpers. It will not be added to the standard agent or default chart. The proposed separately installed profile, managed GKE/EKS/AKS boundary, multi-tenant controls and raw-path policy are documented in the [optional eBPF design](ebpf/OPTIONAL_EBPF_DESIGN.md), [threat model](security/KubeMemLens-threat-model.md), [benchmark protocol](ebpf/BENCHMARK_PROTOCOL.md), and [ADR 0001](adr/0001-defer-ebpf-until-security-and-benchmark-gates.md). Prototype measurements and an independent security review are required before implementation or a support claim.
