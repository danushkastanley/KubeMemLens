# Authentication and authorisation architecture

Status: accepted design; PROD-003 agent ingestion implemented, tenant reads pending
Decision: [ADR 0004](../adr/0004-use-kubernetes-aggregation-for-authentication.md)
Implementation owners: PROD-003, PROD-004 and PROD-005

## Security outcome

KubeMemLens v1 uses the Kubernetes API server as the only production entry point for diagnostic reads and agent writes. The API server authenticates every principal. Kubernetes RBAC and a delegated `SubjectAccessReview` both need to allow the exact operation.

The collector rejects direct identity headers, unauthenticated HTTP and any request whose namespace, resource, node or agent instance does not match the authenticated identity.

## Principals

| Principal | Credential and transport | Identity used by KubeMemLens | Allowed scope |
| --- | --- | --- | --- |
| Namespace operator | Existing kubeconfig credential to the Kubernetes API server | Forwarded username, groups and extras after aggregation proxy validation | Only namespaces with a bound KubeMemLens viewer Role |
| Cluster operator | Existing kubeconfig credential to the Kubernetes API server | Forwarded username, groups and extras after aggregation proxy validation | All namespaces plus node and cluster-status resources; metrics require a separate binding |
| Automation reader | Kubernetes user or dedicated ServiceAccount through the Kubernetes API server | Same delegated identity path as a human operator | The Role or ClusterRole bound to that account |
| Node agent | Kubelet-rotated, Pod-bound `kube-memlens-agent` ServiceAccount token | ServiceAccount username plus authenticated Pod UID, node name, node UID and credential ID extras | Get ingestion epoch and create one snapshot for its authenticated node |
| Metrics scraper | Dedicated ServiceAccount through the Kubernetes API server | ServiceAccount username | Get the cluster-scoped metrics resource only |
| Extension server | Explicit projected ServiceAccount token | KubeMemLens collector ServiceAccount | Read aggregation authentication ConfigMap and create SubjectAccessReviews |
| Certificate bootstrap job | Short-lived projected ServiceAccount token | Bootstrap ServiceAccount | Reconcile one TLS Secret and one APIService, then terminate |

KubeMemLens does not issue human credentials, store kubeconfig tokens or integrate directly with cloud identity providers. GKE, EKS, AKS and self-managed authentication continue to terminate at their Kubernetes API server.

## API resources and policy

The production group is `memory.kubememlens.io/v1alpha1`. These are virtual resources backed by the bounded in-memory store. They are not CRDs and are not persisted in etcd.

| Resource or path | Scope | Verbs | Principal | Server decision |
| --- | --- | --- | --- | --- |
| API discovery | Cluster | `get` | Authenticated Kubernetes clients | API server and extension discovery policy |
| `pods` | Namespace | `get`, `list` | Namespace or cluster viewer | Filter by authorised request namespace before store lookup |
| `pods/history` | Namespace | `get` | Namespace or cluster viewer | Require Pod name and the same namespace decision as the parent Pod |
| `containers` | Namespace | `list` | Namespace or cluster viewer | Never return a container outside the authorised namespace |
| `workloads` | Namespace | `list` | Namespace or cluster viewer | Aggregate only authorised Pods in the request namespace |
| `pods` without a namespace | Cluster | `list` | Cluster viewer only | Deny namespace Role holders even if a query filter narrows results |
| `nodes` | Cluster | `get`, `list` | Cluster viewer only | No namespace-level node listing |
| `clusterstatus` | Cluster | `get` | Cluster viewer only | Return bounded health, coverage and store state without raw identifiers |
| `metrics` | Cluster | `get` | Metrics scraper or a cluster operator with the separate metrics binding | Return workload-labelled metrics only through this authenticated path |
| `ingestionepochs` | Cluster | `get` | Node agent only | Return current collector epoch and schema version |
| `nodesnapshots` | Cluster | `create` | Node agent only | Bind Pod and node claims, validate epoch and sequence, then validate payload |
| `/healthz` on the Pod listener | Local probe | `get` | Kubelet or cluster network allowed by policy | Process health only, no store or identity data |
| Agent `/metrics` | Cluster-local | `get` | Operator monitoring path | Low-cardinality process counters without workload or node labels |
| Legacy `/api/v1/*` and workload `/metrics` | Direct Service | None in secure profile | No production principal | Listener disabled or request rejected before routing |

Client-side compare, capture and recommendation actions may use only records already returned by authorised API calls. They cannot switch to a wider server resource or infer whether a denied object exists. CLI and TUI streaming views use bounded polling; each refresh performs a new authenticated and authorised `list` request. The API does not advertise a Kubernetes `watch` verb.

## Roles

### Namespace viewer Role

```yaml
- apiGroups: ["memory.kubememlens.io"]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: ["memory.kubememlens.io"]
  resources: ["containers", "workloads"]
  verbs: ["list"]
- apiGroups: ["memory.kubememlens.io"]
  resources: ["pods/history"]
  verbs: ["get"]
```

