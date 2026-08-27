# Provider and runtime qualification — `b878c14`

This directory is the reviewed, sanitised PROD-008 result for one immutable
release-candidate identity:

- source: `b878c14ecb4206f82259545017c554a3fb0d704d`;
- image: `sha256:12b1b244ceed8c956cd2e3433d0c18f80c0f891b9bbc54b7c4079ec16c4e6b1b`;
- chart version: `0.0.1-alpha.3`;
- chart: `sha256:908b3a05270d8cdd873c10be2871bee8bf64b9f9fb37d5271003f323a1806f9c`;
- supported-row probe: `sha256:7a3ebe5bfd1a4a19797d20b0c0bb39d44393e9a03fd852c0865b0f540d868df0`;
- review date: 27 August 2026.

The complete [matrix report](provider-matrix.json) passes with five supported
rows and six confirmed unsupported rows. A row applies only to its recorded
Kubernetes, node image, operating system, kernel, runtime, architecture, cgroup
and CNI combination.

## Results

| Profile | Result | Recorded environment | Reason when unsupported |
| --- | --- | --- | --- |
| [GKE COS/containerd](gke-cos-containerd-amd64/provider-qualification.json) | Supported | GKE `v1.35.6-gke.1250000`, COS, containerd `2.1.7`, cgroup v2, GKE Dataplane V2 | — |
| [GKE Ubuntu/containerd](gke-ubuntu-containerd-amd64/provider-qualification.json) | Supported | GKE `v1.35.6-gke.1250000`, Ubuntu 24.04.4 LTS, containerd `2.1.5`, cgroup v2, GKE Dataplane V2 | — |
| [EKS AL2023/containerd](eks-al2023-containerd-amd64/provider-qualification.json) | Supported | EKS `v1.36.2-eks-bca9cf6`, AL2023, containerd `2.2.5`, cgroup v2, VPC CNI NetworkPolicy | — |
| [Self-managed containerd](self-managed-containerd/provider-qualification.json) | Supported | Kubernetes `v1.36.4`, Ubuntu 24.04.4 LTS, containerd `2.3.3`, cgroup v2, Calico | — |
| [Self-managed CRI-O](self-managed-crio-amd64/provider-qualification.json) | Supported | Kubernetes `v1.36.4`, Ubuntu 24.04.4 LTS, CRI-O `1.36.4`, cgroup v2, Calico | — |
| [AKS Ubuntu/containerd](aks-ubuntu-containerd-amd64/provider-qualification.json) | Unsupported for this candidate | AKS `v1.35.7`, Ubuntu 24.04.4 LTS, containerd `2.3.3`, Azure CNI Calico | `requestheader_proxy_identity_unavailable` |
| [GKE Autopilot](gke-autopilot/provider-qualification.json) | Unsupported for deep mode | GKE `v1.35.6-gke.1250000`, Autopilot, GKE Dataplane V2 | `hostpath_not_permitted` |
| [EKS Fargate](eks-fargate/provider-qualification.json) | Unsupported for deep mode | EKS `v1.36.2-eks-bca9cf6`, Fargate pod runtime | `daemonset_not_supported` |
| [AKS virtual nodes](aks-virtual-nodes/provider-qualification.json) | Unsupported for deep mode | AKS `v1.35.7`, Azure virtual kubelet; unreported host/runtime fields | `virtual_nodes_not_supported` |
| [Windows deep mode](windows-deep-mode/provider-qualification.json) | Unsupported for deep mode | AKS `v1.35.7`, Windows Server 2022, containerd `1.7.20` | `windows_nodes_not_supported` |
| [cgroup v1](cgroup-v1/provider-qualification.json) | Unsupported | Kubernetes `v1.36.4`, Ubuntu 22.04.5 LTS, containerd `2.3.3`, cgroup v1 | `cgroup_v1_not_supported` |

The AKS standard result is a recorded-live conversion, not a relabelled generic
timeout. Its schema-v3 receipt binds the retained failed candidate run, provider
inventory, candidate source and artefact digests, provider-owned empty
request-header allowed-name list, and the later qualification-tool commit. The
candidate deliberately failed closed rather than weaken aggregation-proxy
identity validation.

Unsupported means unsupported by the recorded deep-mode candidate. It does not
mean the provider can never be supported. A secure AKS-compatible authentication
design, an agentless/restricted mode, or a provider capability change can be
evaluated as a separate future claim.

## Evidence and privacy

Supported directories contain the manifest-bound lifecycle, environment,
recovery, doctor and status evidence plus pending and reviewed records.
Unsupported directories contain only the sanitised receipt and its pending and
reviewed records. Account, subscription, project, cluster, node, network,
address, credential and raw-error identifiers are excluded.

Provider infrastructure was temporary. Private cleanup records—not published
here—verify zero live resources through the owning cloud APIs, removal of the
temporary budgets and role assignments, deletion of run-created Azure Network
Watchers, removal of dedicated kubeconfigs/helpers, and restoration of the
default kubeconfig.

## Freshness policy

`reviewDueAt` is an advisory freshness date anchored to the original live run.
It does not expire valid historical evidence, trigger cloud provisioning, or
block CI and releases. The matrix reports stale rows as warnings. A change that
affects a provider-sensitive boundary must narrow the claim unless maintainers
explicitly approve a new one-time qualification run.

Reconstruct and evaluate all rows with:

```sh
python3 hack/provider-profiles/evaluate_matrix.py \
  docs/qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/*/provider-qualification.json
```

The evaluator requires the exact eleven-row set, reconstructs each reviewed
bundle, verifies one release identity, checks receipt and evidence-manifest
digests, and enforces the privacy and canonical-profile contracts.
