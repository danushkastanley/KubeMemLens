# ADR 0018: Join bounded workload change context to memory history

Status: implemented; qualification recorded separately
Date: 29 September 2026

## Context

Operators need to correlate memory changes with rollout, restart and resize
reports. Existing history deliberately binds current Pod/container instances and
rejects unknown schema-1 fields. Kubernetes event retention and clocks are
incomplete, and event text can expose tenant or application data.

## Decision

Expose opt-in named `pods/trends-context` and `workloads/trends-context` resources.
They contain unchanged memory history and a bounded typed marker report. Preserve
plain `/trends`, its schema and its permissions. Require both the additional named
context permission and existing history/object permissions. Recheck identities,
controller chains and caller authority before disclosure.

Use existing bounded Kubernetes transport and owner-resolution seams. Acquire only
configured namespaces, with explicit optional event/owner RBAC. Keep source reads,
marker count, ancestry depth, bytes, concurrency and time bounded. Store no event
messages, arbitrary reasons/actions, application logs or configuration values.
Do not add a background informer or persistent event ledger.

Preserve timestamps and UIDs. Keep current revision observation distinct from an
exact rollout timestamp, especially when an old ReplicaSet is reused by rollback.
Retained events remain partial evidence. Missing/denied/unavailable sources and
truncation are explicit. A marker does not change the underlying memory series or
establish causation.

Use incident schema 6 for capture/replay/comparison. Redact identities into local
aliases by default and retain fixed provenance caveats. Never infer continuity
between alias sets. Literal-identity comparisons can report a same-name Pod change
as an uncertain interval while keeping its separate memory lifetimes intact.

## Alternatives

Appending fields to history schema 1 would break strict existing readers.
Automatically querying events for plain history would expand access and latency
without an explicit capability choice. Parsing free-form event messages or arbitrary
revision annotations would collect sensitive text and imply unsupported semantics.
A persistent watch/ledger would add retention and authorisation obligations that
are unnecessary for this bounded read workflow.

## Consequences, migration and rollback

Users opt into a new endpoint/CLI mode and separately bind the marker viewer.
Event list permission is namespace-scoped, although disclosure remains restricted
to verified selected objects. Very busy namespaces can truncate the one-page source;
that limitation is visible, and no complete-history claim is made.

No existing response or saved incident is migrated. Disable markers to remove the
optional endpoint, source permissions and collector flag. Existing history remains
usable. Retain a schema-6-capable reader for captures; older formats are not silently
produced by dropping provenance. Provider support still requires runtime evidence.
