# Existing-Cluster Qualification

This runbook turns managed-provider and runtime compatibility into repeatable evidence for the [support contract](compatibility.md). It does not create a GKE, EKS or AKS cluster, push an image, publish results, or declare support merely because a manifest rendered.

This is maintainer-owned release qualification. Ordinary pull requests use proportionate local and CI checks described in [CONTRIBUTING.md](../CONTRIBUTING.md); contributors do not need managed-provider accounts unless their change makes a provider-specific claim.

Use only a disposable cluster or a cluster whose owner has authorised installation of a read-only hostPath DaemonSet, cluster-scoped read RBAC, a NetworkPolicy and two short-lived probe Pods. Do not treat an alpha qualification run as shared multi-tenant authorisation evidence.

## Scope

`hack/qualify-cluster.sh` verifies:

- the selected provider profile is canonical and the supplied image and packaged
  chart match exact SHA-256 digests;
- the agent covers every Linux node in a dedicated qualification topology with
  exactly one homogeneous Linux pool; an optional Windows pool is permitted for
  the mixed-OS row;
- every Linux executable Pod selects Linux nodes, and the agent retains the
  exact read-only cgroup mount, projected token and non-root, drop-all,
  read-only security posture;
- strict `doctor`, status, mapping, explanation privacy and collector metrics;
- a real 80x24 TUI launch and clean exit;
- elapsed seconds from Helm installation start to the first schema-valid explanation with severity, confidence, caveats and evidence-window metadata;
- CNI enforcement through one denied and one explicitly allowed Pod-to-Pod
  NetworkPolicy probe;
- independent agent and collector restart recovery;
- recovery after one real provider-triggered Linux node replacement;
- one Helm upgrade, rollback and post-rollback strict diagnosis;
- uninstall, cluster-scoped RBAC removal and deletion of the dedicated namespace;
- sanitised, expiry-bound environment evidence without context, cluster, node
  or provider-resource identifiers.

It does not prove long-duration reliability, live scale, every CNI mode, managed-provider compatibility, or optional eBPF support. Provider records will be published together after the GKE, EKS and AKS qualification matrix completes. Run the separate [scale qualification gate](scale-qualification.md) when a release claims a live container profile; neither result substitutes for the other.

## Managed-provider boundary

The standard cgroup collector targets Linux VM node pools in GKE Standard, EKS managed/self-managed node groups and AKS Linux node pools. Support depends on the node exposing read-only `/sys/fs/cgroup`, allowing a DaemonSet and enforcing the rendered NetworkPolicy.

Restricted or serverless modes are separate capabilities, not aliases for their provider:

