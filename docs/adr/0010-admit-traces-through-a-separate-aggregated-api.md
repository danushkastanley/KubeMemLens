# ADR 0010: Admit traces through a separate aggregated API

Status: accepted for local R6 admission; independent security qualification pending.

## Context

An authorised trace must remain bound to one running container lifetime. Existing
memory mapping intentionally permits diagnostic container-ID prefixes; that is
not authority to trace. A user-submitted node, cgroup ID or mutable Kubernetes
object would let requests bypass admission or change target after approval.

The accepted R6 plan permits non-attaching admission work before custom incident
programmes are approved. The standard read API and agent must remain unchanged
outside behaviour-preserving reuse of their authentication implementation.

## Decision

Provide a separately installed aggregated API at
`tracing.kubememlens.io/v1alpha1`. Reuse the validated dynamic Kubernetes
aggregation-proxy authentication in `internal/kubeauth`. Accept identities only
from a trusted proxy certificate and its configured identity headers. Direct
bearer tokens and forged headers cannot authenticate to the extension directly.
Delegate authorisation without an allow or deny cache.

The namespace-scoped `traces` resource supports create, individual get and delete.
There is no cross-namespace listing, mutable trace CRD, generic gadget endpoint or
raw-event persistence. One controller replica owns bounded in-memory admissions;
restart loses them, and clients must not automatically replay a lost create.

Creation requires explicit `create` permission on `traces` in the routed
namespace and current core `get pods` permission on the exact requested Pod.
Check those permissions before looking up the object. Denials must not reveal
whether another tenant's Pod exists. Individual reads and cancellation require
both their API permission and the original requester identity, together with
current target permission. An opaque request ID is not an access credential.

The versioned request is one JSON object of at most 4096 bytes. It may contain
only Pod name, container name, fixed trace kind, explicit raw-path consent and
lower requested bounds. Reject unknown or duplicate fields, null values,
noncanonical field aliases, selectors and runtime identities. Raw paths default
to omitted and require a file trace. Admission enforces the configured server
policy in addition to the engine's absolute ceilings.

Reserve global, principal and namespace quota before target-resolution work;
reserve node quota before node dispatch. Reservations, including pending work,
count against the limits. Expiry and every failure path release them. Defaults
are one per principal, two per namespace, one per node and 32 globally; hard
ceilings are two, four, two and 64 respectively. Pending admissions expire within
15 seconds. Bound concurrent requests and audit output independently.

Resolve the live Pod UID, selected running container's full containerd ID and
StartedAt, scheduled Node name and current Node UID. Reject deleting/terminated
Pods, ambiguous container statuses, incomplete identities and unsupported
runtimes. Resolve the cgroup only on that authenticated node, from exact known
containerd layouts below a read-only cgroup-v2 mount and an explicitly configured
kubelet cgroup root. The latter is trusted installation configuration, bounded
to four validated name components; it is never supplied by the trace caller.
The local kind profile uses `/kubelet`, which participates in every systemd slice
name. Do not reuse prefix matching
or search an unbounded host tree.

The node retains a directory descriptor and its eight-byte kernfs identity for
the binding lifetime. Verify filesystem type, handle type, live population and
exact path identity, with no symlink traversal. Recheck the Kubernetes identity
after binding and immediately before use. A replacement or loss of authoritative
observation invalidates the request; never follow a replacement container.

Use mutually authenticated TLS between the control service and node component.
Trusted installation configuration binds each endpoint and certificate to its
Node UID. The node accepts only the control-service certificate, its own Node UID
and a fresh bounded request identifier. Retain immutable copies and reject
replay. Node-side expiry works independently of controller connectivity. Separate
NetworkPolicies restrict the node listener to controller Pods in its namespace
and deny initiated node egress. The API permits its aggregation TLS port, matching
the standard collector policy for variable control-plane source ranges; proxy
certificate verification remains mandatory. CNI enforcement qualification is
separate from the default-kind authentication tests. Do not
use an elevated helper, runtime socket, host PID namespace or open-by-handle
privilege fallback to obtain cgroup identity.

Audit only bounded decision, reason and principal-category metadata. Do not log
principal names, Pod/container identities, paths, raw request bodies or internal
specifications. Admission responses are not trace events and never imply that
an incident programme has run. The custom-programme execution gate remains.

## Alternatives and consequences

A client-side permission check is bypassable. A mutable CRD does not establish
creator identity or prevent retargeting. Reusing the standard collector listener
would broaden its attack surface. A generic upstream CLI or gadget API cannot
enforce the fixed contract. A shared in-memory controller is deliberately not a
claim of high availability or distributed quota enforcement.

Separate adapters keep Kubernetes policy, the admission state machine, private
node transport and local cgroup binding independently testable through the same
interfaces used in deployment. The controller must not import a BPF loader;
incident execution remains in the optional worker.

## Verification and rollback

Require adversarial two-namespace API tests, denied direct connections, principal
and node forgery, Pod recreation, container restart, cgroup replacement, quota
races, permission revocation, strict-decoder fuzzing and audit redaction. Exercise
the real Kubernetes aggregation path and constrained local Linux binding, not
only an in-memory handler. No incident programme is loaded in this ticket.

Remove only the optional APIService, controller/node resources and their RBAC to
roll back. No stored trace data or standard incident schema requires migration.
Independent security review and R7 acceptance remain separate gates.
