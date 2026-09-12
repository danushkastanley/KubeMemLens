# Prepare a Node-context provider proposal

`hack/node-qualification/prepare_provider.py` prepares configuration for review.
It does not contact Kubernetes, invoke provider CLIs, execute the supplied
binaries, install resources, start a run or grant approval. The current live
qualification runner remains specific to owned kind fixtures. The proposal
includes the fixed [Kubernetes observer footprint](node-context-kubernetes-observer.md).
Provider execution still needs to bind it to owner-supplied targets before a
provider run is approved.

Use this after selecting a disposable target and obtaining its local kubeconfig
path, exact context, pool, Node addresses, API addresses and public kubelet CA.
It requires Python, Git and Helm, plus locally available source objects and
artefact files. No dependency download or cluster creation is performed.

## Profile selection

| Node-context profile | Existing inventory profile |
| --- | --- |
| `gke-standard` | `gke-cos-containerd-amd64` or `gke-ubuntu-containerd-amd64` |
| `eks-managed-linux` | `eks-al2023-containerd-amd64` |
| `aks-linux` | `aks-ubuntu-containerd-amd64` |
| `self-managed-linux` | `self-managed-containerd` or `self-managed-crio-amd64` |

These are configuration choices, not support claims. Historical cgroup results
do not qualify the Node-context path. Standard deep-mode prerequisites, including
the aggregation-proxy identity constraint, remain mandatory.

## Private configuration

Create a mode-0600 JSON file. Replace every placeholder with the intended target
or actual local artefact. Do not put tokens, passwords, kubeconfig contents or
private keys in this file.

```json
{
  "schemaVersion": 2,
  "inventoryProfile": "gke-cos-containerd-amd64",
  "providerSelectors": {
    "project": "exact-project",
    "location": "exact-location",
    "cluster": "exact-cluster"
  },
  "namespace": "kube-memlens-qualification-example",
  "context": "exact-disposable-context",
  "kubeconfigPath": "/absolute/path/to/kubeconfig",
  "kubernetesVersion": "v1.37.0",
  "poolName": "exact-disposable-pool",
  "imageRepository": "registry.example/team/kube-memlens",
  "imageDigest": "sha256:<64 lowercase hex characters>",
  "sourceCommit": "<40 lowercase hex characters>",
  "chartArchive": "/absolute/path/to/chart.tgz",
  "chartDigest": "sha256:<64 lowercase hex characters>",
  "cliBinary": "/absolute/path/to/kubectl-memlens",
  "cliDigest": "sha256:<64 lowercase hex characters>",
  "producerBinary": "/absolute/path/to/memlens-node-context",
  "producerDigest": "sha256:<64 lowercase hex characters>",
  "kubeletCAFile": "/absolute/path/to/public-kubelet-ca.pem",
  "kubeletAudience": "exact-kubelet-accepted-audience",
  "apiServerCIDRs": ["10.0.0.1/32"],
  "nodeCIDRs": ["10.0.1.2/32", "10.0.1.3/32"]
}
```

Use `null` for `poolName` on self-managed targets. Managed pool selectors use
the provider's existing Kubernetes label. The tool does not add wildcard
tolerations or assume that tainted Nodes are schedulable.

Configuration schema 2 binds the provider CLI selectors into the proposal.
Use `project`, `location` and `cluster` for GKE; `region` and `cluster` for EKS;
`subscription`, `resourceGroup` and `cluster` for AKS; and an empty object for
self-managed targets. Do not reuse a schema-1 configuration without adding the
explicit selectors and preparing a new proposal.

The address examples are placeholders. Supply explicit `/32` or `/128` host
routes, including one selected address per profile Node. Broad networks,
duplicates, loopback, unspecified, link-local and scoped addresses are rejected.
Live inventory must subsequently prove that these addresses match the selected
two-Node pool. Configuration validation alone cannot prove that binding.

```sh
umask 077
python3 hack/node-qualification/prepare_provider.py \
  --profile hack/node-qualification/profiles/gke-standard.json \
  --configuration /absolute/path/to/private-configuration.json \
  --output /absolute/path/to/new-private-proposal
```

The output directory must not exist. The tool verifies local artefact hashes,
the complete chart file set and contents against the source commit, canonical
provider values against that commit, and profile files against the qualification
tool commit. It parses the public CA bundle and rejects private-key material.
It does not prove that the supplied image contains those binaries or belongs to
that source commit; image provenance and runtime identity remain required.

## Review bundle

The new directory uses mode `0700`; each file uses `0600`:

- `baseline-values.json` and `enabled-values.json`: usable Helm overrides with
  the frozen 600-second projected lifetime, pool selector and explicit routes.
