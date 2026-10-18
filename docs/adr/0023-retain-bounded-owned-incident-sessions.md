# ADR 0023: Retain bounded owned incident sessions

Status: accepted for opt-in development use; qualification recorded separately
Date: 30 September 2026

## Context

Point-in-time capture files do not preserve the sequence of evidence collection,
operator decisions, source failures and comparisons during an incident. A shared
unbounded incident database would introduce retention, collaboration and source
access obligations outside the approved product scope.

## Decision

Add an opt-in, namespace-scoped ephemeral session store to the authenticated
collector. Require explicit creation and current delegated permissions on every
operation. Derive the actor from authenticated Kubernetes context and bind the
record to the live namespace UID. Initial collaboration is owner-only.

Keep annotations, typed entries and retained capture bytes within fixed quotas and
a non-renewing lifetime. Commit data, references and required gaps atomically,
reserve a closing entry and never silently truncate retained evidence. Use the
existing capture schemas and history/marker acquisition rules. Scope the initial
retained acquisition path to Pod memory and Pod history/markers. Link trace reports
using the established BPFB001 report contract while keeping their bytes outside
the collector. The client validates a selected report and derives its exact-byte
hash and bounded typed reference. The API accepts only the reference within its
existing 4 KiB body limit, never report text or a fetchable path/URL.

Label trace references as operator-supplied. The server authenticates the actor
and validates the projection but does not verify the file, measurement or original
target. Local verification against an authorised export proves file agreement
only. The TUI writes the exact report it references before submission; recreating
it later with a different capture timestamp would not reproduce the same bytes.

Expose separate sanitised and authorised projections. The sanitised form has no
private identity, note, digest or payload fields. Full export is a separate
permission. Share schema validation across API/client/CLI/TUI and reject unknown
versions or contradictory provenance. Offline files do not restore live authority.

Separate unbound creation, operation and full-export roles. The collector gets
only configured Namespace identity reads and namespaced Pod reads beyond its
existing profile. Keep default installation, capabilities and host mounts unchanged.
Use the existing verified client transport and safe atomic file writer. Do not
retry mutations when their outcome is uncertain.

## Alternatives

A database or persistent volume would imply recovery and multi-instance routing
that the collector does not provide. Treating caller-submitted trace references as
server observations would assert unverified provenance. Transiently uploading a
whole report would unnecessarily expose free-form text to upstream request logging.
Implicit capture on ordinary reads would add collection
and retention without an explicit action. One combined role would make creation
draining and sensitive disclosure harder to separate.

## Consequences, migration and rollback

Live records disappear on restart, crash, disablement or expiry. Limits may reject
new entries; failures remain visible and prior evidence stays intact. Export files
outlive server retention and need user-managed protection/deletion. Audit/proxy
body logging is outside the application logger's boundary and needs deployment
verification. RAM has no additional application-level encryption.

Non-trace timelines keep the schema-1 encoding; trace-bearing exports use schema 2.
Readers accept both, preserving the existing schema-1 fixtures unchanged. The
request/status contract remains version 1 and the new action is explicitly scoped
to its trace-reference subresource. Old development clients cannot interpret new
trace entries or schema-2 exports; use matching current clients for that workflow.
Existing capture/report schemas are unchanged. The parser's token ceiling remains
fixed, so capacity accounting also reserves a readable final closed representation.
Before changing the collector image or disabling the feature, stop participating
clients, remove their creation grants, export/close records under the original
identities, and delete or expire them. Account for broader grants such as
cluster-admin. This is an operational drain, not recovery after a restart.

See [operator guidance](../incident-sessions.md) and the
[threat model](../security/incident-sessions-threat-model.md). The
[local functional report](../qualification-results/incident-sessions-local-2026-09-30.md)
records deployed checks and their limits. It does not establish provider,
performance or independent qualification.
