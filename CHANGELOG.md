# Changelog

All notable changes will be documented here. KubeMemLens intends to follow [Semantic Versioning](https://semver.org/) after its first public release.

## Unreleased

### Added

- Prometheus/OpenMetrics collector export with cardinality guardrails and optional ServiceMonitor.
- Versioned snapshot schema, recent `memory.events` deltas, and text/JSON build information.
- Node-filtered informer cache and low-cardinality agent operational metrics.
- Collector ingestion outcome and request-duration metrics.
- CI for formatting, tests, race detection, vet, vulnerability scanning, builds, and Helm validation.
- CI coverage reporting plus digest-pinned configuration, committed-secret, and built-image security scans.
- Open-source product research, implementation plan, and performance baseline.
- Bounded Pod history with pressure/event markers and workload roll-ups that retain replica outliers.
- Cached top-level workload owner resolution with explicit doctor and RBAC diagnostics.
- Evidence-based explanation confidence and copyable Pod/workload follow-up commands.
- Independent diagnosis severity, explicit caveats, and exact gauge/counter evidence windows in text, TUI, JSON, YAML, recommendations, capture, and replay paths.
- Redacted, schema-versioned incident capture and bounded offline replay.
- Live Pod comparison and before/after Pod or workload incident comparison with composition deltas and per-signal growth rates.
- Kubectl-style label/field selection, sorting, watch, header suppression, and table/JSON/YAML/CSV top output.
- K9s-style TUI workload drill-down, pause/resume, fast navigation, and on-demand bounded Pod trends.
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
- CI compatibility coverage aligned to the current upstream-supported Kubernetes 1.34, 1.35 and 1.36 minor releases.
- A digest-pinned Inspektor Gadget prototype evaluation decision that preserves server-side KubeMemLens tenant admission and keeps tracing out of the default chart.

### Changed

- Filesystem-backed cache now excludes shmem/tmpfs; raw cgroup `file` remains observable.
- Compact views now use a non-overlapping residual/other bucket; raw kernel and slab subdivisions remain explicit overlapping drill-down evidence.
- Collector snapshots replace each node atomically, clear missing containers, garbage-collect stale container data, and reject out-of-order updates.
- Collector reads and ingestion use separate ports and a default NetworkPolicy restricts writable ingestion to agent Pods.
- Agent and collector use separate least-privilege ServiceAccounts and hardened security contexts.
- Go networking dependencies and the minimum Go version were updated to resolve reachable vulnerabilities.
- TUI snapshot reads now refresh concurrently, and collector storage/API responses have explicit capacity ceilings surfaced by `doctor`.
- Helm supports exact image digests and the agent explicitly targets Linux nodes, with operator-scoped tolerations available for reviewed node pools.

### Security

- Snapshot bodies, entity counts, timestamps, identifiers, JSON shape, headers, and HTTP timeouts are bounded or validated.
- Removed unused host `/proc`, node RBAC, human Pod/log RBAC, collector API token mounting, and ambient Linux capabilities.
