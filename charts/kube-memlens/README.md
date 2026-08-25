# KubeMemLens Helm Chart

This chart installs the KubeMemLens node-local cgroup reader and its bounded, in-memory collector.

KubeMemLens is a terminal-first Kubernetes memory incident explainer. The standard profile reads cgroup v2 memory accounting from a read-only host mount and does not install eBPF programmes, persistent storage, a CRD, external telemetry, or automatic remediation.

## Install

Create the namespace and install an immutable release image:

```sh
kubectl create namespace kube-memlens

helm upgrade --install kube-memlens \
  oci://ghcr.io/danushkastanley/charts/kube-memlens \
  --version <version> \
  --namespace kube-memlens \
  --set-string image.digest=sha256:<release-image-digest> \
  --wait
```

The digest must be 64 lowercase hexadecimal characters. It takes precedence over `image.tag`.

The current alpha is not yet supported as a shared multi-tenant service. Agent writes and operator reads use Kubernetes request-header authentication and exact delegated authorisation, but the separate adversarial isolation gate must pass before that support claim changes. Run the qualification procedure before evaluating the chart on a managed cluster. The chart targets compatible Linux cgroup v2 nodes; provider-restricted, serverless, Windows, and cgroup v1 nodes are not silently treated as supported.

## Components

- One agent Pod per selected Linux node reads `/sys/fs/cgroup` read-only and maps container cgroups to Kubernetes Pods.
- One collector replica retains bounded current snapshots and short Pod history in memory.
- Authenticated reads, workload metrics and agent writes use the aggregated TLS Service on port `443`; agent health/metrics uses port `8082`.
- The collector Pod retains a health-only listener on port `8080`. The default Service exposes neither that listener nor plaintext ingestion port `8081`.

The collector must remain at one replica because replicas do not share state. The chart rejects other replica counts.

## Important values

| Value | Default | Purpose |
|---|---|---|
| `image.repository` | `ghcr.io/danushkastanley/kube-memlens` | Release image repository |
| `image.tag` | chart `appVersion` | Mutable development/release tag when no digest is supplied |
| `image.digest` | empty | Preferred immutable image identity |
| `agent.nodeSelector` | `kubernetes.io/os: linux` | Linux-node targeting |
| `agent.tolerations` | empty | Operator-reviewed node-pool tolerations |
| `agent.ingestionMode` | `authenticated` | Kubernetes aggregated ingestion; `legacy` is for controlled pre-v1 rollback only |
| `agent.tokenExpirationSeconds` | `3600` | Projected Pod-bound token lifetime |
| `collector.replicas` | `1` | Required single in-memory collector |
| `collector.read.maxConcurrentRequests` | `4` | Authenticated read admission ceiling; aggregate construction is serialised |
| `collector.ingestion.maxConcurrentRequests` | `4` | Concurrent snapshot decode ceiling |
| `collector.ingestion.requestsPerSecondPerAgent` | `1` | Per-agent sustained ingestion rate |
| `collector.ingestion.burstPerAgent` | `2` | Per-agent ingestion burst |
| `extensionTLS.rotateBefore` | `720h` | Serving-certificate rotation window |
| `networkPolicy.enabled` | `true` | Cluster-local read and APIService ingress policy |
| `metrics.includeContainers` | `false` | High-cardinality container metrics opt-in |
| `metrics.serviceMonitor.enabled` | `false` | Direct Prometheus Operator integration in explicit legacy mode only |
| `metrics.prometheusRule.enabled` | `false` | Optional recording and alert rules |
| `metrics.grafanaDashboard.enabled` | `false` | Optional dashboard ConfigMap |

See the repository [support and compatibility contract](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/compatibility.md), [installation guide](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/installation.md), [security model](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/security-model.md), and [qualification runbook](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/qualification.md) for the complete contract.

## Read access

The chart defines three unbound ClusterRoles:

- `kube-memlens-namespace-viewer`, referenced by a RoleBinding in each namespace an operator may inspect;
- `kube-memlens-cluster-viewer`, explicitly bound to approved cluster operators; and
- `kube-memlens-metrics-reader`, separately bound to an authenticated metrics scraper.

The chart deliberately creates no viewer bindings. Follow the [tenant-scoped read runbook](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/runbooks/tenant-scoped-reads.md) to grant and revoke access. The legacy direct ServiceMonitor integration is available only with `agent.ingestionMode=legacy`; authenticated installations must read the aggregated `metrics` resource through the Kubernetes API server.

## Uninstall

```sh
helm uninstall kube-memlens --namespace kube-memlens --wait
kubectl delete namespace kube-memlens
```

The chart creates no CRDs or persistent volumes. Helm removes the three viewer ClusterRoles, but bindings created by an administrator are not owned by the release. Remove those bindings before uninstall and confirm all cluster-scoped KubeMemLens RBAC objects are absent as described in the repository runbook.
