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
- `network-probes.preview.json`: the token-free controlled targets, clients and
  temporary allow rules for ingress and egress verification. Node aliases are
  replaced only with the bound live Node names. The serving targets use Jobs to
  prevent the producer DaemonSet adopting their matching labels.
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
Provider-instance replacement, candidate artefact identity, the provider command,
final evidence assembly and explicit provider cleanup confirmation must still be
completed before a provider run can be approved. Each provider's CNI behaviour
also requires its own live evidence.

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

The saved field summary requires each selected Node to report a field before
marking it available for the pool. Measured zero remains available; a missing
field on either Node stays unreported. Mixed or unknown source provenance is
reported as unknown. Raw values and Node identifiers are omitted from this
summary.

For a shorter API failure investigation, set
`NODE_CONTEXT_EXECUTION_MODE=api-reads` on the same command. It prepares the owned
baseline fixture and runs 40 concurrent container-read batches across both
Nodes. Its output is a diagnostic with `qualified: false`; it has no fixed-window
measurements or recovery result. The normal mode still runs the full protocol.
API failures report fixed authentication, permission, HTTP availability,
deadline, transport or response categories without retaining response bodies,
addresses or credential-plugin diagnostics. Failures remain failures; this
diagnostic adds no retry or tolerance to qualification.

## Local NetworkPolicy verification

Set `NODE_CONTEXT_EXECUTION_MODE=network-policy` to create the dedicated
`kind-136-network` fixture. It uses the pinned Kubernetes 1.36.1 image and Cilium
1.20.1, then runs the same fixed measurement windows and recovery checks before
testing ingress and egress. The Cilium chart is pulled by OCI digest, checked
against its package checksum, rendered and checked for the exact pinned image
inventory before installation. Its configuration and image references are
recorded in the diagnostic.

Cilium is trusted local test infrastructure: its system workloads configure the
owned Nodes' network and BPF state with the chart's host access. The application
and traffic probes retain their existing least-privilege settings. Hubble and
the L7 proxy are disabled in this fixture; probe evidence contains only fixed
outcomes and public artefact identities.

The ingress target inherits the actual producer policy selector. Egress probes
run from the real producer's existing observer and target a controlled Pod on
the other Node. Each direction requires successful traffic before the temporary
allow is removed, blocked traffic while it is removed, and successful traffic
after restoration. The target is checked locally throughout. UID and resource
version guards protect the temporary rules; the chart's policy stays intact.
Probe requests carry no credentials and retain at most 64 response bytes.
After the controls, both original producers must still supply new fresh reports
within the existing recovery budget. API omission of empty policy directions is
normalised for comparison; an added permission or changed selector still fails.

This verifies the declared policy behaviour, with own-Node isolation explicitly
unclaimed. The [Kubernetes NetworkPolicy documentation](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
describes the local-Node exception. This local Cilium configuration enables
`policyCIDRMatchMode: [nodes]` so the chart's host routes can match Node addresses;
see [Cilium's CIDR behaviour](https://docs.cilium.io/en/stable/security/policy/layer3/#selecting-pods-or-nodes-with-cidr-ipblock).
The installer accepts only the explicit local kind target and rejects an
existing CNI. It does not alter a provider's CNI or establish a provider support
row. The fixture is removed after verification.
