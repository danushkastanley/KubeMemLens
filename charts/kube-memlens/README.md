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

Run the qualification procedure before installing into a shared or managed production cluster. The chart targets compatible Linux cgroup v2 nodes; provider-restricted, serverless, Windows, and cgroup v1 nodes are not silently treated as supported.

## Components

- One agent Pod per selected Linux node reads `/sys/fs/cgroup` read-only and maps container cgroups to Kubernetes Pods.
- One collector replica retains bounded current snapshots and short Pod history in memory.
- Read/metrics traffic uses port `8080`; agent ingestion uses port `8081`; agent health/metrics uses port `8082`.
- A default NetworkPolicy permits ingestion only from labelled KubeMemLens agents and keeps the collector cluster-internal.

The collector must remain at one replica because replicas do not share state. The chart rejects other replica counts.

## Important values

| Value | Default | Purpose |
|---|---|---|
| `image.repository` | `ghcr.io/danushkastanley/kube-memlens` | Release image repository |
| `image.tag` | chart `appVersion` | Mutable development/release tag when no digest is supplied |
| `image.digest` | empty | Preferred immutable image identity |
| `agent.nodeSelector` | `kubernetes.io/os: linux` | Linux-node targeting |
| `agent.tolerations` | empty | Operator-reviewed node-pool tolerations |
| `collector.replicas` | `1` | Required single in-memory collector |
| `networkPolicy.enabled` | `true` | Ingestion isolation |
| `metrics.includeContainers` | `false` | High-cardinality container metrics opt-in |
| `metrics.serviceMonitor.enabled` | `false` | Optional Prometheus Operator integration |
| `metrics.prometheusRule.enabled` | `false` | Optional recording and alert rules |
| `metrics.grafanaDashboard.enabled` | `false` | Optional dashboard ConfigMap |

See the repository [installation guide](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/installation.md), [security model](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/security-model.md), and [qualification runbook](https://github.com/danushkastanley/KubeMemLens/blob/main/docs/qualification.md) for the complete contract.

## Uninstall

```sh
helm uninstall kube-memlens --namespace kube-memlens --wait
kubectl delete namespace kube-memlens
```

The chart creates no CRDs or persistent volumes. Confirm the cluster-scoped KubeMemLens RBAC objects are absent after uninstall as described in the repository runbook.
