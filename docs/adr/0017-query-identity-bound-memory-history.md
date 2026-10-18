# ADR 0017: Query identity-bound memory history through an optional read adapter

Status: accepted implementation decision; [local functional qualification passed](../qualification-results/memory-history-local-kind-2026-09-28/README.md). Managed-provider qualification remains separate.

## Context

The collector retains a small cgroup history. Operators also need longer trends
from their own Prometheus-compatible service. Working set and RSS are different
from cgroup charge, and namespace/Pod names can be reused. Existing KubeMemLens
metrics deliberately omit immutable identifiers and cannot safely bind these
remote histories to current instances.

## Decision

Keep existing collection, retention and history routes unchanged. Add an optional
read adapter with a fixed HTTPS endpoint, read-only credential and explicit scopes.
A separate bounded API gate keeps provider waits outside the live-read queue.

Resolve and authorise current Pod/container or workload membership, or a Node,
before querying. Re-resolve the selection before disclosure. Bind remote selectors
and returned series to cluster, Node UID, Pod UID and container ID. Workload views
show only current members and retain the controller UID/generation. Names alone
never establish identity. Unavailable identity produces unavailable evidence.

The initial remote profile reads cAdvisor working-set and RSS metrics with
operator-maintained immutable identity labels. KubeMemLens does not add these
labels to its own exporter or install/manage Prometheus. The adapter accepts no
user PromQL. A single range expression returns values and their Prometheus sample
timestamps; evaluation times alone cannot establish freshness. Duplicate series,
unmatched identities and malformed results fail closed. Missing and NaN samples
remain gaps, distinct from zero. Provider warnings produce partial evidence
without disclosing their raw text.

Each request is limited to 16 targets, 241 points per series, seven days, 1 MiB of
provider response, and one concurrent provider query with a five-second deadline.
The API has its own nine-second deadline including identity revalidation. Default
windows are 15 minutes for local retention and 24 hours for Prometheus; grid steps
are at least one minute and grow to respect the point limit. These limits cannot
be increased by readers. No query retries or automatic source fallback are added.

Local fallback is explicit and carries its actual metric/source. Existing Pod
charge and optional Node Summary retention remain available; a per-container
local history is not invented from a Pod sum. The TUI fetches on demand, cancels
obsolete requests, preserves visible gaps and ages displayed evidence over time.

## Alternatives

Direct client PromQL would distribute authorisation and expose an unrestricted
query surface. Merging remote working set into the existing cgroup composition
schema would invent values and silently change meaning. Adding a database or
changing exporter cardinality would broaden storage and privacy obligations.

## Consequences, migration and rollback

The feature is disabled by default and requires a compatible, identity-enriched
remote source. Existing unlabelled metrics are not accepted as instance evidence.
No existing snapshot or incident schema changes, persisted-data migration, new
host permissions or kernel capabilities are required. Removing the optional
configuration and its RBAC removes the new read path; existing live and local
history workflows continue unchanged. Provider, workload-bound, tenant-isolation
and terminal qualification must pass before this ticket is accepted.

## Sources

- [Prometheus range API](https://prometheus.io/docs/prometheus/latest/querying/api/).
- [Prometheus lookback and staleness](https://prometheus.io/docs/prometheus/latest/querying/basics/#staleness).
- [Sample timestamps](https://prometheus.io/docs/prometheus/latest/querying/functions/#timestamp).
- [cAdvisor metric definitions](https://github.com/google/cadvisor/blob/master/docs/storage/prometheus.md).
