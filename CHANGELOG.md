# Changelog

All notable changes will be documented here. KubeMemLens intends to follow [Semantic Versioning](https://semver.org/) after its first public release.

## Unreleased

## 1.0.0 - replacement candidate prepared as v1.0.0-rc.2 on 2026-08-29

### Added

- Prepared prospective `v1.0.0` artefacts under the replacement `v1.0.0-rc.2` candidate identity for no-rebuild stable promotion after fresh approval, adopter evidence and go/no-go evidence pass.
- Retained the failed `v1.0.0-rc.1` workflow record; it stopped before publication and its immutable tag was not moved.
- Added bounded native Go fuzzing for cgroup parsing, tenant-scoped pagination and snapshot decoding, with a dedicated pull-request and `main` CI job.

### Changed

- Defined one canonical v1 support and release contract covering exact target profiles, evidence owners, multi-tenant security, best-effort availability, bounded history, metadata exposure, unsupported environments and deprecation rules.
- Accepted a Kubernetes aggregated-API design for authenticated operator reads, tenant-scoped authorisation, node-bound agent writes and replay protection.
- Moved agent writes to `memory.kubememlens.io/v1alpha1` with Pod-bound identity, node binding, collector epochs, idempotent sequences, bounded abuse controls and serving-certificate rotation.
- Moved production CLI, TUI and metrics reads to tenant-scoped aggregated resources; the secure Service now exposes only TLS port `443` and keeps port `8080` health-only inside the collector Pod.
- Removed legacy direct ingestion, collector reads and ServiceMonitor rendering from the production Helm chart while retaining binary/client rollback code for pre-v1 installations.
- Narrowed the intended self-managed containerd qualification row to amd64 so one reviewed provider record cannot be misrepresented as arm64 evidence.
- Published one-time, digest-bound provider/runtime evidence for the exact GKE Standard, EKS managed-node, self-managed containerd and CRI-O combinations, with freshness reported as advisory rather than a scheduled or per-release cloud gate.
- Classified standard AKS as unsupported for the recorded candidate because AKS supplied no request-header proxy client-name constraint and the candidate correctly failed closed; GKE Autopilot, EKS Fargate, AKS virtual nodes, Windows nodes and cgroup v1 remain explicit unsupported rows.
- Qualified xterm, Kitty, Alacritty, tmux and delayed SSH for the exact terminal candidate, with 30-minute PTY and real-emulator runs; named macOS emulators remain unqualified until a real permitted run exists.
- Changed release publication to build the multi-architecture image once, promote its OCI archive without digest change, sign the final subject inventory and create a draft only after clean-consumer verification.
- Added strict Helm values schema validation and a non-privileged chart test hook exercised across install, upgrade and rollback.
- Moved required CI lifecycle coverage to the current upstream-supported Kubernetes 1.35, 1.36 and 1.37 minor releases after Kubernetes 1.37 reached GA.
- Aligned local, CI, release and container builds on Go 1.27 and raised the Darwin archive baseline to macOS 13.

### Security

- Updated `golang.org/x/crypto` and its coordinated `golang.org/x/*` module set to releases that address `GO-2026-6303`.
- Expanded the threat model to cover shared-cluster authentication, delegated authorisation, agent token theft, replay, confused-deputy paths, certificate lifecycle and collector compromise.
- Added an opt-in kind feasibility check for aggregation configuration, Pod-bound identity claims and least-privilege namespace, cluster, agent and metrics RBAC.
- Removed the default plaintext ingestion Service port and added projected rotating tokens, exact delegated-auth RBAC and fail-closed ingestion diagnostics.
- Added unbound namespace-viewer, cluster-viewer and metrics-reader roles, server-side scope filtering, direct-identifier denial, scope-bound pagination and multi-user revocation tests.
- Added a disposable-cluster adversarial isolation gate covering direct reachability, NetworkPolicy removal, delegated-authorisation failure, denial timing, bounded read abuse, least privilege and retained-evidence privacy.
- Bound agent operational metrics to loopback and sanitised cgroup scan failures so node-local density and runtime identifiers do not escape through the Pod network or logs.
- Protected `v*` release tags and the reviewed release environment, enabled immutable GitHub releases, and removed persisted write credentials from the publication checkout.

### Upgrade

- Remove retired pre-v1 Helm values before upgrading. Do not use `--reuse-values` across this boundary; render reviewed current values or use Helm's reset-values path.
- Expect one collector restart and loss of its in-memory snapshots, recent history and event-delta baselines. The new generation reports `rebuilding` until agents post fresh evidence.
- Grant the unbound namespace-viewer, cluster-viewer and metrics-reader roles explicitly. The chart does not choose users or service accounts on the operator's behalf.

### Rollback

- In a shared cluster, roll back only to a revision that retains authenticated ingestion and delegated reads. If that boundary cannot be restored, disable KubeMemLens instead of restoring `v0.0.1-alpha.3`.
- The chart has no CRDs, persistent volumes or data migration. Helm rollback restores workloads and configuration but cannot restore discarded in-memory evidence.

### Known limitations

- The collector is one best-effort in-memory replica. High availability and durable history remain deferred.
- Standard AKS, GKE Autopilot, EKS Fargate, AKS virtual nodes, Windows workers and cgroup v1 remain unsupported for deep mode. Provider support never extends beyond the recorded environment versions.
- macOS terminal emulators remain unqualified. The recorded Linux terminal rows and local 5,000-container result do not imply broader terminal or managed-provider scale coverage.

## 0.0.1-alpha.3 - 2026-08-22

### Added

- Prometheus/OpenMetrics collector export with cardinality guardrails and optional ServiceMonitor.
- Versioned snapshot schema, recent `memory.events` deltas, and text/JSON build information.
- Node-filtered informer cache and low-cardinality agent operational metrics.
- Collector ingestion outcome and request-duration metrics.
- CI for formatting, tests, race detection, vet, vulnerability scanning, builds, and Helm validation.
- CI coverage reporting plus digest-pinned configuration, committed-secret, and built-image security scans.
- Bounded Pod history with pressure/event markers and workload roll-ups that retain replica outliers.
- Cached top-level workload owner resolution with explicit doctor and RBAC diagnostics.
- Evidence-based explanation confidence and copyable Pod/workload follow-up commands.
- Independent diagnosis severity, explicit caveats, and exact gauge/counter evidence windows in text, TUI, JSON, YAML, recommendations, capture, and replay paths.
- Redacted, schema-versioned incident capture and bounded offline replay.
- Live Pod comparison and before/after Pod or workload incident comparison with composition deltas and per-signal growth rates.
- Kubectl-style label/field selection, sorting, watch, header suppression, and table/JSON/YAML/CSV top output.
- K9s-style TUI workload drill-down, pause/resume, fast navigation, and on-demand bounded Pod trends.
- TUI 2.0 virtualised tables/details, responsive wide memory dashboard, first-class node navigation, risk filters/sorting, live selected-Pod history and typed read-only incident actions.
- Opt-in three-size PTY qualification with a disposable 20-Pod kind workload and sanitised assertion summary.
- Optional recording/alerting rules, focused runbooks, and a four-panel Grafana dashboard.
- Read-only K9s Pod plugin and versioned JSON/YAML explanation contract without runtime identifiers.
- Composition-aware recommendation export with rationale, guard conditions, and automatic mutation disabled.
- Runtime class, node allocatable memory, and bounded memory-backed `emptyDir` context in Pod explanations.
- `history pod --since` filtering within the bounded in-memory history window.
- Reclaim/refault/major-fault deltas, reclaim efficiency, and per-composition growth rates in history, explanations, and incident comparison.
- Optional eBPF design gate, managed-provider boundary, multi-tenant threat model, benchmark protocol, and ADR-backed implementation deferral.
- An opt-in existing-cluster qualification harness for managed Linux node pools, CRI-O, NetworkPolicy enforcement, immutable image verification, upgrade/rollback, and sanitised evidence.
- A sanitised two-node kind/Calico qualification record proving the harness and default ingestion NetworkPolicy end to end.
- Diagnosis-feedback issue intake for anonymised false/ambiguous findings, plus install-to-first-explanation and per-component density-soak measurements.
- Primary anonymous/file-cache/shmem/residual composition with bounded history, sorting, and machine output, plus kernel, slab, socket, page-table, mapped-file, and THP secondary detail.
- Explicitly pinned Syft release tooling with locally validated non-empty SPDX archive SBOMs.
- Artifact Hub-ready chart README and version-aligned release-image metadata.
- Immutable Dockerfile frontend and Go builder image inputs for reproducible release builds.
- CI compatibility coverage aligned to the then-current upstream-supported Kubernetes 1.34, 1.35 and 1.36 minor releases.
- A digest-pinned Inspektor Gadget prototype evaluation decision that preserves server-side KubeMemLens tenant admission and keeps tracing out of the default chart.

### Changed

- Filesystem-backed cache now excludes shmem/tmpfs; raw cgroup `file` remains observable.
- Compact views now use a non-overlapping residual/other bucket; raw kernel and slab subdivisions remain explicit overlapping drill-down evidence.
- Collector snapshots replace each node atomically, clear missing containers, garbage-collect stale container data, and reject out-of-order updates.
- Collector reads and ingestion use separate ports and a default NetworkPolicy restricts writable ingestion to agent Pods.
- Agent and collector use separate least-privilege ServiceAccounts and hardened security contexts.
- Go, `golang.org/x/text` and the digest-pinned builder were updated to patched releases after reachable-vulnerability scanning.
- TUI snapshot reads now refresh concurrently, and collector storage/API responses have explicit capacity ceilings surfaced by `doctor`.
- TUI defaults to risk-ordered Pods; Tab now changes table/detail focus in wide layouts, while `N/n/w/p/c` select entity views explicitly.
- Helm supports exact image digests and the agent explicitly targets Linux nodes, with operator-scoped tolerations available for reviewed node pools.

### Fixed

- TUI refreshes preserve the terminal title, use synchronized rendering where supported, and report Pod age from Kubernetes creation time rather than snapshot age.

### Security

- Snapshot bodies, entity counts, timestamps, identifiers, JSON shape, headers, and HTTP timeouts are bounded or validated.
- Removed unused host `/proc`, node RBAC, human Pod/log RBAC, collector API token mounting, and ambient Linux capabilities.
