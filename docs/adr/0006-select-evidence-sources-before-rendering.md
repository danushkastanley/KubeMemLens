# ADR 0006: Select evidence sources before rendering

Date: 11 September 2026
Status: accepted for source discovery and current observation queries

## Context

Deep readers return cgroup snapshots. Kubernetes Metrics API observations report
working set and have different permissions, missing fields and sample times.
Putting working set into a cgroup total would change existing diagnoses and
could present missing composition or events as a healthy zero.

## Decision

Separate `--mode=auto|deep|restricted` from the existing collector transport
flag, `--connect-mode`. `internal/capability` owns bounded discovery and a pure
source resolver. Status and the TUI use the same selection and labels. A TUI
session retains its selected reader through refresh failures. Initial discovery
remains asynchronous and can be retried without leaving the TUI.

Auto mode prefers an accessible deep source, including partial or stale evidence.
It can inspect restricted capabilities when the aggregated source is absent or
forbidden. Each source uses the caller's identity independently. Authentication,
transport and malformed-response failures do not trigger a source change.
An explicit deep selection and explicit collector URL/proxy never fall back.

Discovery carries availability, reason, API version, freshness, completeness and
capability stability separately. An advertised or authorised API is not proof of
fresh measurements. The evidence envelope carries source and receive times,
scope, window and caveats when observations are obtained. Health remains the
separate source-specific contract in `internal/volumehealth`.

Restricted discovery evaluates exact self access reviews for Pod status and
Metrics API list access in the requested namespace or explicit cluster scope.
It never lists namespaces, Pods, Nodes, PVCs or CSINodes. Metrics discovery uses
the existing v1/v1beta1 negotiation and performs no metrics read. Missing CSI
health fields are not interpreted as disabled or healthy.

Query availability describes operations provided by the selected evidence path,
not an authorisation grant for every endpoint. Each operation must still perform
its own current server-side access check. Source discovery cannot grant history,
node or metrics access merely because current Pod data is readable.

The common `observation.Reader.Current` query returns source-aware application
types. Restricted queries use bounded Kubernetes reads and explicit optional
working sets; deep queries project the original snapshot contracts. The cgroup
renderers stay guarded until restricted presentation is delivered. Restricted
capture, replay and compare require their own schema work.
The collector wire schemas and incident formats are unchanged by discovery.

## Alternatives

Giving each renderer its own discovery logic would duplicate policy and permit
different memory meanings within one session. Reusing the cgroup memory model
for working set would preserve the Go interface while breaking its semantics.
Neither approach is accepted.

## Security and operational consequences

Self access reviews add read-only policy evaluation through the caller's existing
Kubernetes connection. They do not create RBAC or require installed workloads.
The fixed discovery sequence shares one deadline. Review responses are bounded
to 1 MiB and redirects are rejected. Raw review reasons and evaluation errors
never enter the plan or terminal. Metrics discovery retains its existing bounds.

The [agentless reader](../agentless-reader.md) applies response, combined byte,
object, request and output bounds to every refresh. Required Pod reads precede
optional enrichment. Owner resolution is scoped and UID-bound, with no cache
across refreshes. Credentials remain in the caller transport.

A failed optional API cannot broaden scope or enable a privileged fallback.
Transport failures preserve the last good TUI frame with its age. Confirmed
revocation retains the existing behaviour of clearing data and pending history.

Deep discovery reads scoped summaries before selecting the reader, adding one
bounded startup read. Healthy deep mode does not inspect optional Kubernetes
sources. Large deployments can use the existing summary endpoint rather than
loading all container detail for discovery.

## Migration and rollback

No chart, credential, agent ingestion, collector storage or incident migration is
required. Restore the previous binary to remove discovery, or select `--mode=deep`
to avoid restricted negotiation. Namespace status can use `status -n <namespace>`;
status without a namespace retains its existing cluster-scope meaning.
