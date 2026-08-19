# Local Live-Density Harness Smoke — 18 July 2026

This is development evidence for the live-density harness and paged client path. It is not the 5,000/10,000 release gate and is not managed-provider evidence.

## Environment

- Kubernetes: kind node image `v1.34.8`
- Host: Apple silicon macOS with Docker Desktop
- KubeMemLens: locally built after bounded pagination implementation
- Workload image: immutable digest `sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028`
- Workload: 20 live containers, 10 per Pod, 30-second steady state, then zero-surge rolling restart
- Metrics API: unavailable; resource-use acceptance was therefore not evaluated

## Result

| Signal | Observed |
|---|---:|
| Harness outcome | Passed |
| Samples | 7 steady + 1 post-churn |
| Workload containers visible in every sample | 20/20 |
| Cluster mapping in every sample | 31/31, 0 unmapped |
| CLI `doctor` query time | 44–67 ms |
| Maximum latest agent scan | 23.411 ms |
| KubeMemLens Pod readiness | 2/2 throughout |
| KubeMemLens restarts / OOM kills | 0 / 0 throughout |
| Rolling-restart mapping recovery | 2 seconds |

The agent exposed one cumulative snapshot-post failure at the first sample, consistent with installation startup, and the counter remained exactly `1` throughout steady state and churn. No new post failure occurred during the measured window.

The exact workload namespace, Pod names, node name, container IDs and registry name were excluded from the retained summary. The disposable workload namespace, kind cluster, local KubeMemLens image and temporary kubeconfig were removed after review.

## Interpretation

This run proves that the safety checks, real-container workload generation, bounded client traversal, evidence reduction, churn step, cleanup ownership and final strict `doctor` path work together at development scale. It does not establish capacity, resource overhead, API-server impact, CNI behaviour, a 30-minute soak, or GKE/EKS/AKS compatibility.

The public release gate remains two reviewed runs using the [live-density runbook](live-density-soak.md): 5,000 and 10,000 containers for at least 30 minutes with Metrics API or equivalent monitoring and separately reviewed workload-impact evidence.
