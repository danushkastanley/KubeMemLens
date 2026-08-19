# KubeMemLens Open-Source Completion Audit

Date: 18 July 2026
Scope: every finding, recommendation, roadmap exit criterion and product boundary in [the research report](OPEN_SOURCE_PRODUCT_RESEARCH_2026-07-18.md)
Outcome: source release-candidate implementation complete; public release not yet qualified

## Product outcome

KubeMemLens now implements the intended focused product: a privacy-first, terminal-native Kubernetes memory incident explainer with K9s-like navigation. It is not a generic resource browser, SaaS, long-term metrics store, automatic rightsizer or always-on profiler.

The default installation remains non-privileged and capability-free. Optional eBPF work is deliberately gated by [ADR 0001](adr/0001-defer-ebpf-until-security-and-benchmark-gates.md). Public compatibility and publication claims are gated by [ADR 0002](adr/0002-require-external-qualification-before-public-release.md).

## P0 blocker traceability

| Research finding | Resolution | Primary evidence |
|---|---|---|
| Overlapping file/shmem and kernel/slab taxonomy | Primary anon/file-cache/shmem/residual buckets partition total; explicitly overlapping kernel/slab/socket/page-table/mapped-file/THP detail retains raw evidence and diagnoses use corrected ratios | `internal/model/memory.go`, `internal/cgroup/parser.go`, `docs/memory-semantics.md`, parser/model/explanation tests |
| Snapshot double-counting and unbounded churn | Atomic per-node replacement, empty snapshots, out-of-order protection, stale-data release, node/container capacity ceilings | `internal/collector/store.go`, `internal/collector/store_test.go` |
| Lifetime events presented as recent risk | Consecutive per-container event deltas; first point is a baseline; local events preferred | `internal/model/memory.go`, `internal/collector/history.go`, model/store tests |
| Under-protected ingestion | Separate ports, strict JSON, byte/entity/time/field bounds, HTTP timeouts, NetworkPolicy, explicit residual CNI/same-namespace caveat | `internal/collector/server.go`, chart Service/NetworkPolicy, `docs/security-model.md` |
| Excessive permissions and weak Pod security | Removed `/proc`, split ServiceAccounts, get-only Node/owner access, no human Pod/log access, non-root/seccomp/drop-all/read-only defaults | chart RBAC/workloads, example viewer Role, security docs |
| Reachable Go vulnerabilities | Dependency graph updated; `govulncheck` is a required CI and local check | `go.mod`, `.github/workflows/ci.yml` |
| Missing public release machinery | CI, coverage visibility, pinned configuration/secret/image scans, draft tag workflow, versioning, checksums, SBOMs, provenance, signatures, OCI chart, Krew renderer, Artifact Hub chart metadata and community files prepared | `.github`, `.goreleaser.yml`, `deploy`, chart package, root community documents |

## Scale and operability traceability

| Research gap | Resolution or gate |
|---|---|
| Pod list every five seconds | Node-filtered informer cache with sync timeout and cache metrics |
| Full cgroup walk cost unknown | Reproducible 100/1k/5k/10k fixtures, macOS baseline, Linux kind-node benchmark mode and a prepared live soak; actual 5k/10k live evidence remains ADR 0002 |
| Sequential TUI full-list reads | One bounded paged container traversal builds node/namespace/workload/Pod/container views per refresh; row/detail viewports render only the visible window and selected history remains on-demand |
| Unbounded/unpaginated list endpoints | Current clients request at most 500-record keyset pages with byte-aware reduction; a 10,000-record regression traverses all records within the response ceiling |
| Unsafe collector replicas | Helm rejects any replica count other than one |
| Missing self-observability | Agent scan/map/cache/post and collector ingestion/request/history metrics use bounded label sets |
| Missing runtime/provider matrix | containerd kind and one EKS managed-Linux profile are qualified; an immutable-image harness records Linux-node coverage, runtime, Pod security, NetworkPolicy, upgrade/rollback and cleanup evidence; CRI-O, GKE, AKS and broader EKS qualification remain ADR 0002 |

## Feature pillar traceability

### Pressure-aware model

- Parses current/peak/min/low/high/max, swap current/peak/max/events, PSI some/full, local/hierarchical events, kernel/socket/page-table/slab/THP/mapped/dirty/writeback, scan/steal/refault and major faults.
- Derives event and reclaim deltas per stable container ID; exposes reclaim efficiency and counter-reset baseline state.
- Every explanation includes investigation severity, independent confidence and reason, evidence/counter deltas, explicit caveats, exact observation/counter-window timestamps, and copyable read-only commands. First observations and mixed-node windows are labelled incomplete or non-uniform.

Evidence: `internal/cgroup`, `internal/model`, `internal/explain`, `internal/collector/history.go`, explanation/history tests.

