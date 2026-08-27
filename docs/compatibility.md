# Support and compatibility contract

This is the canonical support contract for KubeMemLens. Other documents link here instead of defining their own provider, runtime, availability or retention promises.

KubeMemLens is currently alpha software. The published alpha is for evaluation on disposable or explicitly authorised clusters. Provider support below is limited to the immutable artefacts and environment versions in the one-time reviewed evidence; it is not a promise that every later provider or KubeMemLens version has been rerun.

## Status terms

| Status | Meaning |
| --- | --- |
| Implemented | The code path exists and has focused automated tests. |
| Locally verified | A repeatable kind or local runtime test passed. This does not prove a managed-provider profile. |
| Qualified | A reviewed, sanitised live result passed for the exact recorded artefacts and environment. |
| Qualification required | The profile is part of the intended v1 contract but cannot be claimed until its named ticket records a passing result. |
| Unsupported | The current deep-mode contract rejects or excludes the profile. This may be reconsidered after an architecture or provider-capability change and fresh evidence. |
| Deferred | The capability belongs to a later release and is not part of v1. |

## v1 profile matrix

| Profile | v1 contract | Current evidence | Evidence owner | Status |
| --- | --- | --- | --- | --- |
| Kubernetes API | The three most recent upstream-maintained minor releases on the release decision date. On 25 August 2026 these are 1.34, 1.35 and 1.36. | Automated kind lifecycle tests cover all three current minors. Provider evidence records, but does not extend, the exact Kubernetes versions it exercised. | PROD-008 and automated compatibility CI | Locally verified |
| Deep-mode node base | Linux, cgroup v2, a readable `/sys/fs/cgroup`, DaemonSet scheduling and an enforcing NetworkPolicy-capable CNI. | Parser, walker and chart tests plus the [reviewed provider/runtime matrix](qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/README.md). | PROD-008 | Qualified |
| GKE Standard | COS with containerd and Ubuntu with containerd on amd64 Linux nodes. Support is limited to the exact recorded GKE, image, kernel, runtime and CNI versions. | Reviewed [COS](qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/gke-cos-containerd-amd64/provider-qualification.json) and [Ubuntu](qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/gke-ubuntu-containerd-amd64/provider-qualification.json) lifecycle bundles. | PROD-008 | Qualified |
| EKS managed nodes | AL2023 with containerd on amd64 managed-node-group Linux nodes. Bottlerocket is not claimed. | Reviewed [AL2023 lifecycle bundle](qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/eks-al2023-containerd-amd64/provider-qualification.json). | PROD-008 | Qualified |
| AKS node pools | Standard Ubuntu/containerd node pools are not supported by the recorded candidate. Azure Linux is also unclaimed. | The reviewed [AKS result](qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/aks-ubuntu-containerd-amd64/provider-qualification.json) confirms that AKS supplied an empty request-header allowed-name list while the candidate required a named aggregation proxy and failed closed. | PROD-008 | Unsupported |
| Self-managed containerd | Linux cgroup v2 on amd64 with containerd and the documented mount, scheduling and NetworkPolicy prerequisites. Support is limited to the recorded distribution, kernel and runtime. | Reviewed [containerd lifecycle bundle](qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/self-managed-containerd/provider-qualification.json). | PROD-008 | Qualified |
| Self-managed CRI-O | One real amd64 Linux cgroup v2 CRI-O distribution. Recognising a CRI-O cgroup path in a fixture is not qualification. | Reviewed [CRI-O lifecycle bundle](qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/self-managed-crio-amd64/provider-qualification.json). | PROD-008 | Qualified |
| Shared multi-tenant clusters | Supported only when authenticated agent writes, tenant-scoped reads and adversarial isolation tests pass. | The authenticated boundary and PROD-005 adversarial suite passed on local kind `v1.35.5`, including NetworkPolicy removal. The published alpha does not have this boundary. | PROD-002 to PROD-005; PROD-008 qualifies exact providers and CNIs. | Locally verified |
| Collector availability | One best-effort, single-replica, in-memory collector with visible restart, partial, stale and history-loss states. | Automated state, retry, readiness and shutdown tests plus the local kind `v1.35.5` failure harness: API recovery 9s, stale transition 29s, agent recovery 7s, partial-rollout detection 4s, removed-Node recovery 15s and graceful shutdown 5s. Provider timing remains unqualified. | PROD-006 | Locally verified |
| Live scale | Only the live container, node and refresh profile measured and published for the release candidate. Configured store ceilings are rejection bounds, not scale claims. | The [local `rc-5000` record](qualification-results/rc-5000-local-kind-2026-08-26.md) passed with 5,000 containers for 30 minutes on four-Node kind `v1.35.5`. Provider qualification does not turn this local density into a managed-provider scale claim. | PROD-007 and the [scale gate](scale-qualification.md) | Locally verified |
| Terminal UI | Apple Terminal, Ghostty, iTerm2 and Warp on macOS; xterm and at least two recorded modern Linux terminals; SSH and tmux at 80x24, 120x30 and 180x50. | State, render, race and local PTY tests. | PROD-009 | Qualification required |
| Release artefacts | Signed CLI archives, a non-root multi-architecture Linux image and an OCI Helm chart, all tied to one immutable release identity. | Alpha.3 consumer verification and CI supply-chain checks. | PROD-010 and PROD-012 | Qualification required |