- `baseline.preview.yaml` and `enabled.preview.yaml`: resource layouts for
  review only. Generated TLS keys/certificates are replaced by labelled values.
  Do not apply these previews. Helm generates serving material during install
  and reuses the existing Secret during upgrade; verify the live result.
- `serving-trust.json`: the supplied public kubelet CA ConfigMap.
- `workload.json`: the same pinned 32-container workload used by the qualification
  protocol, placed in the dedicated namespace with the selected Linux pool.
- `host-observers.json`: the read-only cgroup DaemonSet and deny-all probe
  NetworkPolicy, using the same selected pool.
- `ephemeral-observers.json`: the agent metrics and producer identity/metrics
  container specifications. These are review fragments for existing Pods, not
  standalone resources to apply.
- `probe-identities.json`: the isolated positive and negative probe identities
  with only the declared Node-object and stats grants.
- `probe-pods.preview.json`: review-only examples of the production producer's
  positive, denied-stats, bad-CA and cross-Node checks. Node placeholders are
  bound to the selected live pool at execution. API and kubelet projections
  have separate audiences.
- `configuration.private.json`: the exact private input configuration.
- `plan.private.json`: version-2 profile/configuration/file digests, explicit
  observation method/image, frozen measurement settings and budgets, required
  live checks and ownership-based cleanup steps.

Keep the entire directory private and out of Git and shared evidence. The
manifest is written last. A failed render may leave a private partial directory;
without `plan.private.json` it is not a prepared proposal. Existing output is
never replaced. A dirty tool checkout is recorded and requires fresh preparation
from an immutable checkout before run approval.

The plan always says `prepared-not-approved`, `providerRunStarted: false` and
`qualified: false`. Approval must cover the exact target, artefacts, rendered
resources, read-only host observation method, provider replacement action and
cleanup. Follow the [qualification protocol](node-context-qualification.md) for
actual measurements, sanitisation, independent review and expiry.

## Execution validation and ownership

The execution modules recheck the exact plan acknowledgement, private file set,
file hashes, clean tool commit, candidate chart source and generated resources.
Rehashing a modified privileged probe does not make it valid: its resource
definition must still match the fixed generator. Kubernetes API TLS is checked
before any provider inventory command. Provider selectors come from the private
proposal, rather than an unrelated current CLI selection.

The live pool must contain exactly the profile's two Linux Nodes, with matching
runtime data, unique identities and the approved host routes. The current AKS
inventory adapter requests this two-Node protocol explicitly. It does not
change the older cgroup profile's unsupported result or its three-Node protocol.
An unavailable aggregation-proxy identity remains a hard prerequisite failure.

Resource cleanup records private UID receipts and sends Kubernetes deletion
preconditions. It refuses replacement objects and retains the parent namespace
when ownership is uncertain. Secret payloads are excluded from the receipts.
The coordinator removes recorded objects directly so Helm cannot delete a
replacement object by name during uninstall.

These are execution components, not a completed provider qualification command.
Provider replacement, NetworkPolicy checks, final evidence assembly and
explicit provider cleanup confirmation must still be connected and verified
before a provider run can be approved.

The recovery component tests source loss, agent restart and collector restart
across both bound Nodes after the fixed measurement windows. Source loss removes
the dedicated producer binding's subject temporarily, requires stable stale
evidence on both Nodes, then restores that exact subject. Mutations test the
resource UID and resource version atomically; restoration refuses an intervening
edit. Workload restarts retain existing Pod-template annotations.

Agent recovery requires new containers and successful snapshot posts on both
Nodes while preserving history generations. Collector recovery requires a new
container and changed history generations. Every event requires fresh reports
from both original Node identities within the frozen recovery budget. These
checks do not establish provider-instance replacement or network isolation.

## Local two-Node integration

`hack/verify-node-context-execution-kind.sh` exercises these components against
two newly created local kind Nodes. It builds a local test image, checks the
production TLS and denial probes, installs the chart, measures the fixed windows,
exercises source loss and agent/collector restart recovery, then removes
UID-owned objects plus the fixture. The dedicated
`kind-137-execution` profile declares the workload and budgets before the run.
This diagnostic does not qualify a managed or self-managed provider row.

```sh
NODE_CONTEXT_ACKNOWLEDGE=create-and-remove-node-context-kind \
NODE_CONTEXT_ARTIFACT_DIR=/absolute/path/to/new-evidence \
hack/verify-node-context-execution-kind.sh
```

Only a local Docker socket is accepted. Serving private keys remain inside the
owned Nodes; public CSRs and certificates use bounded exec streams because
Docker's archive-copy API cannot reliably read kind's tmpfs mounts.
