# KubeMemLens Open-Source Implementation Plan

Status: active
Started: 18 July 2026
Source: [Open-source product research](OPEN_SOURCE_PRODUCT_RESEARCH_2026-07-18.md)

## Goal

Build the complete product described in the research document: a privacy-first, terminal-native Kubernetes memory incident explainer with K9s-like ergonomics, trustworthy cgroup semantics, pressure-aware diagnoses, bounded history, strong open-source packaging, and optional integrations.

The goal is complete only after every research recommendation and release gate is implemented, verified, or explicitly deferred with a recorded reason. A developer preview is a milestone, not the end of the goal.

## Delivery phases

### Phase A — trustworthy foundation

- [x] Make displayed filesystem cache exclude shmem/tmpfs while retaining raw cgroup `file`.
- [x] Separate slab from other kernel memory while retaining raw cgroup `kernel`.
- [x] Present primary anonymous/file-cache/shmem/residual buckets and retain overlapping kernel, slab, socket, page-table, mapped-file, and THP detail through history, metrics, comparisons, and machine output.
- [x] Replace per-container upserts with atomic per-node snapshot replacement.
- [x] Clear terminated/restarted containers immediately and release stale container data after TTL.
- [x] Add snapshot schema version 1.
- [x] Derive `memory.events` deltas and use them for recent-risk diagnoses.
- [x] Bound snapshot bytes and entity count; reject malformed, trailing, stale, future, mismatched, and duplicate input.
- [x] Bound collector node/container state and encoded read responses; surface capacity through `doctor`.
- [x] Add bounded keyset pagination for current clients and a 10,000-record API regression; derive nested views client-side to prevent oversized workload payloads.
- [x] Add server read, write, idle, header, and shutdown timeouts.
- [x] Remove the unused host `/proc` mount and broad Node list/watch RBAC; retain only a later, documented Node `get` for own-node MemoryPressure context.
- [x] Split agent and collector ServiceAccounts and remove the collector API token.
- [x] Add non-root, seccomp, capability, read-only-root, and resource defaults.
- [x] Remove Pod and log access from the human viewer role.
- [x] Upgrade the vulnerable Go networking dependency path.
- [x] Separate collector ingestion from read/metrics access and add a usable NetworkPolicy.
- [x] Add automated vulnerability scanning to CI.
- [x] Replace per-scan Pod listing with a node-filtered informer cache.
- [x] Add reproducible 100/1,000-container scan and mapping benchmarks with a development baseline.
- [ ] Run the prepared destructive opt-in live Linux density/churn harness at 5,000 and 10,000 containers and review its agent, API, resource and workload-impact evidence.
- [x] Add low-cardinality agent self-observability for scans, mapping, cache size, and snapshot posts.
- [x] Add collector self-observability for ingestion rejection and request duration.
- [x] Enforce the collector's single-replica in-memory architecture in Helm.

### Phase B — public developer preview

- [x] Add CI for formatting, coverage visibility, tests, race detection, vet, reachable-vulnerability scanning, committed-secret/configuration scanning, built-image scanning, builds, and Helm validation.
- [x] Confirm no source generators are present; validate the Krew renderer, Helm output, Prometheus rules, and dashboard artefact in CI, and require generator checks when one is introduced.
- [x] Add a Kubernetes/runtime compatibility fixture matrix and kind end-to-end coverage.
- [x] Add an opt-in existing-cluster qualification harness with immutable-image, Linux-node coverage, Pod-security, NetworkPolicy, upgrade/rollback, cleanup, and sanitised-evidence checks.
- [x] Add a separate opt-in 5,000/10,000 live-container soak harness with exact targets, steady-state sampling, rolling-restart recovery and sanitised evidence.
- [x] Add text/JSON CLI version output and build identity to all three commands.
- [x] Align the prepared v0.5 chart, app, image, and release version flow.
- [x] Add tag-driven draft release automation for multi-platform CLI archives, multi-architecture images, checksums, SBOMs, provenance, keyless signatures, and Helm packaging.
- [ ] Execute and verify the release workflow on a pre-release tag before promoting a public release.
- [x] Configure the real GHCR image/OCI chart and generate checksum-bound Krew metadata during releases.
- [ ] Execute publication, submit the immutable Krew manifest, and list the OCI chart on Artifact Hub after release audit.
- [x] Add `SECURITY.md`, contributor guide, code of conduct, maintainers/governance, changelog, CODEOWNERS, and issue/PR templates.
- [x] Add release, upgrade, uninstall, compatibility, and support-version documentation.
- [x] Add `doctor` for build identity, collector connectivity, node coverage, freshness, mapping, and store consistency.
- [x] Extend `doctor` with cgroup/runtime detection and explicit cgroup-read diagnostics.
- [x] Complete a disposable live-cluster install, upgrade, smoke, rollback, and uninstall test.