CLI archives exist for Darwin, Linux and Windows on amd64 and arm64. The release workflow checks their construction, checksums and SBOMs. Archive availability does not prove every operating system and terminal combination. PROD-009 owns the interactive client claim. The deep-mode agent remains Linux-only.

[Kubernetes upstream maintains the latest three minor release branches](https://kubernetes.io/releases/). A new minor enters the general KubeMemLens API contract only after the automated kind matrix passes. Provider claims remain limited to their recorded Kubernetes versions unless a separate, explicitly approved requalification widens them. The oldest minor leaves at the next KubeMemLens release after upstream stops maintaining it. Historical provider evidence never extends an upstream end-of-life date.

Kubernetes 1.37 is still a release candidate on 25 August 2026. It is not part of the current support window. The 1.37 ticket family must verify the GA changelog and move the matrix before any 1.37 support claim appears.

## Unsupported and deferred profiles

| Environment or capability | v1 position | Reason |
| --- | --- | --- |
| GKE Autopilot | Unsupported for deep mode | Autopilot does not allow the required read-only `/sys/fs/cgroup` hostPath. |
| EKS Fargate | Unsupported for deep mode | Fargate does not run DaemonSets. |
| AKS standard node pools | Unsupported for the recorded candidate | AKS reported no request-header proxy client-name constraint. The candidate deliberately requires a named aggregation proxy and fails closed rather than trusting forwarded identity under a wider CA-only rule. |
| AKS virtual nodes | Unsupported for deep mode | DaemonSets do not schedule to virtual nodes and NetworkPolicy support is limited. |
| Windows worker nodes | Unsupported for deep mode | The agent and cgroup reader are Linux cgroup v2 components. |
| cgroup v1 | Unsupported | KubeMemLens has no cgroup v1 parser or deployment path. |
| Provider modes without an enforcing CNI | Unsupported for v1 deep mode | The standard profile requires NetworkPolicy enforcement, although NetworkPolicy is not tenant authorisation. |
| High-availability collector or durable history | Deferred | v1 deliberately uses one in-memory collector. |
| Restricted or agentless mode | Deferred until after the deep-mode v1 boundary | It has a separate data and completeness contract. |
| eBPF tracing, process inspection and path telemetry | Deferred | These capabilities require separate packaging, admission, privacy, benchmark and security gates. |
| Automatic remediation or workload mutation | Unsupported | KubeMemLens is read-only. |

The provider restrictions above are sourced and exercised by the [qualification runbook](qualification.md). They describe the current deep-mode candidate, not an irreversible promise about future architectures. The project will not add a privileged or weaker-authentication workaround merely to turn a restricted profile into deep mode.

## Multi-tenant security boundary

Shared multi-tenant clusters are a mandatory v1 threat environment. They are not supported by the current alpha.

The published alpha separates read and ingestion ports but does not provide the complete boundary. Current `main` routes production reads and writes through the Kubernetes aggregated API, validates forwarded identity, delegates exact authorisation and filters namespace data before aggregation. The production chart exposes only TLS port `443`; it has no legacy direct workload-read or collector-metrics Service path.

Before v1 can claim shared-cluster support:

- every collector read must have an authenticated principal and a server-side namespace or cluster-scope decision;
- every snapshot write must authenticate the agent, bind it to the expected node and reject replay or forged-node data;
- direct HTTP, service proxy, history, comparison, capture and metrics paths must preserve the same boundary; and
- PROD-005 must prove isolation with NetworkPolicy present and removed.

TUI filters, namespace selection, Kubernetes service-proxy permission and NetworkPolicy reachability are not substitutes for application authorisation.

The implemented interface is defined in the [authentication and authorisation architecture](security/authentication-and-authorisation.md), [ADR 0004](adr/0004-use-kubernetes-aggregation-for-authentication.md), the [tenant read runbook](runbooks/tenant-scoped-reads.md) and the [PROD-005 validation record](security/tenant-isolation-validation.md). A new or widened provider-specific shared-cluster claim requires reviewed provider and enforcing-CNI evidence.

## Availability and history

The v1 collector contract is best effort:

- exactly one collector replica holds independent in-memory state;
- there is no database, persistent volume, replica consensus or high-availability promise;
- a collector restart or upgrade loses current snapshots, event-delta baselines and recent history;
- agents repopulate current state on their next successful post;
- the API and TUI must report rebuilding, partial, stale and unavailable states instead of presenting missing evidence as healthy; and
- PROD-006 owns recovery timing, readiness and failure-state evidence.

The [reliability and availability contract](reliability.md) defines the probes, states, retry and shutdown bounds. Its [operator runbook](runbooks/reliability.md) covers agent, API, collector, node and partial-rollout failures. These contracts do not add collector replication or persistence.

Default Pod history is retained in collector memory for at most 15 minutes, with at most 181 points per Pod instance and 1,000 series in total. One history lookup returns at most 20 Pod instances. Operators may configure lower limits. Higher limits require measured capacity evidence and remain bounded.

An incident capture is a separate local file, not collector retention. KubeMemLens writes it with mode `0600`, refuses replacement without explicit confirmation and leaves retention and deletion to the operator.

## Data and metadata exposure

Kubernetes names and runtime identifiers can reveal tenant and workload structure. Treat every collector read and capture as sensitive operational data even when it contains no application payload.

| Surface | Data present | Retention and visibility rules |
| --- | --- | --- |
| Agent ingestion | Node, namespace, Pod, Pod UID, container, container ID, cgroup path, bounded labels, resource and owner context, sample times, memory composition, boundaries, pressure and event counters. | Current state and selected Pod history stay in collector memory. The writable endpoint requires authenticated, node-bound ingestion before v1. |
| Collector read API | Container, Pod, namespace, workload and node names; raw runtime identifiers and cgroup paths on container records; Kubernetes context; current memory evidence; bounded Pod history. | Current `main` requires namespaced or explicit cluster RBAC through the aggregated API. The published alpha remains cluster-wide for any caller that can reach it. |
| CLI and TUI | Authorised API data needed for the selected view, including Kubernetes names and memory evidence. | Interactive views may show identifiable names. They must not broaden the caller's server-authorised scope. |
| Collector metrics | Namespace, Pod and node names by default; container names only when container metrics are enabled. Memory, diagnosis, event and freshness values are exported. | The secure profile requires the separate metrics-reader role. No KubeMemLens persistence. The operator's metrics system controls retention. Pod UID, container ID, cgroup path, image, file path, owner reference and arbitrary labels are excluded. |
| Agent metrics | Scan, post, mapping, duration and cache counts. | No namespace, Pod, container, cgroup or node labels. |
| Logs | Node names, bounded counts, operational errors and configured limits may appear. | Current alpha error text requires review before sharing. Before v1, tests must prove that credentials, Pod UIDs, container IDs, raw label maps, cgroup paths and file paths stay out of logs. |
| Default incident capture | Pod, namespace, node, container and workload display names; memory evidence; optional bounded history. | Pod UIDs, container IDs, cgroup paths and label maps are removed. The result is redacted, not anonymous. |
| Sensitive incident capture | The default fields plus Pod UID, container ID, cgroup path and bounded labels. | Requires `--include-sensitive`, stays local and must not be attached to a public issue without manual redaction. |
| File, process and volume names | File and process names are not collected. Volume names are not collected by the current deep-mode model. | Future authorised volume views and direct traces require their own contracts. Names remain excluded from default metrics, logs and redacted captures. |

KubeMemLens does not send product telemetry or copy cluster data to a hosted service.

## Compatibility and deprecation policy

- Stable v1 CLI flags, collector API paths, schema fields and Helm values follow semantic versioning.
- A v1 schema may add optional fields. Removing a field, changing its meaning or changing its type requires a new schema or API major version.
- A public v1 interface is deprecated in the changelog and documentation for at least one KubeMemLens minor release before removal. A critical security fix may remove an unsafe path sooner, with a release note and migration instruction.
- Alpha interfaces may still change before v1. Each alpha or release candidate must state incompatible changes in its release notes.
- Kubernetes, provider and runtime evidence applies only to the recorded combination. `reviewDueAt` reports advisory freshness; passing evidence does not expire into a release failure. A change to the Kubernetes version, node image family, kernel, runtime major, cgroup mode, CNI policy behaviour, chart privileges, authentication boundary or collection path must narrow the affected claim unless maintainers explicitly approve a new opt-in qualification run.

## Changing this contract

A pull request that adds, widens or removes a support row is a release decision. It must:

1. update this file and the changelog;
2. name the exact profile and evidence owner;
3. link a reviewed, sanitised qualification result for any new support claim;
4. update installation, security or release notes when operator behaviour changes; and
5. pass the release documentation gate and clean-consumer checks before publication.

One passing provider run supports only the recorded profile. Live-cloud qualification is not scheduled, run in CI or repeated automatically for releases. A manifest render, fixture, binary archive or configured safety ceiling is not provider, scale or terminal evidence.

## Report a compatibility gap

Use the compatibility issue form and submit synthetic or redacted evidence. Include KubeMemLens, Kubernetes, kernel, runtime, operating system and cgroup-mode versions. Never attach credentials or unredacted production identifiers.
