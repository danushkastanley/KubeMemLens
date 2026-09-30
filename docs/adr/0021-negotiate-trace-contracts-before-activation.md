# ADR 0021: Negotiate trace contracts before activation

Status: implemented and locally verified; no qualification or release promotion implied.

## Context

The client checks strict preflight and stream schemas, but discovers only API
resource names and verbs before making requests. Rejecting a new stream format
after the worker has started is too late. An older server can ignore an unknown
HTTP header during a rolling downgrade, so a handshake alone cannot prevent that
activation. Existing strict decoders also make in-place field additions unsafe.

## Decision

Negotiate a bounded inclusive contract range on the existing authenticated routes
with `X-KubeMemLens-Trace-Contract: 1-1`. The server acknowledges the selected
contract with the same response header containing `1`. Repeated, combined,
non-canonical and oversized offers fail. No common contract returns HTTP 406 and
a fixed client incompatibility error. Missing acknowledgements never silently
select a legacy protocol. Kubernetes core Pod reads do not carry this header.

Contract 1 binds public request schema 3, preflight response schema 1 with the
matching request-schema number, v1alpha1 admission objects, file/cache stream 2,
OOM stream 3 and export schema 2. A peer contract describes supported semantics, not executable
semver, programme selection, permission or resource qualification. Installed
programme versions and digests remain administrator-owned and independently
checked by both node and API. Experimental stream 1 is outside contract 1.

Schema 3 requests contain explicit `contractVersion: 1` plus the existing complete
Pod/container/start/node comparison preconditions. Previous servers reject this
body before binding. New stream activation uses the exact `?contract=1` marker
alongside the negotiated header; previous handlers reject all queries before
claiming a lease. Current handlers require the marker for negotiated activation
and reject other queries. Thus an extension downgrade between preflight and
activation cannot turn an ignored header into a kernel load.

Previous development clients may continue using their unchanged schema-2 request
and preflight representations without a header on the current server. The legacy
schema-1 server-resolved request remains outside the negotiated contract; it does
not supply authority or bypass server policy. A negotiated request must use schema
3, and schema 3 without negotiation is rejected. No JSON fields are added to an
older response representation. This supports server-first rollout; a new client
refuses an extension that does not implement negotiation. No qualified released
trace extension exists yet, so tests must distinguish development binaries from
released product compatibility claims.

Cancellation keeps its existing authorised DELETE and Kubernetes Status v1
response. It requires neither a compatibility offer nor acknowledgement: readers
must remain able to release their own resources after a mismatch. Absence or a
failed cancellation still does not confirm physical cleanup.

## Alternatives

An ignored header alone permits activation on an incompatible older handler.
Selecting a stream format from caller input could downgrade disclosure semantics
or choose an unaccepted programme. New discovery resources would expand routes and
permissions without solving the downgrade race. Reinterpreting old JSON fields or
relaxing strict decoders would make safety-critical changes ambiguous.

## Consequences, migration and rollback

Deploy the extension before the CLI. Every new control operation checks its own
acknowledgement; no cached successful discovery authorises a later operation.
Keep exact identity, bounds, quota, retention and audit checks independently of
negotiation. No standard collector/agent API, chart privilege or stored database
migration is introduced. Rollback disables admission, verifies owned cleanup and
rolls back the matched optional extension; cancellation remains available.

Export readers support schemas 1 and 2 before the writer moves to schema 2. The
new schema records an acknowledged-or-unknown contract and the incompatibility
failure category without changing the frozen schema-1 vocabulary. A separate
bounded archive type preserves accepted bytes and untrusted caveat text, rejects
unknown policy fields and cannot be implicitly logged or JSON-exported. Structural
validation does not establish provenance. Existing files are never silently rewritten.

The previous/current binary matrix and live activation guards passed the
[bounded local checks](../ebpf/COMPATIBILITY_LOCAL_VERIFICATION.md). The released
standard CLI has no trace command and is not represented as a released trace peer.
Resource, provider and release gates remain open.
