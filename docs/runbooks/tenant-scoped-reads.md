# Tenant-scoped read access

The authenticated chart exposes diagnostic reads only through the Kubernetes aggregated API. Kubernetes authenticates the caller, applies RBAC and forwards the validated identity to KubeMemLens. KubeMemLens delegates the exact resource request again before reading its bounded in-memory store.

NetworkPolicy, Service reachability, a CLI namespace filter and a Kubernetes Service proxy grant are not read authorisation.

## Roles

The chart creates these ClusterRole definitions without binding any principal:

| Role | Scope when bound | Resources |
| --- | --- | --- |
| `kube-memlens-namespace-viewer` | The namespace containing the RoleBinding | Pods, containers, workloads and Pod history |
| `kube-memlens-cluster-viewer` | Cluster | Pods, containers, workloads, Pod history, nodes and cluster status |
| `kube-memlens-metrics-reader` | Cluster | Workload-labelled metrics only |

The cluster-viewer role does not include metrics. None of the roles uses wildcard resources or verbs.

CLI and TUI streaming views use bounded polling. Every refresh makes a new authenticated `list` request, so revoking a binding stops the next refresh. The API does not advertise a Kubernetes `watch` verb.

## Grant namespace access

Confirm the username through the cluster's normal identity provider. Then create one RoleBinding in each approved namespace:

```sh
kubectl create rolebinding kube-memlens-namespace-viewer \
  --namespace '<tenant-namespace>' \
  --clusterrole kube-memlens-namespace-viewer \
  --user '<authenticated-kubernetes-username>'
```

Check the exact permissions without retrieving tenant data:

```sh
kubectl auth can-i list pods.memory.kubememlens.io \
  --namespace '<tenant-namespace>' \
  --as '<authenticated-kubernetes-username>'

kubectl auth can-i list pods.memory.kubememlens.io \
  --all-namespaces \
  --as '<authenticated-kubernetes-username>'
```

The first command should return `yes`; the second should return `no` for a namespace viewer.

## Grant cluster or metrics access

Cluster access is an explicit override:

```sh
kubectl create clusterrolebinding kube-memlens-cluster-viewer \
  --clusterrole kube-memlens-cluster-viewer \
  --user '<approved-cluster-operator>'
```

Use a separate identity and binding for a metrics scraper:

```sh
kubectl create clusterrolebinding kube-memlens-metrics-reader \
  --clusterrole kube-memlens-metrics-reader \
  --serviceaccount '<monitoring-namespace>:<scraper-service-account>'
```

Do not grant either ClusterRole to all authenticated users, all ServiceAccounts or another broad group.

## Expected failure behaviour

- A namespace viewer's all-namespace, cross-namespace, node, cluster-status and metrics requests return forbidden.
- Direct requests for an existing and a missing out-of-scope object have the same public denial class.
- Empty means the authorised namespace currently has no retained records. It is not used for a denial.
- A Kubernetes API or delegated-authorisation failure returns unavailable or forbidden and never opens the health-only listener.
- Namespace capture omits cluster node records. Offline replay reads only the previously authorised local capture.

Read-authorisation logs contain a request identifier, principal type, operation, scope marker, decision and bounded reason. Ingestion completion events also contain bounded status and duration. Neither contains tokens, usernames, tenant names or denied object names.

## Policy and test matrix

| Principal and binding | Own namespace | Other namespace | Cluster resources | Metrics |
| --- | --- | --- | --- | --- |
| Namespace viewer RoleBinding | Pod get/list/history; container and workload list | Forbidden, including existing and missing direct IDs | Forbidden | Forbidden |
| Cluster viewer ClusterRoleBinding | All namespaces | All namespaces | Node get/list and cluster-status get | Forbidden |
| Metrics reader ClusterRoleBinding | No workload reads | No workload reads | No node or status reads | Metrics get |

The tenant-read check covers raw list, direct Pod, history, node, status and metrics routes; CLI namespace and cluster views; comparison; recommendation; capture; polling revocation and re-grant; error-class preservation; and collector-log redaction. The separate [tenant isolation validation](../security/tenant-isolation-validation.md) adds a compromised in-cluster workload, direct Service and Pod listeners, NetworkPolicy removal, delegated-authorisation failure, denial timing, bounded abuse, live RBAC and full retained-evidence scanning. Unit and race tests cover trusted request metadata, authorisation-before-store ordering, scope-bound continuations, bounded page construction, response encoding, immutable read shards, capture scope, forbidden-state clearing and unavailable-state retention.

### Reference result

On 25 August 2026, the disposable kind verification passed on Kubernetes `v1.35.5` with separate short-lived tenant and cluster ServiceAccount identities. Twelve-request samples reported:

| Scope | p50 | p95 | Maximum |
| --- | ---: | ---: | ---: |
| Namespace Pod list | 38.402 ms | 52.520 ms | 52.520 ms |
| Cluster Pod list | 42.388 ms | 48.174 ms | 48.174 ms |

The configured-capacity Go benchmark used 100,000 stored containers on an Apple M4 Pro. For the same sparse 1,000-container namespace, the legacy materialise-and-sort baseline measured `1.90 ms/op` and `3,723,848 B/op`; the authenticated 500-item keyset page measured `3.80 ms/op` and `736,160 B/op`. A worst-case 500-item cluster page over all 100,000 records measured `29.15 ms/op` and `881,376 B/op`. The end-to-end authenticated metrics handler, including bounded render and JSON encoding, measured `24.49 ms/op` and `13,181,604 B/op` while counting all 100,000 entities without retaining full nested Pod or container views. The added scan time is the deliberate cost of bounding retained request memory.

## Disposable kind verification

Run the multi-user check only against a disposable or explicitly authorised kind cluster:

```sh
TENANT_READ_KUBECONFIG='<path-to-kind-kubeconfig>' \
TENANT_READ_CONTEXT='<kind-context>' \
TENANT_READ_NAMESPACE='kube-memlens' \
TENANT_READ_CLI='<path-to-kubectl-memlens>' \
TENANT_READ_ARTIFACT_DIR='<evidence-directory>' \
TENANT_READ_ACKNOWLEDGE='run-and-clean-tenant-read-verification' \
make verify-tenant-scoped-reads-kind
```

The check refuses pre-existing fixture names. It creates two namespaces, exercises two namespace users through Kubernetes impersonation, and uses short-lived bound ServiceAccount tokens for the CLI tenant and explicit cluster-operator paths. Temporary kubeconfigs are mode `0600`, credentials are never printed, and cleanup removes every cluster resource and local credential it creates. The evidence contains only sanitised latency and policy results.

## Revoke and uninstall

Delete the binding that granted access. A new request is denied through Kubernetes RBAC and the extension server's uncached delegated decision:

```sh
kubectl delete rolebinding kube-memlens-namespace-viewer \
  --namespace '<tenant-namespace>'
```

Before uninstall, locate administrator-owned bindings that reference the chart roles:

```sh
kubectl get rolebindings --all-namespaces -o json |
  jq -r '.items[] | select(.roleRef.name | startswith("kube-memlens-")) | [.metadata.namespace, .metadata.name, .roleRef.name] | @tsv'

kubectl get clusterrolebindings -o json |
  jq -r '.items[] | select(.roleRef.name | startswith("kube-memlens-")) | [.metadata.name, .roleRef.name] | @tsv'
```

Helm removes only resources it owns. Delete approved external bindings deliberately, then uninstall. If a security rollback is required, make the API unavailable; do not restore the unauthenticated Service read route.