### Phase C — differentiated diagnostic product

- [x] Parse PSI, limits/protection, peak, swap, local events, and reclaim/refault signals with fixtures.
- [x] Add requests, limits, QoS, restarts, owner/workload, and node-pressure context.
  - Implemented: per-container requests/limits, Pod QoS/phase/creation time, restart and recent termination state, runtime class, memory-backed `emptyDir` counts/aggregate limits, and the direct controller owner.
  - Implemented: cached own-node MemoryPressure and allocatable-memory context with `get`-only Node RBAC and explicit `doctor` permission diagnostics.
  - Implemented: bounded, cached ReplicaSet-to-Deployment and Job-to-CronJob owner resolution with explicit least-privilege trade-offs.
- [x] Add evidence-based confidence and copyable next-step commands.
  - Explanations distinguish direct recent evidence, cumulative evidence, limit-risk inference, and single-snapshot composition heuristics.
  - Every live Pod/workload finding exposes independent severity, confidence, evidence, counter deltas, explicit caveats, and exact gauge/counter timestamps; incomplete and mixed-node windows fail visibly rather than implying uniform history.
  - Pod and workload explanations print scoped `kubectl` and `kubectl memlens` follow-up commands.
- [x] Add bounded trend history and incident markers.
  - The collector retains a configurable 15-minute rolling window with hard series, point, and response caps.
  - `history pod` exposes composition, swap, PSI, and memory-event deltas without adding persistent storage or a CRD.
- [x] Add workload views, comparison, capture, and replay.
  - Implemented: top-level workload roll-ups and workload explanation with every replica retained and the largest Pod surfaced.
  - Implemented: schema-versioned, redacted, size-bounded incident capture and cluster-independent replay.
  - Implemented: live Pod-to-Pod composition comparison and before/after Pod or top-level workload incident comparison with per-signal elapsed growth rates.
- [x] Add Kubernetes-compatible selectors, sorting, watch, and structured output.
  - `top` supports Pod label selectors, an explicit safe field-selector set, deterministic sort fields, bounded refresh intervals, header suppression, and table/JSON/YAML/CSV output.
  - Structured rows intentionally omit container IDs, Pod UIDs, cgroup paths, and arbitrary label maps.
- [x] Refine the TUI into the K9s-like memory workflow described by the research.
  - Namespace, top-level workload, Pod, container, and detail views support fast keyboard navigation, search, sort, pause/resume, refresh, drill-down, and backtracking.
  - Pod detail fetches bounded history on demand and shows a compact trend, PSI, incident events, confidence, Kubernetes context, and follow-up commands.

### Phase D — integrations and advanced attribution

- [x] Add Prometheus rules, alert runbooks, and a restrained Grafana dashboard.
- [x] Add a K9s plugin and stable machine-readable explanation contract.
- [x] Add composition-aware recommendation export.
  - Pod and workload recommendations are available as text/JSON/YAML, explain conditions and rationale, and explicitly disable automatic mutation.
- [x] Add maintainer-governed anonymised diagnosis feedback without product telemetry.
  - A dedicated issue form requests the versioned finding, exact evidence window, independent ground truth and a synthetic/redacted replay fixture while blocking real identifiers and sensitive paths.
- [x] Record the optional bounded eBPF architecture, multi-tenant threat model, benchmark protocol, managed-provider boundary, and ADR-backed deferral.
- [x] Select a digest-pinned Inspektor Gadget evaluation set while explicitly rejecting a client-side passthrough as a multi-tenant authorisation boundary.
- [ ] Prototype, benchmark, and independently security-review optional eBPF tracing before implementation or public support.

## Product boundaries

- No SaaS or external telemetry by default.
- No generic Kubernetes resource browser or web dashboard unless terminal/API evidence shows a need.
- No CRD solely for internal bounded state.
- No automatic remediation or resource mutation in the first stable release.
- No unbounded history.
- No privileged or always-on eBPF default.
- No arbitrary Kubernetes labels in metrics.
- No claim of a memory leak without workload-specific evidence.