- GKE Autopilot blocks hostPath access except read-only `/var/log`, so the cgroup agent cannot run there ([GKE Autopilot security](https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-security)).
- EKS Fargate does not support DaemonSets, so the node agent cannot run there ([EKS Fargate considerations](https://docs.aws.amazon.com/eks/latest/userguide/fargate.html)).
- AKS virtual nodes do not receive DaemonSet Pods and have NetworkPolicy limitations ([AKS virtual nodes](https://learn.microsoft.com/en-us/azure/aks/virtual-nodes)).

These environments must be reported as unsupported without a privileged
workaround. A mixed cluster qualifies only when it contains one homogeneous
Linux pool plus the Windows pool being exercised. Another Linux pool makes the
cluster unsuitable for an exact qualification row.

## Prerequisites

- `expect`, `go`, `helm`, `jq`, `kubectl` and `python3` on the operator workstation.
- `gcloud`, `aws` or `az` for its corresponding managed-provider profile, with
  read-only inventory access in addition to the separately approved lifecycle actions.
- `ssh` with a dedicated, least-privilege key and pinned known-hosts file for
  the self-managed cgroup v1 observation.
- An exact kubeconfig context and enough RBAC to create the dedicated namespace and chart resources.
- A release-candidate image repository plus its immutable digest, source commit,
  packaged chart and chart-package digest.
- A clean qualification-tool checkout. Its commit may follow the candidate when
  only qualification code changes; the receipt records it separately and the
  candidate chart, profile values and release identity remain source-bound.
- A checked-in profile from `hack/provider-profiles/`.
- The exact `linux/amd64` probe in `hack/provider-probe-image.json`; its
  repository and digest are source-bound and its digest must match every
  supported row. It contains `/bin/sh`, `wget` and `httpd`.
- A NetworkPolicy-capable CNI with policy enforcement enabled.
- No existing KubeMemLens installation in the target cluster. The chart's current cluster-scoped RBAC names intentionally prevent overlapping qualification.
- Explicit approval and a separate provider cleanup plan for the real node
  replacement and any cloud resources.

Do not put credentials, kubeconfig contents, registry tokens or production workload data in the evidence directory.

## Run

Choose a fresh namespace suffix for each run:

```sh
export QUALIFY_CONTEXT='<exact-context>'
export QUALIFY_NAMESPACE='kube-memlens-qualification-gke-standard'
export QUALIFY_PROFILE="$PWD/hack/provider-profiles/gke-cos-containerd-amd64.json"
export QUALIFY_IMAGE_REPOSITORY='ghcr.io/danushkastanley/kube-memlens'
export QUALIFY_IMAGE_DIGEST='sha256:<64-lowercase-hex-characters>'
export QUALIFY_CHART_ARCHIVE="$PWD/dist/kube-memlens-<version>.tgz"
export QUALIFY_CHART_DIGEST='sha256:<64-lowercase-hex-characters>'
export QUALIFY_SOURCE_COMMIT='<40-lowercase-hex-characters>'
export QUALIFY_PROBE_IMAGE='<trusted-probe-repository>@sha256:<64-lowercase-hex-characters>'
export QUALIFY_ARTIFACT_DIR="$PWD/qualification-evidence/gke-standard"
export QUALIFY_GKE_PROJECT='<private-project-selector>'
export QUALIFY_GKE_LOCATION='<private-location-selector>'
export QUALIFY_GKE_CLUSTER='<private-cluster-selector>'
export QUALIFY_GKE_NODE_POOL='<private-node-pool-selector>'
export QUALIFY_ACKNOWLEDGE='install-test-and-remove-kube-memlens'
export QUALIFY_NODE_REPLACEMENT_ACKNOWLEDGE='provider-action-approved'

make qualify-cluster
```

The runner pauses after the component restart checks and waits for the approved
provider procedure to replace one Linux node. It does not contain provider
credentials or invoke a cloud CLI. The operator performs the already reviewed
replacement separately; the runner detects the changed Node UID privately,
proves DaemonSet and strict-doctor recovery, and discards the identifiers.

Replace exactly one Linux Node in the selected pool. Some providers complete
that as one operation; AKS requires the bounded provider-native delete and
restore sequence below:

| Row | Approved replacement shape |
|---|---|
| GKE Standard | Resolve the selected pool's managed instance group privately, then use `gcloud compute instance-groups managed recreate-instances` for one instance. |
| EKS managed nodes | Resolve one selected-group EC2 instance from the private Node `providerID`, then terminate that instance so the managed node group replaces it. |
| AKS node pools | Start with exactly three Linux Nodes in a fixed-size pool whose `enableAutoScaling` value is `false`. Cordon and drain one selected-pool machine, use `az aks nodepool delete-machines` for that one machine, verify the pool and Kubernetes each contain two Linux Nodes, then use `az aks nodepool scale --node-count 3`. |
| Self-managed | Remove one non-control-plane worker and recreate it from the same reviewed immutable bootstrap configuration and join contract. |

Do not replace a control-plane Node or act on a second Node. Do not resize a
pool except for the exact AKS three-to-two-to-three sequence above. AKS
`delete-machines` does not drain or automatically restore capacity, so verify
the two-Node intermediate state before requesting exactly one replacement.
Abort the row if another Node changes, either AKS count differs from the
expected intermediate or final count, the provider reports a degraded pool, or
the runner reaches its timeout. The runner requires one removed and one added
UID, the same exact runtime tuple, a subsequent fresh snapshot and a refreshed
provider receipt before continuing.

The script refuses an existing namespace, pre-existing KubeMemLens cluster
RBAC, a non-empty or unsafe evidence directory, mutable artefacts, a dirty
qualification-tool checkout or a missing acknowledgement. It verifies the
candidate chart, supported profile, values and probe contract against
`QUALIFY_SOURCE_COMMIT`; a later tool-only commit does not change that release
identity. It cleans up its release, exact
chart RBAC and namespace on success. Failure cleanup distinguishes NotFound
from API, authentication and transport errors, verifies exact absence and marks
the run failed when cleanup cannot be proved.

Managed rows use read-only provider inventory calls before installation. GKE
uses the four `QUALIFY_GKE_*` selectors above; EKS uses
`QUALIFY_EKS_REGION`, `QUALIFY_EKS_CLUSTER` and `QUALIFY_EKS_NODEGROUP`; AKS
uses `QUALIFY_AKS_SUBSCRIPTION`, `QUALIFY_AKS_RESOURCE_GROUP`,
`QUALIFY_AKS_CLUSTER` and `QUALIFY_AKS_NODE_POOL`. These selectors stay in the
process environment and are not copied into evidence. Self-managed rows use
three bounded `kubectl` reads. Each supported profile also has a checked-in
`hack/provider-values/<profile>.yaml` whose digest is bound to the result.

If Linux nodes use custom taints, add only the required tolerations to `agent.tolerations` in a reviewed values file. Do not add a blanket toleration merely to make the qualification pass.

## Unsupported live observations

Unsupported receipts are collected from live provider and Kubernetes reads;
the production CLI has no caller-authored source-file mode:

```sh
python3 hack/provider-inventory/observe_unsupported.py \
  --profile hack/provider-profiles/<unsupported-profile>.json \
  --source-commit '<40-character-commit>' \
  --chart-archive dist/kube-memlens-<version>.tgz \
  --chart-digest 'sha256:<chart-digest>' \
  --output qualification-evidence/<unsupported-profile>/provider-inventory.json
```

The observation command requires a clean qualification-tool checkout, records
its commit, and verifies that the chart archive matches its digest and the
separate release-candidate source commit. All managed observations require
`QUALIFY_CONTEXT` plus the provider selectors
used by the supported collector. Add the profile-specific private inputs:

| Profile | Additional live input |
|---|---|
| GKE Autopilot | `QUALIFY_OBSERVATION_NAMESPACE` naming an existing authorised namespace. The collector checks `auth can-i`, accepts the exact baseline server dry-run and requires the exact cgroup-hostPath Status denial. |
| EKS Fargate | `QUALIFY_EKS_FARGATE_PROFILE` naming an active profile with at least one Ready Fargate Node in its scope. |
| AKS virtual nodes | An enabled ACI connector and at least one Ready Azure virtual-kubelet Node in the selected AKS cluster. The receipt retains the real kubelet version, derives amd64 only from `kubernetes.io/arch`, and records missing architecture plus blank virtual-node OS image, runtime and kernel fields as `unreported`. |
| Windows deep mode | `QUALIFY_AKS_WINDOWS_NODE_POOL` naming a current AKS Windows pool. The live Nodes, provider-owned pool, configured Azure CNI with Calico and rendered candidate agent contract must agree. Cilium is Linux-only and is rejected for this row. The receipt records configuration only and does not claim Windows policy enforcement. |
| cgroup v1 | `QUALIFY_SSH_USER`, `QUALIFY_SSH_ADDRESS_TYPE`, `QUALIFY_SSH_KEY` and `QUALIFY_SSH_KNOWN_HOSTS`. The key must be mode `0600`; strict-host-key, BatchMode SSH checks every Linux Node UID for no `cgroup.controllers` file and a real cgroup v1 mount. |

No SSH address, key path, provider selector, Node UID, raw admission response or
provider response is copied into the receipt. Keep the private command and
cleanup transcript until review, then retain it outside Git according to the
operator's security policy.

Bind the live receipt to the candidate source commit, image, chart archive and
checked-in profile values. The receipt digest separately binds the clean
qualification-tool commit and its canonical unsupported profile:

```sh
python3 hack/provider-profiles/build_unsupported_pending.py \
  --profile hack/provider-profiles/<unsupported-profile>.json \
  --provider-receipt qualification-evidence/<unsupported-profile>/provider-inventory.json \
  --chart-archive dist/kube-memlens-<version>.tgz \
  --chart-digest 'sha256:<chart-digest>' \
  --values hack/provider-values/<unsupported-profile>.yaml \
  --values-digest 'sha256:<values-digest>' \
  --source-commit '<40-character-commit>' \
  --image-digest 'sha256:<image-digest>' \
  --output qualification-evidence/<unsupported-profile>/provider-qualification.pending.json
```

Review unsupported evidence with the exact bundled `provider-inventory.json`
and without `--evidence-manifest`; only supported lifecycle runs have the
six-file manifest.

## Evidence contract

A passing directory contains mode-`0600` JSON:

| File | Purpose |
|---|---|
| `provider-qualification.pending.json` | Unreviewed strict run result with release identity, environment, lifecycle/recovery checks and privacy assertions |
| `provider-inventory.json` | Sanitised provider-owned inventory receipt, qualification-tool commit and canonical receipt digest |
| `evidence-manifest.json` | Exact digests for every retained run file plus the digest-pinned policy probe image |
| `qualification-summary.json` | Compatibility summary for older result consumers |
| `environment.json` | Kubernetes, provider, node image, OS, kernel, runtime, architecture, cgroup, CNI and Linux/Windows counts |
| `recovery.json` | Install-to-explanation plus agent, collector and real-node replacement recovery seconds |
| `lifecycle.json` | Strict, cross-checked facts for every claimed install, runtime, security, CNI, recovery, upgrade, rollback and uninstall check |
| `doctor.json` | Strict checks with connection and node names redacted |
| `status.json` | Bounded store state with connection identifiers redacted |

Validate the pending schema with `hack/provider-profiles/validate.py --pending`.
After inspecting the complete bundle and the private provider/cleanup record,
finalise a supported row with the exact bundle paths:

```sh
python3 hack/provider-profiles/review_result.py \
  --profile hack/provider-profiles/<profile>.json \
  --input <bundle>/provider-qualification.pending.json \
  --output <bundle>/provider-qualification.json \
  --provider-receipt <bundle>/provider-inventory.json \
  --evidence-manifest <bundle>/evidence-manifest.json \
  --acknowledge reviewed-provider-evidence
```

The review step adds `reviewedAt`
and `reviewDueAt`, verifies every manifest-bound file, binds the environment to
the receipt digest, writes
`provider-qualification.json` with mode `0600` and refuses overwrite.
Review must occur within seven days of completion. The 90-day requalification
deadline is anchored to the completion date, so delayed review cannot refresh
old evidence.

The provider matrix is complete only when
`hack/provider-profiles/evaluate_matrix.py` passes all six supported and five
unsupported reviewed records together. It requires one release identity across
the matrix and at least one supported mixed Linux/Windows run whose scheduling
check passed. `qualificationToolCommit` is receipt provenance, not a release
identity field. Existing supported receipt schema v1 and unsupported receipt
schema v2 remain valid; new receipts use supported v2 and unsupported v3.

```sh
python3 hack/provider-profiles/evaluate_matrix.py \
  <matrix-root>/*/provider-qualification.json
```

An unsupported profile is not a failed supported-profile run. After an
authorised provider preflight proves the exact documented restriction, retain
the private provider response and a sanitised observation receipt. A reviewed
`unsupported_confirmed` record must bind that receipt, the provider-owned
inventory and the exact candidate artefacts to the profile's stable reason
code. Caller-supplied reason text or a generic Helm timeout is not evidence.

Before attaching evidence to an issue or pull request, inspect every file
manually. The runner derives provider, node-image and CNI identity from the
sanitised receipt produced by the provider-owned inventory commands in the
[provider source note](provider-qualification-sources.md); a Node `providerID`
prefix is not managed-product proof. Keep the private provider inventory and
cleanup record separate so account and resource identifiers are not published.

## Acceptance record

Add a row to [the support contract](compatibility.md) only after reviewing the evidence and recording:

- Kubernetes, kernel, OS, architecture, container runtime and cgroup v2;
- provider/node-pool type and CNI implementation;
- image and chart version/digest;
- pass/fail for install, node coverage, mapping, NetworkPolicy, explanation, metrics, upgrade, rollback and uninstall;
- duration, date and a durable link to sanitised evidence;
- review date and the profile's 90-day requalification deadline;
- any node selector, toleration, admission-policy or CNI exceptions.

One successful run supports only the tested combination. It is not evidence for every Kubernetes version, node image, CNI or provider mode.

## Publication and provider cleanup

Private provider responses, account selectors, kubeconfigs, node identifiers,
SSH details and cleanup transcripts never enter Git. Before deleting provider
infrastructure, record its run label and a private inventory of clusters, node
pools or groups, instances, disks, load balancers, public IPs, network
interfaces, security groups and provider-managed resource groups. Delete the
labelled resources immediately after their final row, then repeat the provider
inventory until every owned item is absent. An API or authorisation error is not
proof of deletion.

Reviewed public bundles belong under
`docs/qualification-results/provider-runtime-<chart-version>-<source-short-sha>/<profile>/`.
A supported bundle contains the seven manifest-bound run files, the manifest,
the exact pending and reviewed records. An unsupported bundle contains its v2
receipt plus the exact pending and reviewed records; its private raw source is
excluded. Re-run privacy validation, bundle verification and the complete
matrix evaluator from a clean checkout before adding durable links to the
support matrix. Publish all rows together so a partial matrix cannot imply
provider support.
