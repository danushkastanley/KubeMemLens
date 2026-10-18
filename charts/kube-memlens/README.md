# KubeMemLens Helm Chart

This chart installs the KubeMemLens node-local cgroup reader and its bounded, in-memory collector.

KubeMemLens is a terminal-first Kubernetes memory incident explainer. The standard profile reads cgroup v2 memory accounting from a read-only host mount and does not install eBPF programmes, persistent storage, a CRD, external telemetry, or automatic remediation.

## Install v1.0.0

```sh
helm upgrade --install kube-memlens \
  oci://ghcr.io/danushkastanley/charts/kube-memlens \
  --version 1.0.0 \
  --namespace kube-memlens \
  --create-namespace
```

The chart selects the version-matched standard image. Pin `image.digest` with
the complete `sha256:` value from the signed `promotion-subjects.txt` when an
immutable image reference is required.

Essential AWS EKS tests covered inspection, incident capture/replay, access
isolation, recovery and cleanup. [Send feedback through a GitHub issue](https://github.com/danushkastanley/KubeMemLens/issues/new/choose).
See the [support contract](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/compatibility.md)
for the AWS EKS managed Linux profile and permission requirements.

### Release candidate artefacts

The `v1.0.0-rc.3` candidate uses
`oci://ghcr.io/danushkastanley/candidates/1.0.0-rc.3/charts/kube-memlens`
with `--version 1.0.0` and image repository
`ghcr.io/danushkastanley/candidates/1.0.0-rc.3/kube-memlens`.
Candidate installs override the image repository and pin the image digest from
its signed `candidate-manifest.json`. Stable promotion preserves those exact
archive, image and chart bytes.

## Components

- One agent Pod per selected Linux node reads `/sys/fs/cgroup` read-only and maps container cgroups to Kubernetes Pods.
- One collector replica retains bounded current snapshots and short Pod history in memory.
- Authenticated reads, workload metrics and agent writes use the aggregated TLS Service on port `443`; agent health/metrics binds only to Pod-local `127.0.0.1:8082` and is not advertised for remote scraping.
- The collector Pod retains a health-only listener on port `8080`. The production Service exposes neither that listener nor a plaintext ingestion or collector-metrics port.

The collector must remain at one replica because replicas do not share state. The chart rejects other replica counts.

With `networkPolicy.enabled`, the agent, certificate-bootstrap Job and test Job have
explicit ingress-only policies that deny incoming Pod traffic. Their listeners
are loopback-only or absent. These policies also cover their startup on CNIs
that require an explicit policy, such as Amazon VPC CNI in strict mode.
Administrator-defined egress restrictions still apply: permit their Kubernetes
API requests and, for certificate rotation, DNS and the collector's TLS Service.

Failed certificate hooks retain their Jobs and logs for diagnosis until the next
hook attempt or namespace removal. Their RBAC objects carry the Helm release's
ownership annotations so interrupted-install cleanup can verify their owner.

The agent and collector set `GODEBUG=disablethp=1` to limit heap memory overhead
on Linux nodes with transparent huge pages enabled. This process-local Go
setting can trade CPU time for a smaller working set; it does not change host
kernel settings. Recheck it when upgrading Go, because the runtime may remove
this compatibility setting. See the [Go heap tuning guidance](https://go.dev/doc/gc-guide#Linux_transparent_huge_pages).

To place that collector on a specific Linux node or pool, set
`collector.nodeSelector` to matching node labels, such as
`kubernetes.io/hostname: ip-10-0-0-1.ec2.internal`. The Linux selector remains
mandatory; a conflicting OS value is rejected. An unmatched selector leaves the
collector Pending until a matching node is available. This does not change the
agent DaemonSet's separate `agent.nodeSelector`.

The chart ships a strict values schema and a `helm test` Job that checks the collector's TLS Service from a non-root, capability-free Pod. The Job provides controller ownership for network-policy enforcement, with no Job retry and a five-minute deadline. Run it after install, upgrade and rollback:

```sh
helm test kube-memlens --namespace kube-memlens
```

Values from pre-v1 rollback charts such as `agent.ingestionMode`, `agent.collectorURL`, `collector.ingestion.port`, `metrics.serviceAnnotations` and `metrics.serviceMonitor` are no longer part of the chart contract. The strict schema rejects persisted copies. Remove them from saved values files, or use reviewed current values instead of carrying an old release's values into an upgrade.

## Important values

| Value | Default | Purpose |
|---|---|---|
| `image.repository` | `ghcr.io/danushkastanley/kube-memlens` | Release image repository |
| `image.tag` | chart `appVersion` | Mutable development/release tag when no digest is supplied |
| `image.digest` | empty | Preferred immutable image identity |
| `agent.nodeSelector` | `kubernetes.io/os: linux` | Linux-node targeting |
| `agent.tolerations` | empty | Operator-reviewed node-pool tolerations |
| `agent.tokenExpirationSeconds` | `3600` | Projected Pod-bound token lifetime |
| `agent.resources.requests.memory` | `96Mi` | Local `rc-5000` p95-derived agent scheduling request |
| `agent.resources.limits.memory` | `128Mi` | Default per-agent memory ceiling |
| `collector.nodeSelector` | `{}` | Additional placement labels; `kubernetes.io/os: linux` is always required |
| `collector.replicas` | `1` | Required single in-memory collector |
| `collector.read.maxConcurrentRequests` | `4` | Authenticated read admission ceiling; aggregate construction is serialised |
| `collector.ingestion.maxConcurrentRequests` | `4` | Concurrent snapshot decode ceiling |
| `collector.ingestion.requestsPerSecondPerAgent` | `1` | Per-agent sustained ingestion rate |
| `collector.ingestion.burstPerAgent` | `2` | Per-agent ingestion burst |
| `collector.ingestion.maxSnapshotBytes` | `8388608` | Per-node snapshot request ceiling in bytes |
| `collector.resources.requests.memory` | `192Mi` | Local `rc-5000` p95-derived collector scheduling request |
| `collector.resources.limits.memory` | `256Mi` | Default collector memory ceiling |
| `collector.service.extensionPort` | `443` | Authenticated Service and APIService port; targets the collector's named `extension` listener |
| `collector.service.extensionPortName` | `https-extension` | Authenticated Service port name |
| `extensionTLS.rotateBefore` | `720h` | Serving-certificate rotation window |
| `networkPolicy.enabled` | `true` | Cluster-local read and APIService ingress policy |
| `metrics.includeContainers` | `false` | High-cardinality container metrics opt-in |
| `metrics.prometheusRule.enabled` | `false` | Optional recording and alert rules |
| `metrics.grafanaDashboard.enabled` | `false` | Optional dashboard ConfigMap |

See the repository [support and compatibility contract](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/compatibility.md), [installation guide](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/installation.md), [security model](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/security-model.md), and [qualification runbook](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/qualification.md) for the complete contract.

The EKS AL2023 qualification values set the authenticated Service port to `8443`
and its name to `extension`, matching the collector listener for VPC CNI network
policy testing. If changing the listener port, set
`collector.service.extensionPort` to the same number. This updates the APIService
route and Helm connection test too; it does not grant provider qualification.
The default Service remains `443` named `https-extension`. Reverting these values
with a Helm upgrade restores that mapping without changing TLS or RBAC.

## Read access

The chart defines three unbound ClusterRoles:

- `kube-memlens-namespace-viewer`, referenced by a RoleBinding in each namespace an operator may inspect;
- `kube-memlens-cluster-viewer`, explicitly bound to approved cluster operators; and
- `kube-memlens-metrics-reader`, separately bound to an authenticated metrics scraper.

The chart deliberately creates no viewer bindings. Follow the [tenant-scoped read runbook](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/runbooks/tenant-scoped-reads.md) to grant and revoke access. Metrics readers use the aggregated `metrics` resource through the Kubernetes API server; the chart does not render a direct collector `ServiceMonitor`.

## Uninstall

```sh
helm uninstall kube-memlens --namespace kube-memlens --wait
kubectl delete namespace kube-memlens
```

The chart creates no CRDs or persistent volumes. Helm removes the viewer ClusterRoles, including any enabled optional profiles, but bindings created by an administrator are not owned by the release. Remove those bindings before uninstall and confirm all cluster-scoped KubeMemLens RBAC objects are absent as described in the repository runbook.

### Optional Pod volume context

`volumeContext.enabled` defaults to `false`. Set `volumeContext.namespaces` to
existing namespaces to install scoped Pod/PVC acquisition permissions and the
separate, unbound `kube-memlens-volume-viewer` role. Enable
`nodeContext.volumeStats` only with both the volume and verified Node-context
profiles enabled. It shares the existing Summary request and adds no producer
permissions. Namespace viewers need no PV access to read their PVC usage;
PV-derived driver disclosure remains separately authorised. See the
[volume context runbook](../../docs/volume-context.md) for access, bounds and
rollback. CLI/TUI correlation requires both memory and volume viewer
permissions. Default volume captures redact identities.

`volumeContext.health` independently enables CSI health reads and defaults to
`false`. Pod/PVC health uses existing scoped binding reads; the collector receives
an additional CSINode `get` role. The unbound `kube-memlens-volume-backend-viewer`
role grants PV/CSINode reads for operators who need backend detail. It does not
expand the existing namespace viewer. Health reserves16 MiB within the shared
64 MiB volume-retention ceiling; disabling health leaves usage and memory active.

`volumeContext.workloads` defaults to `false`. Enabling it adds bounded live
workload composition, separate namespace acquisition roles and the unbound
`kube-memlens-workload-volume-viewer` role. It does not expand existing viewers.
The route requires snapshot schema 6 and independently checks every Pod and
referenced object using the original caller. See the volume runbook for query
bounds, source uncertainty, capture compatibility and rollback.
