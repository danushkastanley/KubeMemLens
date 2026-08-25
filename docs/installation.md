# Installation, Upgrade, and Uninstall

KubeMemLens is in alpha and has no supported production release. Alpha artefacts are for evaluation on disposable or explicitly authorised clusters. Use exact versions and review the [support and compatibility contract](compatibility.md) and release assets before installation.

Do not claim shared multi-tenant support for the current alpha until the separate adversarial isolation gate passes. The chart routes agent writes and operator reads through the authenticated Kubernetes aggregated API and denies direct workload reads in its secure profile.

## Requirements

- A Linux Kubernetes node using cgroup v2 for deep agent mode.
- Kubernetes RBAC permission to install the chart.
- A namespace viewer RoleBinding in every namespace the operator may inspect, or an explicit cluster-viewer binding.
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

For a published and verified alpha, use the exact OCI chart version:

```sh
helm upgrade --install kube-memlens \
  oci://ghcr.io/danushkastanley/charts/kube-memlens \
  --version 0.0.1-alpha.3 \
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

`image.digest` takes precedence over `image.tag` and rejects anything other than an exact SHA-256 value. Use [the existing-cluster qualification runbook](qualification.md) before adding a provider/runtime combination to the support contract.

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

`status`, strict `doctor` and `-A` examples require the explicit cluster-viewer binding. A namespace viewer should verify with `kubectl memlens top pods -n <tenant-namespace>` and a Pod/history action in that same namespace.

`status` reports the collector evidence state, generation, expected, fresh, stale and missing node counts, and history reset state. A successful connection can still report `rebuilding`, `degraded` or `stale`. Treat the install as populated only after the intended nodes have fresh evidence. The [reliability contract](reliability.md) defines each state and the [reliability runbook](runbooks/reliability.md) covers recovery checks.

Authenticated reads and agent ingestion enter through the Kubernetes API server and TLS Service port `443`. Agent operational metrics bind only to `127.0.0.1:8082` inside each agent Pod and are not advertised for remote scraping. The collector Pod keeps port `8080` for health probes only. The production Service exposes neither port `8080` nor a plaintext ingestion or collector-metrics port.

The chart creates unbound namespace-viewer, cluster-viewer and metrics-reader ClusterRoles. It never chooses principals on the administrator's behalf. To grant one user access to one namespace:

The collector ServiceAccount has `list` on core `nodes` so it can distinguish a missing agent from a Node Kubernetes has removed. It uses the same `agent.nodeSelector` and configured tolerations as the DaemonSet, and pages results within `collector.store.maxNodes`. It does not read Node proxies, logs, secrets or workload objects through this permission. The readiness contract fails closed if this bounded inventory cannot refresh.

```sh
kubectl create rolebinding kube-memlens-namespace-viewer \
  --namespace <tenant-namespace> \
  --clusterrole kube-memlens-namespace-viewer \
  --user <authenticated-kubernetes-username>
```

Grant cluster-wide reads only after explicit review:

```sh
kubectl create clusterrolebinding kube-memlens-cluster-viewer \
  --clusterrole kube-memlens-cluster-viewer \
  --user <approved-cluster-operator>
```

Workload-labelled metrics require a separate binding to `kube-memlens-metrics-reader`. See the [tenant-scoped read runbook](runbooks/tenant-scoped-reads.md) for policy checks, revocation and verification.

## Upgrade

1. Read [CHANGELOG.md](../CHANGELOG.md) and release notes.
2. Export current values:

   ```sh
   helm get values kube-memlens -n kube-memlens -o yaml > kube-memlens-values-backup.yaml
   ```

3. Render and review the new version with the saved values.
4. Run `helm upgrade` with an exact chart version.
5. Wait for both rollouts and exercise `status`, `top`, `explain`, and metrics.

The collector is single-replica and in-memory. Its Deployment uses `Recreate`, so an upgrade drains and stops the old generation before starting the new one. The resulting availability gap prevents traffic from crossing independent stores and epochs. Restarting or upgrading discards latest snapshots, bounded history and event-delta baselines. The new generation reports `rebuilding` until agents post again. It is not highly available and has no persisted user data or CRD migration. The [support contract](compatibility.md#availability-and-history) and [reliability contract](reliability.md) define this boundary.

Current state defaults to at most 5,000 node records and 100,000 container snapshots, with 500 items per keyset page, a 16 MiB encoded JSON response ceiling and four admitted authenticated reads. Pod and workload pages build only selected identities and reserve half of the response ceiling for nested container evidence; aggregate construction is serialised. History defaults to 15 minutes, at most 181 points per Pod instance, 1,000 series in total and 20 returned instances for one Pod lookup. Tune `collector.store`, `collector.read`, `collector.maxResponseBytes` and `collector.history` only from measured cluster density and within the collector Pod's memory limit. Capacity breaches fail explicitly; they do not silently evict current nodes or truncate API results.

These values are safety ceilings, not a sizing recommendation. The declared 5,000-container RC profile remains unqualified. Use the [scale qualification gate](scale-qualification.md) before publishing a live capacity or resource recommendation, and re-run it when the agent interval, limits, workload density or cluster shape changes.

Rollback uses the normal Helm revision path:

```sh
helm history kube-memlens -n kube-memlens
helm rollback kube-memlens <revision> -n kube-memlens
```

Rollback also resets collector history. Follow the [reliability runbook](runbooks/reliability.md#roll-back) and verify a new collection timestamp before treating the rollback as recovered.

## Uninstall

```sh
helm uninstall kube-memlens -n kube-memlens
```

If the namespace was created only for KubeMemLens and contains nothing else, delete it explicitly after inspecting it:

```sh
kubectl get all -n kube-memlens
kubectl delete namespace kube-memlens
```

The chart installs no CRDs or persistent volumes. Uninstall removes the workloads, Services, ServiceAccounts, RBAC objects and NetworkPolicy managed by the release. RoleBindings or ClusterRoleBindings created separately by an administrator are not Helm-owned; remove them before uninstall. Verify that the agent, namespace-viewer, cluster-viewer and metrics-reader ClusterRoles are absent if an uninstall was interrupted.