### Kubernetes-aware explanation

- Includes request/limit/headroom, QoS, restart and recent termination/exit, phase, age, runtime class, direct/top-level owner, Node MemoryPressure and allocatable memory.
- Includes aggregate counts/limits for memory-backed `emptyDir` without collecting volume names.
- Uses only existing Pod watch and get-only own-Node/owner resolution permissions. Kubernetes events and kubelet eviction configuration remain on-demand next commands rather than broader ambient RBAC.

Evidence: `internal/kube`, `internal/agent/scanner.go`, CLI/TUI context renderers, mapper/node/explanation tests.

### Bounded incident history

- Fifteen-minute configurable ring by default, with duration/series/point/response caps and no persistent database/CRD.
- `history pod --since`, TUI sparklines, PSI/event/reclaim markers, composition points, per-signal incident growth rates, and Pod-instance separation.
- Prometheus remains the long-term path; optional rules, five focused runbooks and a four-panel Grafana dashboard are disabled by default.

Evidence: collector history, CLI/TUI history, `charts/kube-memlens/templates/prometheusrule.yaml`, dashboard JSON and `docs/runbooks`.

### Workload and incident workflow

- `doctor`, Pod/workload top and explain, workload outliers, live Pod compare, before/after Pod or workload compare, redacted capture, bounded strict replay.
- Kubectl-style label/field selectors, sorting, watch, no-headers and table/JSON/YAML/CSV output.
- Default captures are mode `0600`, refuse overwrite, strip IDs/paths/labels and make no cluster connection during replay.

Evidence: `internal/cli/capture.go`, `replay.go`, `compare.go`, related tests, and `hack/e2e-kind.sh`.

### K9s-like interaction and integrations

- Bubble Tea node/namespace/workload/Pod/container/evidence navigation with virtualised scrolling, structured filters, deterministic risk sort, pause/manual refresh, drill/back and race-safe on-demand history.
- Compact and standard layouts preserve the minimum diagnostic fields; the wide Pod dashboard combines observed Pod charge, namespace context, Pod composition/limits/signals, selected evidence and node context without claiming total node usage.
- Typed recommendations, compare, private redacted capture and command-copy actions remain read-only. Tab changes table/detail focus only in wide layouts.
- K9s Pod plugin invokes the CLI with current context/kubeconfig; machine explanation v1 exposes severity/caveats/evidence-window metadata while excluding runtime IDs, cgroup paths, UIDs, labels and raw objects.
- Composition-aware recommendations are text/JSON/YAML and always set `automaticMutation: false`.

Evidence: `internal/tui`, `internal/incident`, `hack/tui-smoke.exp`, `hack/e2e-tui-kind.sh`, [local TUI qualification](qualification/local-tui-2.0-2026-08-19.md), `deploy/k9s/plugins.yaml`, `docs/k9s-integration.md`, `docs/explanation-schema.md`, `internal/recommend`.

### Community diagnosis feedback

- A dedicated public issue form separates false, ambiguous, missing-evidence and unhelpful-next-step reports.
- It requests versioned machine evidence and independent ground truth, prefers synthetic/default-redacted replay fixtures, and requires a public-submission privacy acknowledgement.

Evidence: `.github/ISSUE_TEMPLATE/diagnosis_feedback.yml`, `docs/community-feedback.md`, `docs/explanation-schema.md`, default-redacted incident capture/replay tests.

### Optional deep tracing

- No eBPF programme, loader, privileged chart or tracing claim exists.
- The design targets compatible Linux worker nodes on self-managed Kubernetes, GKE, EKS and AKS, while failing preflight on prohibited provider/serverless modes.
- Shared multi-tenant isolation, explicit raw-path disclosure, hard trace limits, teardown, supply chain and independent review are documented.
- ADR 0003 records digest-pinned Inspektor Gadget prototype inputs and preserves KubeMemLens server-side admission; it rejects a bypassable direct CLI passthrough and does not constitute tracing support.

Evidence: `docs/ebpf`, `docs/security/KubeMemLens-threat-model.md`, ADR 0001.

## Product-boundary audit

| Boundary | Result |
|---|---|
| No SaaS/external telemetry by default | Preserved; no phone-home path or external client exists |
| No generic web dashboard | Preserved; optional Grafana content uses the operator's own stack |
| No internal-state CRD or persistent database | Preserved |
| No automatic remediation | Preserved; recommendation schema makes this machine-verifiable |
| No unbounded history/state/response | Preserved through configured hard ceilings |
| No privileged/always-on eBPF default | Preserved; eBPF is unimplemented and separately gated |
| No arbitrary labels in metrics | Preserved; internal selector labels are stripped from metrics/captures/machine explanations |
| No unsupported “memory leak” claim | Preserved in diagnosis language and confidence model |

