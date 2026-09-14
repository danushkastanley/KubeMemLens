# Ephemeral stream contract

Status: optional prototype contract. The stream runs
through real local Kubernetes aggregation with a test-only memory engine.
No incident programme is approved. See [ADR 0011](../adr/0011-stream-traces-through-owned-ephemeral-leases.md) for the transport and ownership decisions.

## Framing and limits

The version 1 format is NDJSON: one metadata frame, zero or more event frames,
then one terminal summary. Each encoded frame, including its newline, is at most
8 KiB. The session reserves 2 KiB for the terminal summary before accepting any
event. A requested total output limit that cannot fit metadata plus that reserve
is rejected before the engine starts or any bytes are written.

Every encoded byte counts against the admitted total, including metadata,
newlines and the summary. `writtenBytesBeforeSummary` reports bytes accepted by
the transport before the summary; readers add the actual summary length for the
full total. This avoids a self-referential encoded-size field. Partial writes are
counted as partial bytes, never as a successful event. After a failed/partial event
write the session closes output and does not append a summary to a broken frame.
A disconnected reader must report an incomplete stream; terminal delivery cannot
be guaranteed on a failed connection.

Callbacks are synchronous and serialised. There is no growing event queue. A
frame sink must enforce cancellation and the one-second write deadline; a plain
unbounded writer does not satisfy the interface. Engines must stop on output
failure/context cancellation and release all owned resources before returning,
as required by `trace.Adapter`. Independent enforcement against a stuck runtime
worker is part of the node lifecycle work; this Go interface is not proof that
an arbitrary non-cooperative adapter can be stopped safely.

The admitted map-memory ceiling includes ring-buffer storage and per-CPU map
multiplication. Each approved programme must declare and validate its complete
allocation before loading; reducing a request's budget must reject an oversized
programme, never silently exceed it. The BPF-004 memory engine allocates no BPF
maps or ring buffer. Allocation enforcement for real programmes is an acceptance
requirement of their adapter tickets, not evidence supplied by the memory engine.

## Identity, privacy and interpretation

Metadata names the admitted namespace, Pod UID, container and container start,
engine/programme digests, immutable disclosure policy, bounds and session deadline.
A SHA-256 digest commits to the full internal target, including Node UID and cgroup
ID, without exposing those runtime identifiers. Digest syntax is not programme
approval: the trusted node integration must select an accepted programme.

File paths are absent under the default policy. Confirmed-path events use a
canonical visible representation: backslashes are doubled, and terminal/control,
Unicode formatting and line-separator characters become `\u{hhhh}` escapes.
Invalid UTF-8 is rejected. Original byte limits are checked before encoding and
again when readers validate the reversible escaped representation. A terminal
client displays this representation; decoding it back to original control bytes
is only for explicitly authorised non-terminal processing.

Ordinary JSON encoding and formatted logging of frames, metadata and summaries
are blocked/redacted. Only the explicit ephemeral codec exposes wire bytes.
Collectors, incident capture, history, logs and metrics must not receive them.

Engine `produced` counts target-filtered candidate observations. `sampled`, `lost`
and engine `rejected` are disjoint pre-callback outcomes. Session `rejectedEvents`
counts callbacks refused before successful transport completion. `writtenEvents`
counts callbacks whose complete frame was accepted without a transport error;
it does not prove a human read the event. Unknown engine counts are explicitly
`null`, including on permission loss. Missing count fields are rejected.

A complete summary requires a known observation window, known loss counts, no
reported loss/sampling/rejection, consistent produced/outcome accounting and
normal expiry. Session start/deadline and the engine's reported observation
window are separate timestamps. Failed, cancelled, truncated or inconsistent
results remain incomplete. Permission loss stops callbacks and suppresses final
engine observations and counts.

## Claim and node ownership

The manager claims an admission atomically for one consumer. Claim and subsequent
revalidation require exact `get traces/stream`, ordinary trace-read permission,
owner identity and current Pod permission. Request cancellation closes the claim.
An active status reports its active deadline without changing the frozen target
or execution choices. The session uses the earlier of that deadline and its own
admitted duration bound.

The initial private node bind now includes kind, path policy and bounds. Old
private requests without that intent are rejected; update controller and node
together using the idle-first deployment procedure. Activation can only use the
stored specification and cannot extend its duration. Replay protection and node
quota remain held while active teardown is unconfirmed. Expiry cancels execution
but does not release the cgroup reference before the engine finishes cleanup.

The controller likewise retains all quotas during uncertain cleanup and retries
closure at a bounded cadence. A lost bind response returns cleanup-only authority
to the manager. An explicit replay denial does not confer authority to close the
original binding. No error response is treated as proof that a node owns nothing.

## Validation

The decoder rejects unknown versions/fields, case aliases, duplicate keys,
non-nullable nulls, arrays and ambiguous event/frame unions. The stream reader
also enforces ordering, immutable kind/path policy, timestamps, cumulative event
and byte ceilings, terminal reservation and summary accounting. EOF before a
summary and any frame after a summary invalidate the stream. The caller must
bound underlying transport reads and consume through EOF.

Core verification covers round trips, privacy, strict decoding, visible text
escaping, reader limits, event/output/duration ceilings, partial and slow writes,
permission revalidation, concurrent callbacks, one-use sessions and cleanup before
terminal output. Local aggregation tests cover long streams, ownership, revocation, cancellation,
consent, target/node loss, metadata-only auditing and Cilium enforcement. The test
engine is not evidence of kernel programme semantics.


## Serving and process identity

The public attachment route is GET `traces/ID/stream`; it accepts no body or query
parameters. The controller checks the node's metadata before forwarding it. A
healthy downstream connection can receive an incomplete control-side terminal
summary after target, authority or node failure. Upstream observations/counts are
unknown in that summary; actual delivered-frame counts remain explicit.

Node and controller process nonces scope every private binding operation. A
replacement process cannot confirm predecessor cleanup; the controller retains
uncertain quota. The R6 prototype requires verified administrative cleanup before
coordinated recovery. It does not automatically adopt or resume previous streams.

Before installation, put a metadata-only rule ahead of broader request/response
audit rules for the tracing group, and verify the applied API-server policy:

```yaml
- level: Metadata
  resources:
  - group: tracing.kubememlens.io
    resources: [traces, traces/stream]
```

Qualification checks the real audit log for absent request/response objects and
fixture-event markers. The production binary has no memory engine or approved
programme allowlist; only the separately built test executable can enable the
explicit `KML_STREAM_QUALIFICATION=owned-local-kind-contract-fixture` mode.

See [the local qualification procedure](../../prototype/trace/qualification/STREAM.md)
for building that test executable and reproducing the aggregation checks.
