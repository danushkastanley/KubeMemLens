# ADR 0008: Separate volume evidence and disclosure

Status: model, optional Summary collection and scoped collector read API implemented.
CSI health acquisition is integrated; presentation and incident integration remain separate work.

## Context

Kubelet Summary volume records contain cross-tenant identities and filesystem
measurements. CSI health is a separate Kubernetes source with independent Pod,
PVC-controller and Node-backend reports. Existing strict JSON clients require
representation negotiation. Node-only permissions cannot authorise Pod data.

## Decision

Use one bounded volume-context module for validated identity joins, evidence
semantics and output policy. Reuse the existing CSI health evaluator. Bind
samples to current Pod/Node lifetimes and verified PVC bindings. Keep byte and
inode values outside memory totals and preserve absent versus measured zero.

Separate private ingestion, named authorised responses and redacted exports.
Named responses are deliberate and require current underlying-object checks;
producer credentials, Node access and cached objects do not grant that access.
Default formatting, metrics and redacted captures cannot expose volume names or
driver text. Discard free-form backend messages before retaining joined data.

Activate snapshot schema 4 with private batch ingestion and a separate named
Pod-volume read resource. Project out new fields for schemas 1/2/3. Incident
schema 5 remains reserved until capture/replay integration exists. Snapshot schema
5 separately adds historical health; schema4 readers receive only current health.

The [volume contract](../volume-context.md) defines the permission matrix,
source states, field/byte bounds, retention requirements and rollback order.

## Threats and verification

| Threat | Required control and evidence |
| --- | --- |
| Same-name PVC or Pod replacement returns another tenant's data | Namespace/UID/lifetime joins, claim and ephemeral ownership validation, negative identity tests |
| Node viewer derives tenant identities | Separate volume-query and underlying-object checks before stored-data access; no volume records in Node history or diagnostics |
| Cached health outlives permission or Node identity | Current-principal access checks and identity invalidation; warm-cache revocation tests during integration |
| Hostile driver text leaks identifiers or controls the terminal | Fixed limits, control/format-character rejection, message removal and redaction golden tests |
| Large Summary or ingestion payload exhausts memory | Independent input byte, record, per-Pod, nesting and output bounds; malformed-wire and fuzz tests |
| Old client breaks on optional fields | Reserved versions activated only alongside negotiated projections and strict-decoder compatibility tests |

Verification covers model joins, strict decoders, separate retention capacity,
original-principal checks and the disposable local Summary-to-viewer path.
Provider and network-enforcement qualification remain profile-specific.

## Alternatives and consequences

Adding volume records to the Node memory object would expose them through
Node-only history and capture. Direct TUI joins would duplicate access and
identity decisions across consumers. A separate persistent service would add
operational complexity without meeting a new requirement.

An optional Node-context token still has the kubelet privileges granted by its
ClusterRole. Application checks do not contain a stolen token used outside the
application. Existing explicit profile trust and network qualification remain
necessary. No CSI sockets, Kubernetes status writes or cloud APIs are added.

## Migration and rollback

Acquisition and read routes default to off. Upgrade the collector first, enable
the namespaced volume profile, then enable producer filesystem collection.
The optional viewer role is not automatically bound. Existing memory and Node
viewer permissions remain unchanged. Disable enrichment before collector downgrade and
export a compatible incident for older readers. No durable data migration is
required.

## Health acquisition and retention

Reuse the existing live Pod/PVC/PV binding reads for health and cache only
sanitised immutable status. Use bounded target-specific CSINode GETs for the
Node/driver backend source. This keeps namespace acquisition at `get` only;
list/watch would broaden collection without removing the live identity checks.
Request rate, concurrency, source deadlines and refresh/backoff bounds are part
of the [volume contract](../volume-context.md), and consumers use a separate
volume refresh cadence. No Kubernetes health-status writes are introduced.

Reserve16 MiB for health from the shared64 MiB volume retention ceiling while
health is enabled. Keep current failure/absence separate from an explicitly stale
last-good health observation. Deduplicate condition bytes independently of read
and transition times; preserve old adverse flags without presenting them as a
current report. Do not cache caller permissions or let a short/cancelled caller
request alter shared source availability. Node-scoped backend reports may predate
a newly selected Pod, provided the current Node UID and authorised driver binding
match. They keep their original API observation time.

Snapshot schema5 is intentional: adding `health.lastGood` to schema4 would break
its strict readers. Project the new field out for schema4 while preserving current
failure state, filesystem values and schema4 producer ingestion. This snapshot
version is separate from the reserved incident schema5.

## Volume correlation integration

Snapshot schema 6 carries optional per-container I/O pressure and authorised
binding references. Older schemas omit those fields. Incident schema 5 now stores
one Pod with its volume evidence and optional same-instance memory history.
Default redaction removes cross-capture identity, so comparison cannot use aliases
to infer continuity. Sensitive export is explicit, private and freshly authorised.

Workload queries compose live UID-owned Pods and deduplicate verified shared PVC
bindings without adding filesystem bytes to memory. The workload acquisition and
viewer roles are separate opt-ins. Query count, bytes, deadlines and concurrent
work are bounded, and volume requests cannot occupy the ordinary memory read
gate. Permission decisions are never cached. These boundaries permit scoped
operator diagnosis without granting storage mutation or cluster-wide disclosure.

A single pure explanation module owns correlations. CLI and TUI render its source
labels, separate severities and uncertainty. No diagnostic rule belongs to the
presentation layer. PSI remains container-scoped; correlation and binding identity
do not prove causality, storage latency or continuity through a filesystem reset.