## Current verification record

Verified on 18 July 2026:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...` — no vulnerabilities found.
- All three commands built: `kubectl-memlens`, `memlens-agent`, and `memlens-collector`.
- `kubectl-memlens sample explain cache-heavy` and `kubectl-memlens sample top` exercised successfully.
- Helm 3.18.4 lint and template passed in a container.
- Kubeconform 0.7.0 validated all eight default rendered Kubernetes resources in strict mode.
- Prometheus 3.12 `promtool check rules` validated all eight optional recording/alert rules; dashboard JSON and all local Markdown links validated.
- A current-source GoReleaser 2.17 snapshot built Linux/macOS/Windows amd64/arm64 archives; all six checksums and archive layouts validated and the packaged macOS arm64 binary ran. A checksum-verified Syft 1.44.0 generated six non-empty SPDX 2.3 archive SBOMs, all covered by the checksum file and checked for local-path leakage. Keyless signing and the real tag workflow remain external and unverified.
- Synthetic mapping and cgroup scan measurements are recorded in [the Phase A performance baseline](PERFORMANCE_BASELINE_2026-07-18.md).
- Linux arm64 kind-node fixture runs covered 5,000/10,000 cgroup-shaped directories and Pod mappings without being mislabelled as a live-container soak.
- A 10,000-record collector regression confirmed the legacy full-array response reaches the 16 MiB safety ceiling and the current paged client traverses all records without duplicate keys or oversized responses.
- The live-density harness passed a disposable development smoke with 20 real containers, complete steady/churn mapping and owned cleanup; [the record](qualification/local-live-density-smoke-2026-07-18.md) explicitly does not satisfy the 5k/10k gate.
- Live Kubernetes 1.34.8, 1.35.5 and 1.36.1/containerd 2.x runs on kind 0.32.0 passed Helm install, 11/11 workload-container mapping, strict `doctor`, `status`, selectors/structured `top`, Pod/workload `explain`, history/since, live comparison, capture/replay, Pod/workload incident comparison, machine-output privacy, recommendations, collector/agent metrics, a valid upgrade and rollback, uninstall, and cluster-scoped RBAC removal.
- `hack/e2e-kind.sh` preserves that install-to-uninstall path with isolated kubeconfig and digest-pinned Kubernetes 1.34.8/1.35.5/1.36.1 CI jobs, matching the three upstream-supported minors as of 18 July 2026; its live explanation gate waits for and validates an exact elapsed counter window through capture/replay.
- The live path was repeated after pressure parsing and Kubernetes context were added; `doctor --strict` confirmed cgroup v2/systemd, containerd, zero cgroup read errors, Node MemoryPressure=False, and 11/11 mapping before and after rollback.
- `hack/qualify-cluster.sh` statically validates and prepares the same evidence path for authorised GKE/EKS/AKS, CRI-O and NetworkPolicy-capable clusters. It has not yet been executed against those external environments.
- The qualification harness passed end to end on a two-node Kubernetes 1.34.8/containerd/Calico 3.32.1 kind cluster, including digest identity, Linux-node coverage, 15/15 mapping, enforced ingestion isolation, explanation privacy, metrics, upgrade, rollback recovery, uninstall and RBAC cleanup. See [the evidence record](qualification/local-kind-calico-2026-07-18.md).

Not yet verified: GitHub-hosted execution of the three-supported-minor kind matrix, provider-specific NetworkPolicy modes, CRI-O or GKE/EKS/AKS nodes, actual 5,000/10,000-container soak results, and the tag-driven release workflow. The repeatable live-soak procedure is documented in [the density runbook](qualification/live-density-soak.md); an unrun harness is not performance evidence. These gates are recorded in [ADR 0002](adr/0002-require-external-qualification-before-public-release.md).

Optional eBPF tracing remains intentionally unimplemented. Its architecture, threat model and benchmark protocol are recorded, but prototype measurements and an independent security review are release gates rather than paper claims.

## Completion audit

Before closing the goal:

1. Re-read the research document line by line.
2. Map every recommendation and exit criterion to code, tests, documentation, or an explicit ADR-backed deferral.
3. Run the complete CI and compatibility matrix from a clean checkout.
4. Exercise install, diagnosis, history, capture/replay, metrics, K9s integration, upgrade, and uninstall paths on supported clusters.
5. Verify security, privacy, accessibility, performance, release provenance, and rollback documentation.
