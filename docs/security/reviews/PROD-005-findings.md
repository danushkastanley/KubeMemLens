# PROD-005 security finding register

- Review date: 26 August 2026
- Review branch: `feature/tenant-isolation-validation`
- Validated harness commit: `41a6ca5b4e8129197121f384d3b1d0fef102a011`
- Live image digest: `sha256:0de9c86466a024a7397e711e646623d4d8a69d762aa882503f2eab12d7496322`
- Local chart package SHA-256: `7a012c11881d635ea2ecf4e33dc60bac5fcf45c80c1c3a4d459f32799c128a60`
- Runtime: kind Kubernetes `v1.35.5`, Helm revision 21
- Decision: approved for pull-request review
- Open P0 findings: 0
- Open P1 findings: 0

The review used a read-only finding pass followed by separate remediation and verification passes. The authenticated GitHub pull-request merge record is the maintainer sign-off for this gate. This file records the technical decision; it is not a substitute for that signed repository event.

## Findings

| ID | Threat | Severity | Finding | Remediation owner | Status | Fix | Verification | Residual risk |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| ISO-001 | TM-017 | P1 | Agent scan errors could log cgroup paths, Pod UIDs and container IDs. | Agent | closed | Bounded agent failure reasons and counts | `go test ./cmd/memlens-agent ./internal/agent`; ISO-LIVE-007 scans every agent log | Raw errors remain available inside the process only. |
| ISO-002 | TM-022 | P1 | Unauthenticated agent metrics on the Pod network exposed node-local density counts. | Chart | closed | Loopback default, no container port or scrape annotation | Helm render checks, Pod-proxy denial and ISO-LIVE-011 from the adversary Pod | An explicit non-loopback binary override is unsafe for a shared cluster and remains documented as local-only. |
| ISO-003 | TM-011 to TM-022 | P1 evidence gate | No reviewer-owned finding register or closure rule existed. | Maintainer | closed | This register and PR sign-off rule | Register link check and authenticated PR merge record | Managed-provider qualification remains separate. |
| ISO-004 | TM-017 | P2 | The TUI capture seam accepted injected node and history slices without rechecking the selected Pod scope. | TUI | closed | Capture filters exact Pod history and at most its node; partial captures contain no nodes | `go test ./internal/tui ./internal/incident` | Display names remain in a redacted capture by design. |
| ISO-005 | TM-012 | P2 | Permission revocation cleared cached data but rendered a misleading connection error. | TUI | closed | Forbidden refresh renders access revoked and permission denied | `TestForbiddenRefreshClearsPreviouslyAuthorisedData` | Other transport failures still retain the last good frame as designed. |
| ISO-006 | TM-017 | P2 evidence gate | Leakage checks covered only one collector log sentinel and one capture namespace. | Security harness | closed | Collector and agent log scans, direct-route scans, capture checks and sanitised evidence policy | ISO-LIVE-007 | Raw Kubernetes audit retention is owned by the cluster operator. |
| ISO-007 | TM-012 | P2 evidence gate | Existing and missing out-of-scope denials had content equivalence but no timing evidence. | Security harness | closed | 60 interleaved requests with declared median, p95 and maximum budgets | Existing p95 `8.699 ms`; missing p95 `12.554 ms`; maximum `13.519 ms`; identical normalised hash | Local scheduler timing is not a managed-provider benchmark. |
| ISO-008 | TM-011 to TM-022 | P2 | The threat model described implemented authentication as future work. | Documentation | closed | Threat model status, controls and evidence links updated | Documentation and local-link checks | Optional eBPF threats remain future work. |
| ISO-009 | TM-017, TM-021 | P2 | The authentication document incorrectly said no metrics contained workload display names. | Documentation | closed | Security-decision telemetry and authorised workload metrics are documented separately | Metrics tests and documentation review | Aggregated metrics remain cluster-scoped and require the separate binding. |
| ISO-010 | TM-017 | P3 | The tenant runbook attributed status and duration to read-authorisation logs. | Documentation | closed | Read and ingestion event fields are documented separately | Documentation review | Kubernetes audit policy remains operator-owned. |
| ISO-011 | TM-017 | P3 | Legacy rollback binary errors may include a bounded response body. | Binary rollback | accepted-risk | Production chart legacy listeners, ports and ServiceMonitor were removed | Helm default and obsolete-value render checks; direct routes return `404` | The binary/client code remains only for controlled pre-v1 rollback on a trusted cluster. |

## Live closure evidence

The final local run passed with the NetworkPolicy present, absent and restored. Direct Service and Pod-IP requests with forged identity returned `401`. Pod-local legacy reads, writes and metrics returned `404`; port `8081` had no listener. An unbound workload write returned `403`. Removing the delegated-authoriser binding returned `500` without data and access recovered after exact restoration.

The 32-request burst produced 26 successful responses and six bounded admission responses. Maximum latency was `15.483 ms`; the next normal request succeeded in `4.332 ms`. Collector working set was `32,268,288` bytes before and after, with zero restarts. Cleanup restored the Helm-owned NetworkPolicy and binding and removed both tenant namespaces.

Kind does not prove CNI enforcement. It proves the application boundary is unchanged when the NetworkPolicy object is absent. PROD-008 owns enforcing-CNI and managed-provider qualification.
