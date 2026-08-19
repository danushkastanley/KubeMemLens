# Installation, Upgrade, and Uninstall

KubeMemLens has not published its first supported release. Use source installs only for development until a release candidate completes the compatibility matrix.

## Requirements

- A Linux Kubernetes node using cgroup v2 for deep agent mode.
- Kubernetes RBAC permission to install the chart.
- `get` on `services` and `services/proxy` in the KubeMemLens namespace for CLI service-proxy mode.
- A NetworkPolicy-capable CNI to enforce the default ingestion restriction.

The agent is not required for local sample commands.

## Build the CLI from source

```sh
go build -trimpath -o kubectl-memlens ./cmd/kubectl-memlens
./kubectl-memlens version
./kubectl-memlens sample top
```

Putting the binary on `PATH` as `kubectl-memlens` enables `kubectl memlens` discovery.

## Development cluster install

Build and load an image into a disposable local cluster as described in [README.md](../README.md), then install the local chart with an explicit image override:

```sh
helm upgrade --install kube-memlens ./charts/kube-memlens \
  --namespace kube-memlens \
  --create-namespace \
  --set image.repository=kube-memlens \
  --set image.tag=local-smoke \
  --set image.pullPolicy=Never
```

Do not use `collector.replicas` above `1`; the chart rejects this because each collector has an independent in-memory store.

The agent targets `kubernetes.io/os: linux`. Node pools with custom taints require explicitly reviewed `agent.tolerations`; the chart does not grant a blanket toleration by default.

## Release install

After a release is published and verified, the intended OCI chart flow is:

```sh
helm upgrade --install kube-memlens \
  oci://ghcr.io/danushkastanley/charts/kube-memlens \
  --version <version-without-v> \
  --namespace kube-memlens \
  --create-namespace
```

Use an exact version. Do not copy a floating command from an untrusted source. Release archives, checksums, SBOMs, signatures, and provenance will be attached to the corresponding GitHub draft before it is promoted.

For qualification or policy-controlled installations, pin the workload artefact independently of the chart version:

```sh
helm upgrade --install kube-memlens ./charts/kube-memlens \
  --namespace kube-memlens \
  --create-namespace \
  --set-string image.repository=ghcr.io/danushkastanley/kube-memlens \
  --set-string image.digest=sha256:<64-lowercase-hex-characters>
```

`image.digest` takes precedence over `image.tag` and rejects anything other than an exact SHA-256 value. Use [the existing-cluster qualification runbook](qualification.md) before adding a provider/runtime combination to the compatibility matrix.

## Verify an install

```sh
kubectl get daemonset,deployment,service,networkpolicy -n kube-memlens
kubectl rollout status daemonset/kube-memlens-agent -n kube-memlens
kubectl rollout status deployment/kube-memlens-collector -n kube-memlens
kubectl logs -n kube-memlens daemonset/kube-memlens-agent
kubectl logs -n kube-memlens deployment/kube-memlens-collector
kubectl memlens status
kubectl memlens doctor
kubectl memlens top pods -A
kubectl memlens history pod <pod-name> -n <namespace>
```

The collector read port is `8080`; agent ingestion is isolated on `8081`; agent operational metrics use `8082` by default.

## Upgrade

1. Read [CHANGELOG.md](../CHANGELOG.md) and release notes.
2. Export current values:

   ```sh
   helm get values kube-memlens -n kube-memlens -o yaml > kube-memlens-values-backup.yaml
   ```

3. Render and review the new version with the saved values.
4. Run `helm upgrade` with an exact chart version.
5. Wait for both rollouts and exercise `status`, `top`, `explain`, and metrics.

The collector is in-memory. Restarting or upgrading it discards its latest snapshots, bounded history, and event-delta baselines; agents repopulate current state on their next interval. No persisted user data or CRD migration exists.

Current state defaults to at most 5,000 node records and 100,000 container snapshots, with a 16 MiB encoded JSON response ceiling. History defaults to 15 minutes, at most 180 points per Pod instance, 1,000 series in total, and 20 returned instances for one Pod lookup. Tune `collector.store`, `collector.maxResponseBytes`, and `collector.history` only from measured cluster density and within the collector Pod's memory limit. Capacity breaches fail explicitly; they do not silently evict current nodes or truncate API results.

Rollback uses the normal Helm revision path:

```sh
helm history kube-memlens -n kube-memlens
helm rollback kube-memlens <revision> -n kube-memlens
```

## Uninstall

```sh
helm uninstall kube-memlens -n kube-memlens
```

If the namespace was created only for KubeMemLens and contains nothing else, delete it explicitly after inspecting it:

```sh
kubectl get all -n kube-memlens
kubectl delete namespace kube-memlens
```

The chart installs no CRDs or persistent volumes. Uninstall removes the workloads, Services, ServiceAccounts, RBAC binding, and NetworkPolicy managed by the release. The cluster-scoped agent ClusterRole and ClusterRoleBinding should be removed by Helm; verify them if an uninstall was interrupted.
