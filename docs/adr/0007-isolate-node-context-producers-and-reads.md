# ADR 0007: Isolate Node-context producers and reads

Date: 11 September 2026
Status: contract defined; collection and ingestion integration pending

## Context

Full Node memory contains cross-tenant evidence. The standard agent has no
kubelet stats permission. The collector permits one active agent per Node and
replaces its container snapshot on every post. A separate producer using that
path would retire the standard agent or erase its observations. Strict clients
also reject new JSON fields without representation negotiation.

## Decision

Use a separate default-off DaemonSet, binary and ServiceAccount. It reads direct
HTTPS `/stats/summary` with `get nodes/stats` and a bounded GET of its scheduled
Node object. It has no metrics/proxy/workload permission, host mount or Linux
capability. The [contract](../node-context.md) defines identity, TLS, tokens,
privacy, retention and bounds.

Keep Kubernetes aggregation as the production ingestion/read entry point.
Derive each producer's fixed role from its configured ServiceAccount, never
request JSON. Bind ownership to Node UID and role; bind sequence/digest state
to Pod instance and role. Node-context writes update a separate record, never
the container replacement operation. Replacement or failure cannot alter the
other stream's data, ownership or freshness.

Introduce negotiated snapshot schema 3 during ingestion integration and preserve
schemas 1/2 explicitly. Node-context producers stop if negotiation selects an
older schema. Standard producers retain their compatible cgroup representation.
Failure reports cannot refresh source timestamps; a new Node UID cannot inherit
old deltas or history continuity.

Add opt-in `nodecontexts` and `nodecontexts/history` permissions without expanding
existing viewer roles. Node-only access gives Node totals and fixed system
categories. Pod contributors, counts and accounting gaps additionally require
cluster-wide Pod list authorisation before contributing lookups. Namespace
views keep their own observed charge without full Node accounting.

A pure analysis module consumes authorised inputs. It does not mix working set
with cgroup charge or double-count Summary's `pods` system category. Reserve
incident schema 4 for Node-context evidence; restricted schema 3 keeps its meaning.

## Alternatives and consequences

Expanding standard credentials, sharing one producer owner, reading kubelets from
the TUI and using a proxy fallback would each weaken an existing boundary. A
separate collector or persistent database would add unnecessary operational cost.
Independent source records can share authenticated transport and bounded storage.

A stolen optional token can access other Nodes' stats within its ClusterRole.
Application target validation cannot contain an arbitrary client. Explicit trust
configuration, audience checks and provider network qualification remain required.
Node aggregates can reveal co-tenant activity even without identities; installing
the dedicated viewer binding explicitly grants that aggregate visibility.

## Migration and rollback

The contract-only chart rejects `nodeContext.enabled=true` and preserves standard
rendered output. Upgrade the collector before enabling the future producer and
disable it before collector downgrade. The observed-charge Node view remains
available. Storage remains ephemeral; new captures need a schema-4-capable reader.
