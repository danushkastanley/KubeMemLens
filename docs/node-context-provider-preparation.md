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
  "schemaVersion": 1,
  "inventoryProfile": "gke-cos-containerd-amd64",
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
