# Compatibility Matrix

This matrix distinguishes implemented support from verified environments. Empty confidence is not treated as compatibility.

## Current contract

| Area | Current state | Evidence |
|---|---|---|
| Go | Module baseline 1.26.6 | CI configuration, patched digest-pinned builder and local build/test |
| Host accounting | cgroup v2 `memory.*` files | Parser, walker, and synthetic fixtures |
| cgroup v1 | Unsupported | No v1 parser or deployment path |
| CPU architecture | Linux, macOS, and Windows amd64/arm64 CLI archives; Linux amd64/arm64 image configuration | Current-source archives and SBOMs validated locally; published tag workflow not yet executed |
| Kubernetes | The three upstream-supported minor releases: 1.34, 1.35 and 1.36 as of 18 July 2026 | Complete local 1.34.8/1.35.5/1.36.1 kind lifecycle runs; matching automated matrix prepared from kind 0.32.0 release images |
| Container runtimes | Recognises containerd, Docker, CRI-O, and raw hex cgroup path conventions | containerd 2.x on kind verified; other runtimes remain fixture-only |
| Collector scale | Exactly one in-memory replica | Helm enforces one replica |
| History | Bounded 15-minute default with per-instance points, event/reclaim deltas, PSI and composition | Store/CLI/TUI tests and live kind workflow |
| Terminal UI | 80×24 minimum workflow; standard 120×30; wide memory dashboard at 150×30 and above | State/render/race tests plus [20-Pod local PTY qualification](qualification/local-tui-2.0-2026-08-19.md) |
| Managed node targeting | Linux nodes only; explicit operator-supplied tolerations | Rendered chart and existing-cluster qualification harness |
| Workload artefact | Tag for normal release flow; exact SHA-256 digest supported and preferred for qualification | Helm validation and CI render checks |

## Developer-preview matrix

| Kubernetes | Runtime | Node OS | Cgroup | Status |
|---|---|---|---|---|
| 1.34.8 | containerd 2.x | kind node image | v2 | Verified locally on 18 July 2026 |
| 1.34.8 | containerd 2.3.1 + Calico 3.32.1 | kind/LinuxKit arm64 | v2 | Qualified locally on two nodes; NetworkPolicy enforced; [evidence](qualification/local-kind-calico-2026-07-18.md) |
| 1.35.5 | containerd 2.x | kind node image | v2 | Verified locally on 18 July 2026; GitHub-hosted job pending |
| 1.36.1 | containerd 2.x | kind node image | v2 | Verified locally on 18 July 2026; GitHub-hosted job pending |
| Profile not yet qualified | CRI-O | Fedora CoreOS | v2 | Fixture-only; no support claim |
| Profile not yet qualified | containerd | GKE Standard Linux node pool | v2 | Qualification run required |
| 1.36.2 | containerd 2.2.4 | EKS managed Linux / Amazon Linux 2023 amd64 | v2 | [Qualified on 18 July 2026](qualification/eks-managed-linux-2026-07-18.md); Fargate excluded |
| Profile not yet qualified | containerd | AKS Linux node pool | v2 | Qualification run required |

[Kubernetes upstream maintains the latest three minor releases](https://kubernetes.io/releases/). The declared window must be reviewed before every release and the CI matrix moved forward when that window changes; an end-of-life minor must not remain a release-support claim merely because an older test passed. Patch versions use the immutable node images published for the current kind release, which can trail the newest Kubernetes patch. Each CI row also downloads a checksum-pinned kubectl from the same minor, keeping client/server skew inside the supported contract. A row becomes verified only after install, node coverage, mapping, explanation, metrics, restart, upgrade, and uninstall checks pass and evidence is linked.

The 1.34.8, 1.35.5 and 1.36.1 runs used kind 0.32.0 with release-pinned node-image digests. Each passed Helm install, fresh node reporting, cgroup v2/systemd detection, containerd detection, Node MemoryPressure/allocatable lookup, zero cgroup read errors, 11/11 workload-container mapping, CLI `status`, strict `doctor`, selectors/structured `top`, Pod/workload explanations, bounded history/since, live comparison, capture/replay, Pod/workload before-after comparison, machine-output privacy, read-only recommendations, collector/agent metrics, an upgrade, a rollback between valid revisions, and uninstall with cluster-scoped RBAC removal. The kind CNI does not enforce NetworkPolicy, so these runs do not replace the separate Calico evidence. The repeatable path is `make e2e-kind`.

The TUI 2.0 extension to that lifecycle seeded 20 workload Pods across three namespaces and exercised a real service-proxy session at 80×24, 120×30 and 180×50. The minimum-size pass covered scrolling, sort, filter, detail history, pause/manual refresh, a read-only action and terminal restoration; the wide pass asserted the memory dashboard. See the [sanitised record](qualification/local-tui-2.0-2026-08-19.md).

Use [`make qualify-cluster`](qualification.md) for an existing managed or CRI-O cluster. It requires an immutable workload digest, checks every schedulable Linux node, proves read-port access and ingestion-port denial, exercises upgrade/rollback/uninstall, and writes sanitised evidence. The harness being present is not provider evidence; only a reviewed passing run changes a matrix row to verified.

Use the separate [live-density soak](qualification/live-density-soak.md) for 5,000/10,000-container capacity claims. Installation qualification and a synthetic API regression do not establish live density, churn recovery or workload impact.

The local Calico qualification proves that the rendered policy permits read access and denies an unlabelled same-namespace Pod on ingestion port `8081`. It does not prove GKE Dataplane V2, Amazon VPC CNI policy, Azure CNI policy or another provider-specific enforcement mode.

## Report a compatibility gap

Use the compatibility issue form and submit synthetic or redacted evidence. Include KubeMemLens, Kubernetes, kernel, runtime, operating system, and cgroup mode versions. Never attach credentials or unredacted production identifiers.
