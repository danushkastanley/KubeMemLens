# ADR 0004: Use Kubernetes aggregation for authentication

Date: 25 August 2026
Status: accepted; PROD-003 ingestion implemented, PROD-004 and PROD-005 pending

## Context

The alpha collector has separate read and ingestion listeners, bounded JSON parsing and a NetworkPolicy. It does not authenticate either listener. Kubernetes service-proxy RBAC controls who can reach the read Service, but the proxy target is one cluster-wide HTTP API. Once connected, a caller can request every Pod, container, workload, node and history record held by the collector.

The v1 contract includes shared multi-tenant clusters. Namespace filters in the CLI, service-proxy permission and NetworkPolicy cannot enforce that contract. The server must receive a Kubernetes-authenticated principal, apply namespace or cluster policy to every operation and bind every agent write to its scheduled node.

The design must keep the normal no-port-forward workflow, avoid a hosted identity service, work with kubeconfig authentication on GKE, EKS, AKS and self-managed clusters, and preserve a capability-free standard agent.

## Decision

Expose the production read and ingestion API as an aggregated Kubernetes API under `memory.kubememlens.io/v1alpha1`.

The Kubernetes API server will:

1. authenticate the operator, automation account or bound agent ServiceAccount token;
2. apply Kubernetes RBAC to the requested KubeMemLens resource and namespace;
3. connect to the KubeMemLens extension server over TLS with its aggregation proxy client certificate; and
4. forward the authenticated username, groups and extra identity fields.

The extension server will validate the proxy client certificate and allowed name using the `extension-apiserver-authentication` ConfigMap. It will then submit an exact `SubjectAccessReview` for the original principal and requested verb, group, resource, subresource, namespace and name. Missing identity, malformed identity, direct requests, authorisation errors and no-opinion responses are denied.

Use the upstream `k8s.io/apiserver` delegated authentication and authorisation packages rather than parsing request headers or proxy certificates in KubeMemLens code.

### Operator identities

Human and automation clients keep using their existing kubeconfig credentials against the Kubernetes API server. No user bearer token, client certificate or exec-plugin output is sent to or stored by the collector.

Namespace Roles grant only the `get` and `list` operations implemented by namespaced KubeMemLens resources. A separate ClusterRole grants all-namespace and node views to approved cluster operators. There is no wildcard resource or verb grant. CLI and TUI streaming views use bounded polling, and every refresh receives a new delegated decision.

### Agent identity

Each agent uses its projected, Pod-bound ServiceAccount token to call the aggregated API. Kubelet rotates the token. The agent must reload it through the normal client-go transport.

The extension server accepts `nodesnapshots` writes only from the exact release namespace and `kube-memlens-agent` ServiceAccount. It also requires the authenticated extra fields for Pod UID, node name and node UID. The snapshot node name and node UID must equal the authenticated claims. Missing or conflicting claims are denied before the body reaches the store.

Every collector process creates a random ingestion epoch. An agent reads the current epoch, then sends that epoch and a strictly increasing sequence with each snapshot. The collector rejects a wrong epoch, duplicate sequence, lower sequence, stale capture time, future time or node mismatch. A collector restart changes the epoch, so a request captured before the restart cannot be replayed into the new process.

### Metrics and health

Workload-labelled metrics become a cluster-scoped `metrics` resource and are read through the Kubernetes API server by a dedicated metrics ServiceAccount or an operator with the separate metrics binding. The current direct workload `/metrics` route is disabled in the secure profile. Low-cardinality agent process metrics and kubelet probes may remain cluster-local because they contain no tenant or node identity.

Liveness does not depend on Kubernetes. Readiness fails when the extension server cannot validate aggregation configuration, load its serving certificate or delegate authorisation. An API server or authoriser outage returns an unavailable or forbidden result. It never enables a direct unauthenticated fallback.

### TLS and certificate lifecycle

The APIService uses a dedicated serving CA bundle and a release-namespace TLS Secret. Helm creates both objects. A short-lived hook job generates or rotates the material and updates only those exact resource names. It receives `get` and `update`, but not `create`, because Kubernetes RBAC cannot constrain `create` by `resourceNames`. Helm deletes the job and its temporary RBAC after success. The collector never receives Secret write permission.