Bind this Role in each namespace the principal may inspect. A RoleBinding in the KubeMemLens installation namespace is not a tenant grant.

### Cluster viewer ClusterRole

```yaml
- apiGroups: ["memory.kubememlens.io"]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: ["memory.kubememlens.io"]
  resources: ["containers", "workloads"]
  verbs: ["list"]
- apiGroups: ["memory.kubememlens.io"]
  resources: ["pods/history"]
  verbs: ["get"]
- apiGroups: ["memory.kubememlens.io"]
  resources: ["nodes"]
  verbs: ["get", "list"]
- apiGroups: ["memory.kubememlens.io"]
  resources: ["clusterstatus"]
  verbs: ["get"]
```

Grant `metrics` separately so an interactive cluster viewer does not automatically become a monitoring scraper.

### Node agent ClusterRole

```yaml
- apiGroups: ["memory.kubememlens.io"]
  resources: ["ingestionepochs"]
  verbs: ["get"]
- apiGroups: ["memory.kubememlens.io"]
  resources: ["nodesnapshots"]
  verbs: ["create"]
```

The agent cannot get, list, watch, update, patch or delete snapshots. It retains its existing Pod and owner metadata permissions because those feed local mapping, not collector authorisation.

### Extension server permissions

- Bind `system:auth-delegator` to the collector ServiceAccount.
- Bind `extension-apiserver-authentication-reader` in `kube-system` to the collector ServiceAccount.
- Do not grant the collector `get`, `list` or `watch` on Pods, Nodes, Secrets or workload resources.
- Mount only a short-lived projected API token. Keep normal automatic token mounting disabled.

### Certificate bootstrap permissions

Helm creates the serving Secret and APIService objects. The hook job may get and update only those exact resource names. It does not receive `create`, because Kubernetes RBAC cannot constrain that verb by `resourceNames`. Helm removes the hook Pod, ServiceAccount, Role, ClusterRole and bindings after success.

## Request authentication

The extension server loads these values from `kube-system/extension-apiserver-authentication`:

- request-header client CA;
- allowed proxy client names;
- username headers;
- group headers; and
- extra-header prefixes.

It accepts forwarded identity only after the TLS client certificate chains to that CA and its common name is allowed. It removes any identity headers received on an unverified connection. A caller cannot gain identity by setting `X-Remote-User` or an extra header directly.

Use Kubernetes delegated authentication and authorisation libraries. Hand-written header parsing or an allow decision based on NetworkPolicy, source IP, Pod label or HTTP path is prohibited.

## Authorisation decision

For each request, construct one `SubjectAccessReview` with the original user, groups and extras plus the exact Kubernetes request attributes. The group is `memory.kubememlens.io`. Resource, subresource, verb, namespace and name must match the route before reading the store.

Allow only `status.allowed=true`. Deny a missing decision, `status.denied=true`, malformed response, timeout, TLS error, API server error or identity parsing error. Do not retry a denied request. Bounded retries may cover a transient transport failure only before any store mutation.

The response for missing and out-of-scope objects uses the same public status and error shape where practical. Logs use a reason code without the denied object name.

## Agent binding and replay protection

The extension server authenticates the request, checks the agent identity, and applies body-size, rate and concurrency limits before decoding a snapshot body. It then checks all of the following before storing the snapshot:

1. The principal username is the exact release namespace `kube-memlens-agent` ServiceAccount.
2. The authenticated extras contain one Pod UID, one node name and one node UID.
3. The request has `create` permission for `nodesnapshots`.
4. The payload node name and node UID equal the authenticated claims.
5. The payload ingestion epoch equals the current collector epoch.
6. The sequence is greater than the last accepted sequence for that Pod UID.
7. Capture time is within the configured age and future-skew bounds.
8. Existing schema, entity-count, identifier and encoded-byte checks pass.

The collector records accepted sequence state by authenticated Pod UID. A new agent Pod receives a new UID and starts a new sequence. Recently replaced Pod UIDs remain denied for two minutes, covering the documented deletion and termination revocation window, then expire from bounded state. Per-node rate-limit state survives Pod replacement and evicts the least recently seen node at capacity rather than blocking a cluster rollout. A collector restart creates a new random epoch, so stored requests from the previous process fail before sequence evaluation.

A stolen live agent token can act only as that ServiceAccount while the bound Pod exists and the token remains valid. It cannot claim another node because the signed node extra must match the payload. Deleting the Pod or ServiceAccount revokes the bound token through Kubernetes. The remaining revocation delay and token lifetime are recorded in the runbook.

## Credential lifecycle

- Operator credentials stay inside normal Kubernetes client-go handling. KubeMemLens never serialises or logs them.
- Agent and collector tokens use projected volumes. Kubelet refreshes them before expiry, and clients reopen the token file rather than caching its first contents.
- Agent tokens are bound to the agent Pod. Agent replacement changes Pod UID and credential ID.
- RoleBinding or ClusterRoleBinding removal revokes new API access through Kubernetes RBAC.
- Certificate rotation updates the serving Secret and APIService CA bundle through a dual-CA transition; the extension server reloads the mounted certificate without enabling a plaintext fallback.
- Compromise response deletes the affected Pod, removes its binding where applicable, rotates the serving certificate if proxy trust is in doubt and restarts the collector to change its ingestion epoch.

