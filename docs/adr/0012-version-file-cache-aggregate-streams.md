# ADR 0012: Version file/cache aggregate streams

Status: candidate implementation; incident programme acceptance and local kernel
qualification remain pending.

## Context

BPF-005 requires path-free aggregate output by default. The version 1 stream
counts individually delivered file/cache frames. Calling those frames aggregates,
or silently reinterpreting their count in an existing summary, would mislead
readers and break produced/outcome accounting.

## Decision

The installed file/cache worker selects NDJSON version 2. Default sessions produce
metadata and fixed numeric aggregates in a terminal summary. Only explicitly
confirmed file paths permit per-operation event frames. File totals distinguish
read/write operation counts and requested/completed bytes; cache totals distinguish
add/remove operation counts and pages. Unknown input and overflow remain explicit.
All current file/cache results remain incomplete because hook blind spots persist.

The aggregate observation ceiling applies without requiring public event frames.
`writtenEvents` continues to count fully delivered frames. Confirmed-path events
enter the aggregate only after complete delivery. Aggregates never retain paths,
identities, timestamps or an event queue, and are suppressed on authorisation loss.
The existing ephemeral transport, permissions, redaction and ownership rules apply.

Both controller and node select the version independently of trace requests.
Private activation compares the version and engine/programme digests before
execution. The controller then checks the returned metadata. Every frame in the
stream, including a controller-generated failure summary, uses that version.
Readers reject mixed versions. A 4 KiB terminal reserve replaces the 2 KiB reserve
for version 2; both retain the 8 KiB frame ceiling and admitted total byte budget.

## Alternatives

Adding optional aggregate fields to version 1 would make an older reader reject
the stream without identifying a contract change. Replacing delivered-event counts
with observation counts would change their established meaning. A new unversioned
aggregate endpoint would duplicate admission and lifecycle boundaries. Explicit
framing versions preserve those boundaries and make incompatibility detectable.

## Consequences and migration

Version 1 constructors and decoding remain available, including its OOM contract.
The candidate version 2 supports files/cache only. Deploy the private node and
controller together while idle; requests missing the explicit version fail closed.
Clients consuming installed file/cache sessions must understand version 2.
There is no automatic negotiation or downgrade based on node output.

Normal expiry allows the worker's existing bounded exit grace to deliver final
counters, but stops event callbacks at the deadline. Earlier parent cancellation
and malformed output retain their failure paths and forced cleanup bounds.

This decision does not approve the programme, SDK patch or deployment profile.
The candidate samples cgroup evidence before activation and after detachment,
retains sampling/overlap intervals and clock uncertainty, and carries the six
selected fields through the private result and v2 terminal. Missing values,
counter resets and unavailable states remain explicit. Signal cancellation
interrupts final sampling even after normal expiry; revocation suppresses output.
Kernel correlation, semantics, allocation and teardown qualification remain
separate required work. Current tests exercise codecs,
sessions, real local TLS/HTTP transport and supervised fixture processes.

## Rollback

Disable new admissions, finish or confirm cleanup of owned sessions, and remove
the candidate worker installation policy before reverting the node/controller
pair. Preserve uncertain cleanup quotas until administrative confirmation. Do not
relabel a version 2 stream as version 1 or fall back to an unaccepted programme.