Normal upgrades reuse a valid certificate. Rotation happens before expiry or through an explicit operator action. Install, upgrade, rollback and uninstall tests must prove that the APIService, Secret and hook RBAC do not leave stale resources.

### Audit and privacy

The Kubernetes API audit trail remains the source for caller authentication and RBAC decisions. KubeMemLens records a bounded decision event with request ID, principal type, verb, resource, namespace scope, allow or deny, reason code and agent node claim when applicable.

Logs and metrics never contain bearer tokens, certificate data, raw user group lists, Pod UIDs, container IDs, cgroup paths or denied object names. Authentication metrics use bounded labels such as principal type, resource, verb, decision and reason.

## Resource policy

The full endpoint and principal matrix is in [the authentication and authorisation architecture](../security/authentication-and-authorisation.md). The main rules are:

- namespace viewers can read only namespaced Pod, container, workload and history resources;
- cluster viewers can read all namespaces, node summaries and cluster status, while workload metrics require a separate binding;
- agents can get the ingestion epoch and create node snapshots, but cannot read snapshots or operator resources;
- the extension server can read aggregation authentication configuration and create SubjectAccessReviews; and
- the certificate bootstrap job has only short-lived, name-scoped update permissions for Helm-created objects.

## Alternatives considered

### Keep service-proxy RBAC as the boundary

Rejected. It authorises access to the Service proxy, not individual collector records. It cannot enforce namespace scope after the request reaches the collector.

### Forward a bearer token in a custom proxy header

Rejected. It creates another credential-bearing header, risks proxy or debug logging, complicates token audiences and makes the collector responsible for every kubeconfig credential type.

### Pass the caller's Kubernetes token directly to the collector

Rejected. The token is intended for the Kubernetes API server, direct transport needs a second TLS and audience design, and compromise of the collector would expose reusable user credentials.

### Mint per-namespace viewer ServiceAccount tokens

Rejected. Shared ServiceAccount identities lose the original human or automation principal and turn token-creation permission into an impersonation mechanism.

### Add an authentication sidecar

Rejected for the primary path. A sidecar still needs a trusted caller credential, splits policy and audit ownership, and does not solve agent node binding by itself.

### Add a CRD or Lease-based access grant

Rejected. It creates persisted authorisation objects and a controller protocol for a problem the aggregation layer and RBAC already solve.

### Issue KubeMemLens client certificates

Rejected. A separate user and agent PKI adds issuance, rotation and revocation work without improving the Kubernetes-native operator experience.

## Consequences

- The normal client path changes from Service proxy to an aggregated API, while the CLI still uses the user's kubeconfig and needs no port-forward.
- The collector becomes an extension API server and gains tightly scoped delegated-authorisation permission.
- The standard agent gains no Linux capability or host mount. Its API credential becomes explicitly projected and rotated.
- Direct collector reads and writes remain available only in an explicit development mode that cannot be enabled by the production chart.
- APIService availability and control-plane-to-Service reachability enter provider qualification.
- A collector compromise can expose in-memory diagnostic data, but it cannot use delegated-authorisation permissions to read arbitrary Kubernetes resources or mint credentials.

## Migration

PROD-003 added the extension-server authentication layer, agent resource, node binding and replay contract. PROD-004 adds tenant-scoped read resources and moves the CLI to Kubernetes discovery. PROD-005 removes the remaining legacy read path and proves end-to-end tenant isolation with NetworkPolicy present and removed.

During migration, the alpha HTTP API remains clearly marked insecure and is never presented as the v1 shared-cluster path. No persisted data migration exists.

## Rollback

Before v1, rollback removes the APIService, serving Secret, new RBAC and projected token configuration, then restores the alpha chart on a trusted evaluation cluster. After v1, a security rollback disables KubeMemLens reads and ingestion rather than enabling the unauthenticated API. Existing incident captures remain local and unchanged.
