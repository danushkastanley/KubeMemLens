# ADR 0011: Stream traces through owned ephemeral leases

Status: implemented for the optional prototype; incident programmes remain gated.

## Context

Admission establishes one immutable target and policy, but cannot authorise a
second consumer or release quotas while a node still owns execution resources.
Streaming also needs different deadlines from ordinary diagnostic requests, and
a replacement process cannot attest to the absence of its predecessor's handles.

## Decision

Attach once with an authenticated GET to the namespace-scoped
`traces/ID/stream` subresource. Require current `get traces/stream`, ordinary
trace-read permission, owner identity and Pod read permission. Revalidate authority
and Kubernetes target lifetime every second. The node separately rechecks its
retained cgroup handle. No client-supplied runtime identity, programme reference,
new bounds or changed path consent enters attachment.

The initial private node bind freezes target, kind, path policy and bounds.
Activation uses that stored specification under an immutable deadline no later
than the admitted duration. It retains the cgroup descriptor until the engine
confirms teardown. Cancellation/expiry stops execution; it does not prematurely
close the identity reference. Controller and node quotas remain charged through
uncertain cleanup and consumer termination.

Use TLS 1.3 with pinned peer certificates and the existing Node UID registry.
A private identity handshake obtains a fresh node-process nonce before binding.
Every operation carries that nonce and a controller-process nonce. The node stores
the owning controller nonce. A replacement node refuses old-instance requests,
and another controller cannot adopt an existing binding. This is scoped ownership,
not a replacement for certificate authentication.

Unknown predecessor cleanup retains quota. Recovery requires ownership-specific
verification and a controlled restart; automatic reconciliation is not claimed
by BPF-004. BPF-007 must finish the independent watchdog and recovery fault matrix.
A missing record in a new process must never be treated as proof of old cleanup.

The node owns the frame session and selected engine. The controller validates
metadata against its immutable claim and independent programme allowlist, then
relays bounded NDJSON frames. It holds the terminal summary until validated EOF.
For a control-side failure, a healthy downstream connection receives an incomplete
summary containing only known delivery counts and an unknown observation window.
A failed/partial downstream write aborts the connection without appending a frame.

Reserve 2 KiB for the terminal summary within the total output budget. Each frame,
including newline, is at most 8 KiB. Use synchronous serial callbacks with no
growing userspace event queue. Frame writes have a one-second deadline. Execution
ends at the earlier lease/session deadline; transport has at most two additional
seconds to drain the terminal response. A slow or disconnected client cannot
extend the execution window or request replay.

Node streams use separate connections and two stream slots, preserving two
control/cleanup slots. The node command bounds connections at 16. The aggregated
API bounds connections at 64 and stream handlers at 32. Only the exact stream
route is long-running; initial delegated authorisation remains bounded at five
seconds. The Kubernetes serving stack's header bounds remain 1 MiB/32 seconds,
with a 90-second idle connection timeout; HTTP/2 is disabled. Node headers are
bounded at 4096 bytes with a two-second header timeout.

Raw paths require immutable consent and a permitting server policy. The visible
escaping format survives JSON decoding without restoring terminal controls.
Ordinary logging/JSON persistence of internal frames is prohibited. Kubernetes
operators must configure metadata-only audit rules for trace resources before
streaming, and qualification must inspect the actual audit output.

## Qualification and consequences

The production entrypoints currently supply no approved runtime or programme
allowlist, so tracing remains unavailable. The memory engine exists only in test
code, enabled in a test binary with an explicit owned-cluster qualification marker.
It exercises real aggregation, tenant tokens, SAR, TLS, retained cgroup bindings,
Cilium policies and audit behavior without claiming incident-programme execution.

Local qualification covers bounded frames, long streams, ownership, cancellation,
revocation, target loss, node loss, generation isolation, audit redaction and
network policy. Programme semantics, benchmark acceptance and independent security
review remain separate R6 gates. No managed-provider or R7 readiness claim follows.

A persistent CRD/replay log would retain sensitive observations and weaken the
one-consumer boundary. A direct gadget API would bypass admission. Sharing stream
and cleanup connection limits risks starving cancellation. Releasing ownership
on a transport error would confuse unconfirmed cleanup with successful teardown.

## Rollback

Stop admission, end active streams, verify owned cleanup and remove the optional
API/controller/node resources. Controller and node protocol changes deploy
together after idle cleanup. No collector history, standard chart or stored-data
migration is involved.
