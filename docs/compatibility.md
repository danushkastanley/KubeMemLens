# Compatibility Matrix

This matrix distinguishes implemented support from verified environments. Empty confidence is not treated as compatibility.

## Current contract

| Area | Current state | Evidence |
|---|---|---|
| Go | Module baseline 1.26.6 | CI configuration, patched digest-pinned builder and local build/test |
| Host accounting | cgroup v2 `memory.*` files | Parser, walker, and synthetic fixtures |
| cgroup v1 | Unsupported | No v1 parser or deployment path |
| CPU architecture | Linux, macOS, and Windows amd64/arm64 CLI archives; Linux amd64/arm64 image configuration | Current-source archives and SBOMs validated locally; published tag workflow not yet executed |
| Kubernetes | The three upstream-supported minor releases: 1.34, 1.35 and 1.36 as of 22 August 2026 | Automated kind lifecycle matrix |
| Container runtimes | Recognises containerd, Docker, CRI-O, and raw hex cgroup path conventions | containerd 2.x on kind verified; other runtimes remain fixture-only |
| Collector scale | Exactly one in-memory replica | Helm enforces one replica |
| History | Bounded 15-minute default with per-instance points, event/reclaim deltas, PSI and composition | Store/CLI/TUI tests and live kind workflow |
| Terminal UI | 80×24 minimum workflow; standard 120×30; wide memory dashboard at 150×30 and above | State, render and race tests plus local PTY qualification |
| Managed node targeting | Linux nodes only; explicit operator-supplied tolerations | Rendered chart and existing-cluster qualification harness |
| Workload artefact | Tag for normal release flow; exact SHA-256 digest supported and preferred for qualification | Helm validation and CI render checks |

## Managed-provider qualification

| Provider | Target | Status |
|---|---|---|
| GKE | Standard Linux node pool with containerd and cgroup v2 | Pending |
| EKS | Managed Linux node group with containerd and cgroup v2 | Pending consolidated record |
| AKS | Linux node pool with containerd and cgroup v2 | Pending |
| Self-managed | Linux with CRI-O and cgroup v2 | Pending |

[Kubernetes upstream maintains the latest three minor releases](https://kubernetes.io/releases/). Review the declared window before every release and move the CI matrix forward when that window changes. An end-of-life minor must not remain a support claim merely because an older test passed. The repeatable local and CI path is `make e2e-kind`.

Use [`make qualify-cluster`](qualification.md) for an existing managed or CRI-O cluster. It requires an immutable workload digest, checks every schedulable Linux node, proves read-port access and ingestion-port denial, exercises upgrade/rollback/uninstall, and writes sanitised evidence. The harness being present is not provider evidence; only a reviewed passing run changes a matrix row to verified.

Publish the GKE, EKS and AKS records together after all three runs pass review. Until then, the managed-provider rows remain pending and no provider support claim is made.

## Report a compatibility gap

Use the compatibility issue form and submit synthetic or redacted evidence. Include KubeMemLens, Kubernetes, kernel, runtime, operating system, and cgroup mode versions. Never attach credentials or unredacted production identifiers.
