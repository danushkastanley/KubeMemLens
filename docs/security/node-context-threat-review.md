# Node-context threat review

Status: contract and local collection/ingestion review. Managed-provider and
incident-capture mitigations require their separate qualification.
Read with the [main model](KubeMemLens-threat-model.md),
[contract](../node-context.md) and [ADR 0007](../adr/0007-isolate-node-context-producers-and-reads.md).

The dedicated producer crosses a new boundary by reading cross-tenant kubelet
data. It posts a bounded normalised record through aggregation; the collector
never receives its kubelet bearer credentials.

| Threat | Required mitigation | Verification |
| --- | --- | --- |
| Token theft reads another Node | Separate SA, projected rotation, explicit residual risk and qualified network limits; own-node application checks are not token containment | NODE-002/006 transport and profile tests |
| Forged target or redirect exfiltrates token | Authenticated SelfSubjectReview Node name/UID binding, fixed HTTPS path, trusted CA/SAN, no redirects or environment proxies | NODE-002 hostile HTTPS, substituted Node name/UID and direct kubelet tests |
| CA or audience differs from API server | Explicit trust/audience qualification; stop preflight without bypass | NODE-002/006 preflight and rotation tests |
| New producer retires cgroup agent or clears its data | Fixed authenticated roles, independent ownership, separate store operation, cross-role payload rejection | NODE-003 two-producer replacement/race tests |
| Replay after restart or Node-name reuse | Epoch/sequence/digest, retired identities, current Node UID and start-time binding | NODE-003 replay/recreation tests |
| Failure report refreshes stale evidence | Separate report/sample/receive times, retained last good sample, no history append on failure | NODE-003/005 source-loss tests |
| Node-only caller obtains tenant identities or gaps | Separate viewer binding; cluster Pod authorisation before lookups; no namespace/global subtraction | NODE-003 endpoint and NODE-004 secondary-authorisation/scope tests |
| Revoked access survives cache/history/capture | Current uncached authorisation; clear protected state and block fresh exports | NODE-003/005 revocation tests |
| Summary leaks names/paths/volumes | Discard unrelated fields, fixed categories/codes, no raw bodies or errors in telemetry/capture | NODE-002 privacy and NODE-005 capture tests |
| Large/nested response exhausts memory | Byte/depth/token ceilings, bounded fields, deadline, one request, store/history/admission limits | NODE-002 fuzz and NODE-003 capacity tests |
| Mixed definitions/times imply false accounting | Pure source-aware rules, disjointness evidence, explicit confidence and incomplete-coverage suppression | NODE-004 property/paired-sample tests |
| New fields break strict clients | Negotiated explicit schema 1/2 support and separate incident schema 4 | NODE-003/005 compatibility tests |

## Disposition

Authenticated roles, separate store operations and current Node UID inventory
now enforce producer isolation. Local Kubernetes verification exercises positive
ingestion, Node-only reads, denied tenant/Pod access, immediate viewer revocation,
producer replacement, collector rebuilding and profile enable/disable. Unit
checks cover replay, individual source clocks, bounded history and strict schema
1/2 compatibility. These checks do not qualify managed networks or providers.

Operator accounting files are trusted configuration, bounded by size and expiry,
and tied to a Node UID and boot. Producers and read queries cannot qualify their
own arithmetic. Operator review and invalidation after runtime/configuration
changes remain necessary; an evidence digest is not a certification.

Schema-4 capture introduces an untrusted offline-file boundary. A typed preflight
bounds fields and arrays before allocation and rejects duplicate/case-aliased keys;
validation then checks source observations, identities, access metadata, finite
signals and history bounds. Node-only captures cannot contain contributor fields.
Capture reads analysis last and does not accept a screen's cached authorisation.
Failed reads and oversized exports preserve existing files and emit no partial
stdout. Default Node UID fingerprints remain correlatable; contributor aliases
are local to the file. Files are private, not signed attestations, and must not be
treated as independent proof of accounting or provider qualification.

Residual risks remain stats access with a stolen optional token, inference from
explicitly authorised Node aggregates, and provider-specific TLS/network limits.
They are disclosed, not described as solved by application checks or portable
NetworkPolicy. No managed-provider or portable NetworkPolicy enforcement qualification is claimed.
Revisit this review when fields, permissions, endpoints, bounds or identities change.
