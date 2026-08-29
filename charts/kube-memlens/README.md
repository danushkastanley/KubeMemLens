# KubeMemLens Helm Chart

This chart installs the KubeMemLens node-local cgroup reader and its bounded, in-memory collector.

KubeMemLens is a terminal-first Kubernetes memory incident explainer. The standard profile reads cgroup v2 memory accounting from a read-only host mount and does not install eBPF programmes, persistent storage, a CRD, external telemetry, or automatic remediation.

## Install

### Candidate prerelease

Create the namespace and install the published `v1.0.0-rc.1` prospective stable
chart from its version-scoped candidate repository. Pin the candidate image
repository and digest from the signed `candidate-manifest.json`:

```sh
kubectl create namespace kube-memlens

helm upgrade --install kube-memlens \
  oci://ghcr.io/danushkastanley/candidates/1.0.0-rc.1/charts/kube-memlens \
  --version 1.0.0 \
  --namespace kube-memlens \
  --set-string image.repository=ghcr.io/danushkastanley/candidates/1.0.0-rc.1/kube-memlens \
  --set-string image.digest=sha256:a5963f8bb8e68359cd3648cf9c6064309772b3794a5bf51547ff314ba559e809 \
  --wait
```

The manifest digest already includes its `sha256:` prefix. It takes precedence over `image.tag` and must resolve to 64 lowercase hexadecimal characters after the prefix.

### Stable release (not published)

The production repository is not valid yet. Stable `v1.0.0` requires a separate
decision and exact-tag approval. If it is later published, it must contain the
same chart bytes:

```sh
helm upgrade --install kube-memlens \
  oci://ghcr.io/danushkastanley/charts/kube-memlens \
  --version 1.0.0 \
  --namespace kube-memlens \
  --set-string image.digest=<complete-promoted-image-digest> \
  --wait
```

The legacy `v0.0.1-alpha.3` release is not supported as a shared multi-tenant
service. The candidate has passed the local adversarial isolation gate for
authenticated writes and exactly delegated reads. Exact provider and
enforcing-CNI evidence still bounds that support claim. Run the qualification
procedure before evaluating the chart on a managed cluster. The chart targets
compatible Linux cgroup v2 nodes; provider-restricted, serverless, Windows, and
cgroup v1 nodes are not silently treated as supported.

## Components

- One agent Pod per selected Linux node reads `/sys/fs/cgroup` read-only and maps container cgroups to Kubernetes Pods.
- One collector replica retains bounded current snapshots and short Pod history in memory.
- Authenticated reads, workload metrics and agent writes use the aggregated TLS Service on port `443`; agent health/metrics binds only to Pod-local `127.0.0.1:8082` and is not advertised for remote scraping.
- The collector Pod retains a health-only listener on port `8080`. The production Service exposes neither that listener nor a plaintext ingestion or collector-metrics port.

The collector must remain at one replica because replicas do not share state. The chart rejects other replica counts.

The chart ships a strict values schema and a `helm test` hook that checks the collector's TLS Service from a non-root, capability-free Pod. Run it after install, upgrade and rollback:

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
| `collector.replicas` | `1` | Required single in-memory collector |
| `collector.read.maxConcurrentRequests` | `4` | Authenticated read admission ceiling; aggregate construction is serialised |
| `collector.ingestion.maxConcurrentRequests` | `4` | Concurrent snapshot decode ceiling |
| `collector.ingestion.requestsPerSecondPerAgent` | `1` | Per-agent sustained ingestion rate |
| `collector.ingestion.burstPerAgent` | `2` | Per-agent ingestion burst |
| `collector.ingestion.maxSnapshotBytes` | `8388608` | Per-node snapshot request ceiling in bytes |
| `collector.resources.requests.memory` | `192Mi` | Local `rc-5000` p95-derived collector scheduling request |
| `collector.resources.limits.memory` | `256Mi` | Default collector memory ceiling |
| `extensionTLS.rotateBefore` | `720h` | Serving-certificate rotation window |
| `networkPolicy.enabled` | `true` | Cluster-local read and APIService ingress policy |
| `metrics.includeContainers` | `false` | High-cardinality container metrics opt-in |
| `metrics.prometheusRule.enabled` | `false` | Optional recording and alert rules |
| `metrics.grafanaDashboard.enabled` | `false` | Optional dashboard ConfigMap |

See the repository [support and compatibility contract](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/compatibility.md), [installation guide](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/installation.md), [security model](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/security-model.md), and [qualification runbook](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/qualification.md) for the complete contract.

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

The chart creates no CRDs or persistent volumes. Helm removes the three viewer ClusterRoles, but bindings created by an administrator are not owned by the release. Remove those bindings before uninstall and confirm all cluster-scoped KubeMemLens RBAC objects are absent as described in the repository runbook.