No long-lived token Secret is created.

## Audit and telemetry

One security decision record contains:

- request ID;
- principal type, not a raw token;
- verb, resource and namespace-scoped or cluster-scoped marker;
- allow, deny or error;
- bounded reason code;
- agent node-claim match result for ingestion; and
- request completion status and duration.

Do not log usernames by default. A cluster administrator may correlate request IDs with Kubernetes audit logs. Metrics use bounded labels only and never include username, group, namespace, Pod, token, UID or object name.

## Failure behaviour

| Failure | Result |
| --- | --- |
| Kubernetes API unavailable | Reads and writes return unavailable. No direct fallback opens. |
| SubjectAccessReview timeout or error | Deny without store access or mutation. |
| Aggregation ConfigMap missing or invalid | Readiness fails and identity-bearing requests are rejected. |
| Proxy certificate or allowed name mismatch | Reject before trusting headers. |
| Serving certificate expired or CA bundle mismatched | APIService becomes unavailable. No plaintext fallback. |
| Agent token or Pod deleted | Kubernetes authentication fails after its documented invalidation window. |
| Collector restart | Ingestion epoch changes; agents refresh it and retry with their next sequence. |
| NetworkPolicy removed | Authentication and authorisation still apply to every production route. |
| Collector compromised | In-memory data is exposed to that process. Delegated RBAC cannot read arbitrary Kubernetes objects or mint credentials. |

Offline incident replay remains available because it uses a local capture that was created from previously authorised data. It never connects to the collector.

## Managed-cluster portability

Each provider qualification must verify:

- the aggregation layer and `apiregistration.k8s.io/v1` APIService are available;
- the request-header authentication ConfigMap has the required keys;
- the control plane can reach the extension Service over its TLS port within the five-second discovery budget;
- Pod-bound ServiceAccount authentication forwards Pod and node extras;
- delegated SubjectAccessReview works for namespace and cluster roles;
- private-cluster firewall or control-plane network rules permit the extension Service;
- certificate install, rotation, rollback and uninstall leave no stale APIService or RBAC; and
- an unavailable extension does not create an unbounded namespace-deletion or discovery failure.

Failure of any item keeps that provider profile unqualified. There is no cloud-specific authentication bypass.

## Implementation dependency

The extension server imports `k8s.io/apiserver` and `k8s.io/component-base` at the same `v0.33.12` level as the existing Kubernetes client modules. This is a deliberate exception to the usual no-new-dependency default. The upstream request-header authenticator, certificate controllers, request-info filter and delegated authoriser implement the trust boundary that KubeMemLens must not reproduce by hand.

This adds the Kubernetes API-server transitive graph to the collector build even though KubeMemLens does not use etcd or admission storage. The repository keeps the modules version-aligned, scans reachable vulnerabilities, scans the built image, and tests the real aggregation path. The packages are Apache-2.0 licensed and maintained with Kubernetes. Removing them would require another supported Kubernetes extension-server library that preserves request-header rotation and exact delegated `SubjectAccessReview` behaviour.

Operational response, rotation and rollback steps are in the [authenticated ingestion runbook](../runbooks/authenticated-agent-ingestion.md).

## Feasibility evidence

Run the local check against a disposable or authorised kind cluster:

```sh
AUTH_VERIFY_KUBECONFIG='<path-to-kubeconfig>' \
AUTH_VERIFY_CONTEXT='<kind-context>' \
AUTH_VERIFY_ACKNOWLEDGE='run-and-clean-auth-feasibility' \
make verify-auth-architecture-kind
```

The check refuses pre-existing resource names, creates one fixed namespace and three fixed cluster roles, prints a sanitised JSON result and removes every resource it created.

On 25 August 2026, Kubernetes 1.35.5 kind verification passed:

- aggregation request-header configuration contained the client CA, proxy CA, allowed names, username, group and extra-header keys;
- TokenReview authenticated the Pod-bound agent token and returned credential, Pod and node extras;
- TokenReview rejected a non-matching audience;
- namespace read was allowed only in the bound namespace;
- namespace node read and cross-namespace read were denied;
- cluster read and node read were allowed only for the cluster viewer; and
- the agent could get the ingestion epoch and create, but not list, node snapshots; and
- the metrics scraper could get metrics but could not list Pods.

The custom token audience in this check isolates TokenReview claim and audience behaviour. The production aggregated path uses the Kubernetes API server audience and never forwards the token to the extension server. This check proves only the Kubernetes identity and RBAC inputs. PROD-003 separately verifies the aggregated TLS write path and serving-certificate lifecycle. PROD-004 still owns the read-resource implementation.
