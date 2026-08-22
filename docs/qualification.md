# Existing-Cluster Qualification

This runbook turns managed-provider and runtime compatibility into repeatable evidence. It does not create a GKE, EKS or AKS cluster, push an image, publish results, or declare support merely because a manifest rendered.

This is maintainer-owned release qualification. Ordinary pull requests use proportionate local and CI checks described in [CONTRIBUTING.md](../CONTRIBUTING.md); contributors do not need managed-provider accounts unless their change makes a provider-specific claim.

Use only a disposable cluster or a cluster whose owner has authorised installation of a read-only hostPath DaemonSet, cluster-scoped read RBAC, a NetworkPolicy and two short-lived probe Pods.

## Scope

`hack/qualify-cluster.sh` verifies:

- the supplied image is pinned and actually running by SHA-256 digest;
- the agent covers every schedulable Linux node or fails with a toleration instruction;
- all KubeMemLens Pods retain the expected non-root, drop-all, read-only security posture;
- strict `doctor`, status, mapping, explanation privacy and collector metrics;
- elapsed seconds from Helm installation start to the first schema-valid explanation with severity, confidence, caveats and evidence-window metadata;
- an ordinary Pod can reach the read-only service port but cannot reach the ingestion port;
- one Helm upgrade, rollback and post-rollback strict diagnosis;
- uninstall, cluster-scoped RBAC removal and deletion of the dedicated namespace;
- sanitised environment evidence without context, cluster, node or provider-resource identifiers.

It does not prove long-duration reliability, every CNI mode, managed-provider compatibility, or optional eBPF support. Provider records will be published together after the GKE, EKS and AKS qualification matrix completes.

## Managed-provider boundary

The standard cgroup collector targets Linux VM node pools in GKE Standard, EKS managed/self-managed node groups and AKS Linux node pools. Support depends on the node exposing read-only `/sys/fs/cgroup`, allowing a DaemonSet and enforcing the rendered NetworkPolicy.

Restricted or serverless modes are separate capabilities, not aliases for their provider:

- GKE Autopilot blocks hostPath access except read-only `/var/log`, so the cgroup agent cannot run there ([GKE Autopilot security](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-security)).
- EKS Fargate does not support DaemonSets, so the node agent cannot run there ([EKS Fargate considerations](https://docs.aws.amazon.com/eks/latest/userguide/fargate.html)).
- AKS virtual nodes do not receive DaemonSet Pods and have NetworkPolicy limitations ([AKS virtual nodes](https://learn.microsoft.com/en-us/azure/aks/virtual-nodes)).

These environments must be reported as unsupported without a privileged workaround. A mixed cluster can still qualify its compatible Linux node pool only when node selection and expected coverage are explicit.

## Prerequisites

- `go`, `helm`, `jq` and `kubectl` on the operator workstation.
- An exact kubeconfig context and enough RBAC to create the dedicated namespace and chart resources.
- A published or private KubeMemLens image repository plus its immutable digest.
- A digest-pinned, trusted probe image containing `/bin/sh` and `wget`.
- A NetworkPolicy-capable CNI with policy enforcement enabled.
- No existing KubeMemLens installation in the target cluster. The chart's current cluster-scoped RBAC names intentionally prevent overlapping qualification.

Do not put credentials, kubeconfig contents, registry tokens or production workload data in the evidence directory.

## Run

Choose a fresh namespace suffix for each run:

```sh
export QUALIFY_CONTEXT='<exact-context>'
export QUALIFY_NAMESPACE='kube-memlens-qualification-gke-standard'
export QUALIFY_IMAGE_REPOSITORY='ghcr.io/danushkastanley/kube-memlens'
export QUALIFY_IMAGE_DIGEST='sha256:<64-lowercase-hex-characters>'
export QUALIFY_PROBE_IMAGE='<trusted-probe-repository>@sha256:<64-lowercase-hex-characters>'
export QUALIFY_ARTIFACT_DIR="$PWD/qualification-evidence/gke-standard"
export QUALIFY_ACKNOWLEDGE='install-and-remove-kube-memlens'

make qualify-cluster
```

The script refuses an existing namespace, pre-existing KubeMemLens cluster RBAC, a non-empty or unsafe evidence directory, a mutable image reference, or a missing acknowledgement. It cleans up its release, exact chart RBAC and namespace on success and attempts the same cleanup on failure.

If Linux nodes use custom taints, add only the required tolerations to `agent.tolerations` in a reviewed values file. Do not add a blanket toleration merely to make the qualification pass.

## Evidence contract

A passing directory contains mode-`0600` JSON and probe logs:

| File | Purpose |
|---|---|
| `qualification-summary.json` | Final outcome, immutable digest, redacted registry, NetworkPolicy result and install-to-first-valid-explanation seconds |
| `environment.json` | Kubernetes version and grouped OS/kernel/runtime profiles without node names |
| `doctor.json` | Strict checks with connection and node names redacted |
| `status.json` | Bounded store state with connection identifiers redacted |
| `explanation.json` | Machine explanation privacy contract exercised on the qualification collector |
| `qualification-*.log` | Allowed-read and denied-ingestion probe output |

Before attaching evidence to an issue or pull request, inspect every file manually. Provider family is inferred from the Node `providerID` prefix and deliberately recorded as, for example, `gke-or-gce`; it is not proof that a specific managed product was used. Link provider-owned CI or cluster inventory evidence separately without exposing account or resource identifiers.

## Acceptance record

Add a row to [the compatibility matrix](compatibility.md) only after reviewing the evidence and recording:

- Kubernetes, kernel, OS, architecture, container runtime and cgroup v2;
- provider/node-pool type and CNI implementation;
- image and chart version/digest;
- pass/fail for install, node coverage, mapping, NetworkPolicy, explanation, metrics, upgrade, rollback and uninstall;
- duration, date and a durable link to sanitised evidence;
- any node selector, toleration, admission-policy or CNI exceptions.

One successful run supports only the tested combination. It is not evidence for every Kubernetes version, node image, CNI or provider mode.
