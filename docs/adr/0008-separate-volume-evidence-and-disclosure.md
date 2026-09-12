# ADR 0008: Separate volume evidence and disclosure

Status: model and private wire contract implemented; acquisition and read integration pending.

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

Reserve snapshot schema 4 and incident schema 5 for their respective integration
changes. Do not advertise those versions or relax strict readers before the
corresponding production paths and compatibility projections exist.

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

The pure model proves its join, validation and export rules. It does not prove
production authentication, acquisition isolation, storage capacity or runtime
revocation. Those controls require integration tests before enabling the paths.

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

This change enables no acquisition or read route. Later integration upgrades
the collector first, uses separately switchable enrichment, and retains old
memory representations. Disable enrichment before collector downgrade and
export a compatible incident for older readers. No durable data migration is
required.