## Verification completed locally

- Complete unit suite, 61.5% aggregate statement coverage report, race suite, vet, builds, formatting/diff checks and reachable-vulnerability scan. The 19 August rerun used Go 1.26.6 and reported zero reachable vulnerabilities.
- Digest-pinned Trivy 0.72.0 configuration and committed-secret scans plus a scan of the built scratch image; no high/critical configuration or image finding and no committed secret was reported.
- Helm lint/default and optional renders, strict Kubeconform validation, unsafe-replica rejection.
- Prometheus 3.12 `promtool check rules`: eight rules valid; dashboard JSON parsed and panel count checked.
- K9s plugin and issue-form YAML parsed; machine explanation privacy/recommendation contracts, severity/caveats and exact evidence windows tested.
- Kubernetes 1.34.8, 1.35.5 and 1.36.1/containerd kind installs, strict doctor, top/selectors, Pod/workload explanations with a real elapsed counter window, history/since, live compare, capture/replay preserving the window, Pod/workload before-after compare, metrics, upgrade, rollback, uninstall and cluster-RBAC removal. The development density/churn smoke ran on the 1.34 path.
- TUI 2.0 state, resize, virtualisation, failure, privacy and race tests, with 78.4% package statement coverage. A one-iteration 10,000-Pod risk-filter/sort benchmark completed in 146 ms on the local Apple M4 Pro. A disposable 20-Pod kind run passed the full 80×24 workflow plus 120×30 and 180×50 PTY layout checks, then completed Helm rollback/uninstall, RBAC removal and cluster deletion.
- One Amazon EKS 1.36.2 managed Linux node-group profile passed immutable-image verification, 11/11 strict doctor checks, 7/7 mapping, VPC CNI ingestion isolation, explanation privacy, metrics, upgrade, rollback, uninstall, RBAC removal and AWS residue cleanup. See the [sanitised EKS record](qualification/eks-managed-linux-2026-07-18.md).
- Two-node Kubernetes 1.34.8/containerd/Calico 3.32.1 qualification with immutable image identity, Linux-node coverage, Pod-security assertions, 15/15 mapping, enforced ingestion NetworkPolicy, sanitised evidence, upgrade, rollback recovery, uninstall and RBAC cleanup.
- Linux arm64 kind-node synthetic fixtures at 5,000/10,000 cgroups and Pod mappings; see the performance baseline for limitations.
- Synthetic 10,000-container collector/API regression: the legacy array hit the configured 16 MiB ceiling while current keyset pages returned all records without duplicates or an oversized body.
- Development-scale live-density harness validation with 20 real containers, 30 seconds steady state, complete mapping, 44–67 ms CLI queries, no new post failures/restarts/OOMs and two-second churn recovery. Metrics API was unavailable; no 5,000/10,000 claim is made. See [the smoke record](qualification/local-live-density-smoke-2026-07-18.md).
- A current-source GoReleaser 2.17 snapshot produced all six Linux/macOS/Windows amd64/arm64 CLI archives; every checksum and archive layout validated and the packaged macOS arm64 binary ran with embedded version metadata. Checksum-verified Syft 1.44.0 produced six non-empty SPDX 2.3 archive SBOMs, included in the checksum file and free of local build paths. This remains local evidence, not a substitute for the tag workflow, keyless signing or GitHub provenance gates.

## Remaining public-release gates

These are deliberately not marked complete:

1. GitHub-hosted CI from a clean pushed commit, including all three upstream-supported minor versions in the declared kind matrix.
2. CRI-O and representative GKE/AKS profiles, plus EKS profiles beyond the single qualified Amazon Linux/containerd row. Generic Kubernetes NetworkPolicy semantics are verified locally with Calico and the qualified EKS run observed VPC CNI ingestion denial.
3. Execute the prepared [5,000/10,000 live-container churn/soak](qualification/live-density-soak.md) and review its per-component agent/collector resources, node/API latency, mapping, filesystem-read/API-request and separately attached workload-impact evidence.
4. An authorised pre-release tag and audit of actual archives, image, chart, checksums, SBOMs, signatures and provenance.
5. Authorised publication, immutable Krew submission and Artifact Hub listing.
6. An eBPF prototype, benchmarks and independent security review if that optional feature proceeds.

Until those gates pass, describe the repository as a locally verified release candidate, not a supported public release or production-certified managed-provider tool.

The repeatable external paths are [`hack/qualify-cluster.sh`](../hack/qualify-cluster.sh) with [the installation qualification runbook](qualification.md), and [`hack/soak-live-density.sh`](../hack/soak-live-density.sh) with [the live-density runbook](qualification/live-density-soak.md). Their existence reduces manual variance but is not substitute evidence for any unrun provider or density row.
