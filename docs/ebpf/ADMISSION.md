# Optional trace admission

The R6 prototype can authorise a request and retain the exact running container's
cgroup identity for at most 15 seconds. It does not execute an incident programme
or return trace events. Custom programme approval, lifecycle/stream work and
independent security qualification remain separate gates.

[ADR 0010](../adr/0010-admit-traces-through-a-separate-aggregated-api.md) records
the trust boundaries. The standard agent, chart and release do not install this
API or grant BPF permissions.

## Installation boundary

Use an administrator-owned disposable Linux kind cluster for qualification. The
current renderer installs one controller replica and one binding-node replica
for one explicitly selected Node UID. It uses `Recreate` updates: overlapping
controllers or binding-node replicas would invalidate the quota assumptions.
This is an in-memory prototype, with no HA or distributed quota claim.

The controller needs current `get pods`, `get nodes`, SubjectAccessReview creation
and the standard aggregation authentication ConfigMap reader role. It runs as
UID 65532, with all capabilities dropped, no host mounts, a read-only root and
RuntimeDefault seccomp. Its ServiceAccount token is for those API calls only.

The binding node has no ServiceAccount token, runtime socket, host PID/network/IPC
namespace, writable host mount or SYS_ADMIN capability. It uses BPF and PERFMON
only to execute the existing non-attaching baseline at startup. Unsupported or
degraded preflight prevents the listener from opening. Cgroup binding itself
has been exercised with zero capabilities. The node retains directory handles;
it does not attach programmes or pin BPF objects.

Before deployment:

1. Build `prototype/trace/Dockerfile`, load the image into the owned kind nodes
   and record its immutable digest. The renderer uses `imagePullPolicy: Never`.
2. Install [binding-node.json](../../prototype/trace/seccomp/binding-node.json)
   beneath the node's kubelet seccomp root as
   `kube-memlens-trace/binding-node.json`. This extends the fixed preflight policy
   with exact directory-handle operations and TCP listener syscalls. It permits
   only IPv4/IPv6 stream sockets; BPF attach, pin and global-ID commands remain
   denied. The service never modifies the profile.
3. Make the real BTF, tracing, securityfs, bpffs and cgroup-v2 directories available
   at their usual paths. They are mounted read-only into the binding node. Local
   qualification mounts securityfs and bpffs in the disposable kind node's mount
   namespace before starting the Pod. The worker cannot mount them.
4. Read the selected Node UID and its configured kubelet cgroup root. kind uses
   `/kubelet`. A replacement Node requires a new trusted endpoint registration;
   it must not inherit the former Node's authority merely by reusing its name.
5. Prepare short-lived CA-signed TLS identities in a protected local directory:
   `ca.crt`, `api.crt`, `api.key`, `node.crt`, `node.key`, `control.crt`, `control.key`.
   Use serverAuth SANs `admission-api.NAMESPACE.svc` and
   `binding-node.NAMESPACE.svc`; the control identity needs clientAuth. Include
   normal CA constraints, key usages and subject/authority key identifiers.
   Private keys and rendered Secret manifests must not enter source control.
   Kubernetes aggregation proxy trust is loaded separately from its ConfigMap.

The private node connection requires TLS 1.3, normal CA/hostname verification and
an exact administrator-pinned leaf certificate SHA-256 digest in both directions.
The registry is keyed by Node UID. Certificate or registry changes require a
controlled restart of both services; there is no unattended trust expansion.
The APIService validates the API serving CA and never skips TLS verification.

```sh
kubectl --context kind-kml-r6-admission apply \
  -f prototype/trace/kubernetes/admission-namespace.yaml
umask 077
python3 prototype/trace/kubernetes/render_admission.py \
  --image YOUR_LOADED_IMAGE@sha256:YOUR_VERIFIED_DIGEST \
  --node YOUR_NODE --node-uid YOUR_NODE_UID \
  --namespace kube-memlens-trace-admission --kubelet-cgroup-root /kubelet \
  --certificate-directory /YOUR/PROTECTED/CERTIFICATES > /YOUR/PROTECTED/admission.json
kubectl --context kind-kml-r6-admission apply -f /YOUR/PROTECTED/admission.json
```

