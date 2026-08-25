# PROD-005 security finding register

- Review date: 26 August 2026
- Review branch: `feature/tenant-isolation-validation`
- Validated source and harness commit: `afe018ae6aa416d9cdb0b52b88b207763ea9128e`
- Local OCI image ID: `sha256:0d91b897cb8dc08c5bdd6da0afbf5590f0ac76d12d0be04952d9a3a9544561fe`
- Runtime image ID: `sha256:aea778a77bf1b990bd59e4699359a03bdec39e936b5fd174ddc00da0f1ce4f45`
- Tracked chart-source SHA-256: `8056db731bdd96978345538a65a4e93220e2fd330b5af5345efea1d963927147`
- Installed-manifest SHA-256: `87d1e32b57debd683756d9da894e14089678be3eb1773bf9751ecbfc26848666`
- Runtime: kind Kubernetes `v1.35.5`, Helm revision 24
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
| ISO-007 | TM-012 | P2 evidence gate | Existing and missing out-of-scope denials had content equivalence but no timing evidence. | Security harness | closed | 60 interleaved Pod requests and 60 history requests with declared median, p95 and maximum budgets | Pod p95 `8.604/8.794 ms`; history p95 `8.355/9.881 ms`; identical hashes within each route | Local scheduler timing is not a managed-provider benchmark. |
| ISO-008 | TM-011 to TM-022 | P2 | The threat model described implemented authentication as future work. | Documentation | closed | Threat model status, controls and evidence links updated | Documentation and local-link checks | Optional eBPF threats remain future work. |
| ISO-009 | TM-017, TM-021 | P2 | The authentication document incorrectly said no metrics contained workload display names. | Documentation | closed | Security-decision telemetry and authorised workload metrics are documented separately | Metrics tests and documentation review | Aggregated metrics remain cluster-scoped and require the separate binding. |
| ISO-010 | TM-017 | P3 | The tenant runbook attributed status and duration to read-authorisation logs. | Documentation | closed | Read and ingestion event fields are documented separately | Documentation review | Kubernetes audit policy remains operator-owned. |
| ISO-011 | TM-017 | P3 | Legacy rollback binary errors may include a bounded response body. | Binary rollback | accepted-risk | Production chart legacy listeners, ports and ServiceMonitor were removed | Helm default and obsolete-value render checks; direct routes return `404` | The binary/client code remains only for controlled pre-v1 rollback on a trusted cluster. |

## Live closure evidence

The final local run passed with the NetworkPolicy present, absent and restored. Direct Service and Pod-IP requests with forged identity returned `401`. Pod-local legacy reads, writes and metrics returned `404`; port `8081` had no listener. An unbound workload write returned `403`. Removing the delegated-authoriser binding returned `500` without data and access recovered after exact restoration.

The 32-request burst produced 29 successful responses and three bounded admission responses. Maximum latency was `11.540 ms`; the next normal request succeeded in `3.980 ms`. Collector working set was `21,700,608` bytes before and after, with zero restarts. Cleanup restored the Helm-owned NetworkPolicy and binding and removed both tenant namespaces.

The verifier matched the clean repository and both embedded binary commits to `afe018a`. It matched the expected image reference, runtime image ID, local OCI image ID, deterministic chart-source hash and installed-manifest hash before retaining any result. A metrics-only ServiceAccount could read only the aggregated metrics resource. The authorised metrics, redacted capture and final evidence directory contained no runtime identifier, label value, credential or certificate sentinel.

Kind does not prove CNI enforcement. It proves the application boundary is unchanged when the NetworkPolicy object is absent. PROD-008 owns enforcing-CNI and managed-provider qualification.
