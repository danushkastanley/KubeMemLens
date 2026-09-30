# Incident session trust boundary

Status: component and local functional verification; managed-provider qualification pending.

## Assets and boundaries

Annotations, capture bytes, identifiers and digests are private tenant evidence.
The collector holds bounded records in RAM; authorised exports become user-owned
files. Kubernetes authentication, delegated authorisation, namespace/Pod identity,
source providers, client transport and local output are distinct trust boundaries.
An export's schema/digest validation establishes internal consistency, not
independent authenticity or permission to restore it. No endpoint restores a
submitted session export.

The namespace allow-list is configured by the operator. Requests cannot supply an
actor or namespace UID. The server derives an actor binding from the authenticated
Kubernetes name and available UID, copies bounded groups/extras for fresh policy
checks, and binds records to the live namespace UID. Group changes cannot create a
second owner identity. Namespace recreation and a different actor fail closed.
This uses Kubernetes principal semantics; it does not establish human identity.

## Threats and controls

| Threat | Control and evidence surface |
| --- | --- |
| Forged ownership or cross-tenant access | Every operation authorises its exact resource/subresource and enforces stored actor/namespace identity; scope, replacement and revocation tests |
| Collector confused-deputy reads | Caller source permissions precede privileged acquisition; live Pod UID/Node and history/marker identities are revalidated before retention |
| Private-data disclosure | Separate full-export permission; sanitised projection is an allow-list without note/payload fields; bounded public status and masked TUI input |
| False or partial evidence | Source failures and coverage states become typed gaps; evidence bytes, references and required gaps commit atomically |
| Memory, decoder or slow-client exhaustion | Global/namespace quotas, entry/payload/encoded-byte bounds, input structure limits, two in-flight operations, rate limits and transport deadlines |
| Replay, stale completion or wrong cluster | Explicit session IDs, owner checks, no automatic mutation retries; TUI generation guards, cancellation and original-reader transport binding |
| Retention escape | Non-renewing expiry checked on access and swept while idle; deletion/shutdown clear state; TUI clears expired/revoked displays |
| File replacement or terminal injection | Validated exports, 0600 temporary files, no-replace publication by default, explicit overwrite and bounded/quoted terminal text |
| Imported trace report treated as a trusted observation | Client-derived reference explicitly labelled operator-supplied; server authenticates the actor, not report origin or original target; offline file matching grants no execution authority |
| Trace text entering upstream request logs | CLI/TUI send only closed typed reference fields; raw reports, free-form caveats, paths and URLs are outside the request schema and never uploaded by these workflows |

The service admits at most ten requests per second globally (burst 20) and two per
configured namespace (burst eight). Limits bound exposure; an authorised user can
still consume its namespace quota. Full exports require the session owner and
current export permission, not renewed live permissions for every historical
object. They are retained evidence under a distinct disclosure capability.

## Logging and storage

Private values are absent from collector error messages and added logs. This does
not control API-server audit, proxies, debug tracing, node inspection or users'
export destinations. Configure metadata-only audit/body handling for the session
resources where supported and verify the actual deployment. Kubernetes documents
that [audit policy controls body collection](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/#audit-policy).
Do not log request/response bodies for annotations or full exports.

Trace references retain private digests and reported counters, not trace results.
The existing 4 KiB API request limit is unchanged. The submitting client can forge
a syntactically valid reference; neither its hash nor its provenance label is a
signature. The authorised export/local report verification compares exact bytes
and does not establish measurement authenticity. Public projection removes the
digests and counters. A client-supplied reference cannot select a remote source,
cause a collector file read, create a trace or bypass incident ownership checks.

The TUI saves its exact report before submitting the reference and preserves it
on an uncertain server outcome. Trace generation, namespace and revocation checks
precede submission. Local exports remain the operator's retention responsibility.

TLS protects transport. There is no application-level encryption of working RAM;
node isolation and platform protection remain required. No database or persistent
volume is introduced. Exports are explicit, mode-0600 files and do not expire with
the server session. The operator owns their encryption, access and deletion.

## Residual risks and qualification

Source reads are not an atomic Kubernetes transaction. Revalidation catches
observed changes, not every brief intervening change. A compromised authorised
source, collector, cluster administrator or node remains a trust risk. Privilege
changes after the final policy check follow request-time authorisation semantics.
A lost mutation acknowledgement can leave an action or creation unconfirmed;
clients never promise rollback and do not retry automatically.

Creation, ongoing operation and private disclosure have separate unbound roles.
Removing participating users' creation grants permits export/closure before a
rollout without restarting the store. Broader grants must be accounted for during
that drain. Restarting/disabling the collector loses remaining in-memory records.

Component tests cover ownership, replacement, revocation, strict codecs, bounds,
atomicity, privacy, client/CLI/TUI and file behaviour. Helm contracts assert exact
new permissions and unchanged default privileges. The
[local functional report](../qualification-results/incident-sessions-local-2026-09-30.md)
records actual delegated Kubernetes identity/RBAC, adversarial walkthroughs,
retention and upgrade checks. Managed-provider evidence remains separate.
No independent review is claimed.