The namespace has an explicit Pod Security exception for the node's capabilities
and host metadata mounts. Keep deployment, Secret, Pod/log and ConfigMap access
within the administrator trust boundary. The renderer grants no tenant access. Its NetworkPolicies permit the aggregation
TLS port and restrict the node listener to controller Pods in the same namespace;
the node has no initiated egress. As with the standard collector policy, proxy
certificate verification protects against variable control-plane source ranges.
NetworkPolicy enforcement requires a supporting CNI. The local default-kind run
verified authentication and authorisation, not CNI policy enforcement.
Do not scale it or change the security profile to obtain a passing preflight.

## Namespace user contract

Grant an intended namespace user `create`, `get` and `delete` on
`traces.tracing.kubememlens.io` in that namespace, plus `get` on the selected core
Pod. Exact `resourceNames` restrictions are honoured on Pod reads and individual
trace reads/deletes. There is no trace list, update, watch, CRD or generic gadget
endpoint. Every operation requires current policy and owner checks; knowing an
admission ID grants no access.

A create is a JSON POST to
`/apis/tracing.kubememlens.io/v1alpha1/namespaces/NAMESPACE/traces`:

```json
{"schemaVersion":1,"pod":"target","container":"worker","kind":"files"}
```

The request allows only fixed trace kinds (`files`, `cache`, `oom`), explicit
raw-path consent and lower limits. Raw paths are denied by the default server
policy. Namespace is taken from the authenticated route. Node, Pod UID, container
ID, cgroup ID, engine references and selectors are rejected as user fields.
Duplicate, null, unknown and case-aliased fields are rejected within a 4096-byte
body limit. The response contains an opaque admission name, namespace, expiry,
engine digest and `admitted` state; it exposes no runtime identifiers.

GET the individual admission to revalidate, or DELETE it to cancel. The optional
single-consumer `GET traces/ID/stream` route additionally requires exact
`get traces/stream` permission and reports the `active` state and deadline after
claim. Production has no approved incident runtime; see the
[stream contract](STREAM.md) for the test-only qualification path. API errors
use bounded Kubernetes Status responses. A lost create response must not be
replayed: the node rejects reused request IDs and independently expires handles.
Controller restart loses admissions. In-flight reservations count against quota:
one per principal, two per namespace, one per node and 32 globally. The node has
an independent hard ceiling of two handles and 256 unexpired replay records;
full replay storage refuses work. Node expiry runs every 100 ms, independently of
controller connectivity. Cleanup failures are reported and block new node work.

The controller bounds inflight requests and applies an additional 20-request/s
admission limiter. Node connections, concurrent handlers, message sizes, timeouts
and lease duration are independently bounded. Audit records contain fixed
operation, decision, reason and principal-category values, with no usernames,
Pod/container identities, cgroup IDs, request bodies or paths.

## Verification and rollback

`make check` covers strict request decoding, policy and quota races, current
Kubernetes resolution, owner isolation, TLS peer forgery, replay, independent
expiry, transport response bounds and shutdown. Ordinary tests do not load BPF.

For the documented `trace-target-a` and `trace-target-b` fixtures, create a
`tenant` ServiceAccount in each and `colleague` in A. Bind namespace trace access
and exact `get pods/target` permission as above. Both fixtures use a running
container named `worker` on the registered node. Then run:

```sh
python3 prototype/trace/qualification/admission_api.py \
  --kubeconfig /YOUR/PROTECTED/LOCAL_KUBECONFIG --output /YOUR/PROTECTED/result.json
```

The runner checks the owned local context, uses real short-lived tenant tokens,
and tests admission, cross-tenant/owner denials, quotas, revalidation and
cancellation through Kubernetes aggregation. It records assertions and statuses.
Additional local qualification covers permission revocation, Pod recreation,
container restart, direct API/node connections, and retained cgroup removal.
The opt-in `targetfs` Linux tests exercise a capability-free read-only binding,
symlink rejection, ambiguous layouts and same-path cgroup replacement. The
mutation fixture creates only its own empty cgroups and child process, removes
its directories, and must run only in an owned disposable kind node.

To roll back, delete the rendered resources, including the APIService and RBAC,
then remove the administrator namespace, local test certificates and owned kind
cluster. Verify the APIService is absent and test node containers are gone.
No standard chart change, stored trace data or data migration is involved.
Local results do not establish managed-provider support or R7 readiness.
