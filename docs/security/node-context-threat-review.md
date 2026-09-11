# Node-context threat review

Status: NODE-001 design review. Runtime mitigations need later validation.
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
| Node-only caller obtains tenant identities or gaps | Separate viewer binding; cluster Pod authorisation before lookups; no namespace/global subtraction | NODE-003 endpoint and NODE-004 scope tests |
| Revoked access survives cache/history/capture | Current uncached authorisation; clear protected state and block fresh exports | NODE-003/005 revocation tests |
| Summary leaks names/paths/volumes | Discard unrelated fields, fixed categories/codes, no raw bodies or errors in telemetry/capture | NODE-002 privacy and NODE-005 capture tests |
| Large/nested response exhausts memory | Byte/depth/token ceilings, bounded fields, deadline, one request, store/history/admission limits | NODE-002 fuzz and NODE-003 capacity tests |
| Mixed definitions/times imply false accounting | Pure source-aware rules, disjointness evidence, explicit confidence and incomplete-coverage suppression | NODE-004 property/paired-sample tests |
| New fields break strict clients | Negotiated explicit schema 1/2 support and separate incident schema 4 | NODE-003/005 compatibility tests |

## Disposition

The contract resolves competing producer ownership and implicit Pod access through
Node resources before collection exists. Both have explicit invariants and
required implementation tests. The chart rejects enablement, so this change
creates no runtime trust path.

Residual risks remain stats access with a stolen optional token, inference from
explicitly authorised Node aggregates, and provider-specific TLS/network limits.
They are disclosed, not described as solved by application checks or portable
NetworkPolicy. No runtime isolation or managed-provider qualification is claimed.
Revisit this review when fields, permissions, endpoints, bounds or identities change.
