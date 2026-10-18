# Trace compatibility contracts

This is the development contract implemented by BPFB-001. No trace profile
or public-beta release is qualified by this document. The optional extension is
separate from the standard collector and agent; their snapshot negotiation is
unchanged.

## Public negotiation and upgrade order

The current CLI offers contract range `1-1` with
`X-KubeMemLens-Trace-Contract`. The extension returns the selected version `1`.
The minimum and maximum implemented public contracts are both 1. Version ranges
are protocol contracts, not application semver or Kubernetes versions.

| Client | Extension | Behaviour |
| --- | --- | --- |
| Current contract-1 client | Current contract-1 extension | Negotiated request schema 3; exact selection; installation-selected streams |
| Previous development client using schema 2 | Current extension | Unchanged legacy response shape; current authorisation, policy and limits still apply |
| Current client | Extension without negotiation | Explicit incompatibility before admission; no missing-header fallback |
| Future offer retaining contract 1 | Current extension | Select contract 1; its existing request/stream semantics remain mandatory |
| Disjoint or malformed offer | Current extension | 406 or 400 respectively, before target binding or activation |

The future-range rows are contract tests, not claims that an unreleased binary
exists. There is no qualified released trace client/extension compatibility window
yet. The previous development binary and released standard-product binaries must
be identified separately in verification records.

Deploy the optional extension before the CLI. A controller downgrade after a
successful discovery/preflight does not grant permission to start an unreadable
stream: schema-3 request bodies and the exact `?contract=1` activation marker are
rejected by the previous handler before binding/claiming. Current handlers require
the marker alongside negotiation. Other query parameters remain forbidden.

Cancellation uses the unchanged authorised DELETE and Kubernetes Status v1
response without negotiation. Keep uncertain reservations until physical cleanup
is confirmed; version mismatch, a 404, or an expired handle is not cleanup proof.

## Representation inventory

| Representation | Explicit version | Compatibility rule |
| --- | --- | --- |
| Contract offer/acknowledgement | Request `min-max`; response selected integer | Current minimum/maximum 1; canonical bounded single header |
| Public admission/preflight request | JSON `schemaVersion` 3 and `contractVersion` 1 | Requires complete Pod UID, container ID/start and node preconditions |
| Legacy development request | JSON schemas 1/2, no contract field/header | Existing semantics retained outside negotiated contract; cannot widen server policy |
| Preflight response | JSON schema 1, plus matching request-schema number | Old schema-2 callers receive 2; negotiated callers receive 3 |
| Admission/session handle | Kubernetes `apiVersion: tracing.kubememlens.io/v1alpha1` | Ephemeral owner-bound handle; no result replay |
| Cancellation/error | Kubernetes `apiVersion: v1`, `kind: Status` | Fixed reasons/codes; no private target data in contract errors |
| Public file/cache frames | NDJSON `version: 2` | One version per stream; aggregate/default-path policy remains fixed |
| Public OOM frames | NDJSON `version: 3` | OOM-specific event and evidence semantics; never relabelled as file/cache v2 |
| Private node binding/preflight/control | HTTP `/v1/` routes | Exact bounded messages, mTLS, installation identity and instance pins |
| Private stream activation | `/v1/` plus explicit `streamVersion` and digests | Node checks accepted format/programme before loading |
| Private worker request/results | Length-prefixed JSON `version: 1` | Canonical exact fields; installed worker/SDK/programme identity remains pinned |
| Redacted local export | Reader schemas 1–2; writer schema 2, `kind: TraceReport` | Schema 2 adds acknowledged-or-unknown contract evidence and the explicit incompatibility failure category; schema 1 remains frozen |
| Audit record | JSON `schemaVersion: 1`, `type: trace_audit` | Fixed HMAC references, categories and frozen limits; no raw event payloads |

An HTTP route version is an explicit wire-contract version; adding another numeric
field would break a strict prior decoder without adding version information.
Likewise, frame versions 2 and 3 describe different trace kinds, not a continuous
range of interchangeable encodings. Stream version 1 remains a historical decoder
format, outside negotiated contract 1.

## Additions, deprecation and retained data

Policy-critical fields, identity, ceilings, consent, retention and evidence meaning
must never acquire permissive defaults for an older reader. Existing trace payloads
use exact decoders: a new payload field requires a new representation/contract and
compatible readers before writers. An unknown field is not silently dropped to
make a request or sensitive result appear supported. Free-form diagnostic text is
not a channel for negotiating policy.

Within formats that explicitly allow additive data, preserve it within the original
byte limits and treat text as untrusted content. Existing open arrays such as export
caveats may gain explanatory entries without changing the interpretation of numeric
measurements; unknown policy fields and raw payload additions remain prohibited.
The bounded archive reader preserves original bytes and caveat text for schemas 1 and 2. It validates aliases, frame/summary semantics, numeric uncertainty and byte/event accounting; unknown fields, ambiguous JSON and unsupported versions are rejected. An imported archive is structurally validated evidence, not authenticated provenance. Its text remains untrusted.

After the first qualified release, removing an advertised contract requires notice
in the compatibility matrix and changelog for at least two subsequent minor releases
and at least 90 days, whichever is later. A security defect may disable an unsafe
operation immediately with an explicit incompatibility/unavailable response; it
must never trigger a less restrictive fallback. No notice period turns an untested
provider or experimental format into supported functionality.

The server retains no completed trace payload. In-flight handles and quotas are
in-memory and are not migrated between controller processes. Client exports are
operator-owned local files; upgrading or rolling back cannot silently rewrite,
retain remotely or delete them. Audit-key rotation changes correlation epochs,
not retained payload policy. Existing audit/export byte and privacy limits remain.

See [ADR 0021](../adr/0021-negotiate-trace-contracts-before-activation.md),
[stream semantics](STREAM.md) and [audit/retention](AUDIT_AND_RETENTION.md).

## Saved exports and validation

Current exports use schema 2. `contractVersion` is `1` only after the client has
validated the server acknowledgement; `null` means it was not observed. The new
`incompatible` failure category belongs to schema 2 and is rejected under frozen
schema 1. No success or measured zero is inferred from an unknown handshake,
missing summary or incomplete transport.

`internal/tracereport.Read` reads at most 32 KiB and preserves accepted original
bytes without rewriting an operator file. It returns a separate private archive
type that cannot be implicitly JSON-serialised or formatted into logs. Numeric
values and allowed explanatory text remain sensitive operational data. Schema
validation does not prove authorship, authenticity or that a measurement occurred.

Golden fixtures cover both retained export versions and file/cache/OOM stream
semantics, including loss, truncation, cancellation, missing terminal evidence,
correlation, maximum-width counters and unknown-future fields. Actual previous
schema-1 local reports also pass the reader unchanged. The previous/current binary
matrix, real activation guards and cleanup are recorded in
[local compatibility verification](COMPATIBILITY_LOCAL_VERIFICATION.md).
