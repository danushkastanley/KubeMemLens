# Tenant isolation validation

This plan exercises the production Kubernetes aggregated API. It assumes a disposable kind cluster, the authenticated chart profile and two tenant namespaces. The test never treats NetworkPolicy as an authorisation decision.

Run the live suite with:

```bash
TENANT_READ_KUBECONFIG=<disposable-kind-kubeconfig> \
TENANT_READ_CONTEXT=kind-<name> \
TENANT_READ_NAMESPACE=kube-memlens \
TENANT_READ_CLI=<absolute-path-to-kubectl-memlens> \
ISOLATION_ACKNOWLEDGE=remove-and-restore-kube-memlens-security-controls \
ISOLATION_EXPECTED_COMMIT=<full-source-commit> \
ISOLATION_EXPECTED_IMAGE_REFERENCE=<installed-image-reference> \
ISOLATION_EXPECTED_RUNTIME_IMAGE_ID=<resolved-Pod-image-ID> \
ISOLATION_EXPECTED_LOCAL_IMAGE_ID=<local-OCI-image-ID> \
ISOLATION_EXPECTED_CHART_SOURCE_SHA256=<tracked-chart-tree-hash> \
make verify-tenant-isolation-kind
```

The acknowledgement permits the suite to remove and restore only the Helm-owned `kube-memlens-collector` NetworkPolicy. The cleanup trap restores the policy and delegated-authorisation binding before it removes test fixtures. Tokens, certificates, kubeconfigs, response bodies and raw logs stay in a mode-`0700` temporary directory and are deleted at exit.

## Deterministic threat plan

| Test | Threat | Adversary action | Required result |
| --- | --- | --- | --- |
| ISO-LIVE-001 | TM-011 | An unbound tenant ServiceAccount submits a node snapshot through Kubernetes and sends a bearer token plus forged identity headers to the extension Service and Pod IP. | Kubernetes returns `403`; direct TLS never returns an authenticated response; the store does not change. |
| ISO-LIVE-002 | TM-012 | A namespace reader lists, gets and reads history in another namespace, then uses CLI polling, compare, recommendation and capture paths. | Every cross-namespace operation is forbidden. Polling stops after revocation. No prior or foreign data remains in the TUI or capture. |
| ISO-LIVE-003 | TM-013 | A compromised workload reaches the Service and Pod directly, before and after NetworkPolicy removal. | Forged request headers and bearer tokens never authenticate. Reachability changes do not change the result. |
| ISO-LIVE-004 | TM-014 | An agent claims another node and reuses a token after its bound Pod is deleted. | The node claim is rejected and the deleted-Pod token becomes unauthorised. |
| ISO-LIVE-005 | TM-015 | An agent repeats a sequence, changes a repeated payload, lowers the sequence and sends a mismatched epoch. | Exact duplicates are idempotent. Conflicting, lower and wrong-epoch requests are rejected without store mutation. |
| ISO-LIVE-006 | TM-016 | The suite removes the collector's delegated-authorisation binding during an otherwise valid read. | The read returns no diagnostic data. Restoring the exact binding restores normal access. No direct fallback appears. |
| ISO-LIVE-007 | TM-017 | The suite scans status bodies, captures, metrics, collector logs, agent logs and retained evidence for synthetic secrets and runtime identifiers. | No token, certificate, raw identity, Pod UID, container ID, cgroup path, label sentinel or data outside the principal's authorised scope appears. |
| ISO-LIVE-008 | TM-018 | The collector ServiceAccount attempts Pod, Node object, Secret, KubeMemLens tenant and metrics reads, then checks the exact Node list contract. | Kubernetes denies every unneeded read, permits only the documented Node list, request-header ConfigMap read and SubjectAccessReview create permissions. |
| ISO-LIVE-009 | TM-019 | A tenant changes namespace, object name, selector and continuation scope across read routes. | Route-derived SubjectAccessReview attributes remain exact. Cross-scope tokens and selectors return no foreign data. |
| ISO-LIVE-010 | TM-020 | Install, certificate reuse or rotation, rollback and uninstall exercise the bootstrap resources. | Trust is not widened, temporary RBAC is removed and the APIService uses the expected CA bundle. |
| ISO-LIVE-011 | TM-021 | A tenant requests metrics, calls Pod-local `/metrics` and tries a Service proxy path. A metrics-only principal requests the aggregated metrics resource. | Tenant and direct routes return no workload metrics. The metrics-only principal can read only the aggregated metrics resource. |
| ISO-LIVE-012 | TM-012, TM-017 | Existing and missing out-of-scope Pod and history requests run in an interleaved timing sample. | Status, reason and normalised response hash match. Median and p95 deltas stay within the predeclared materiality budget. |
| ISO-LIVE-013 | TM-016, TM-017 | Thirty-two concurrent authorised reads target the API. Separate unit and ingestion suites send malformed, oversized and rate-limited requests. | Results are bounded success or admission rejection, every request completes within the server timeout, the collector does not restart and a normal read recovers within two seconds. Malformed and oversized inputs fail closed. |

The content test replaces only the caller-supplied object name before hashing. It requires the same HTTP status, Kubernetes reason, content type and normalised hash. For 30 interleaved samples per target, the median delta must not exceed 50 ms, the p95 delta must not exceed 250 ms and no request may exceed two seconds. These thresholds detect a material existence oracle without turning scheduler noise into a flaky gate.

## Evidence sources

- `make verify-tenant-isolation-kind` owns ISO-LIVE-001 to ISO-LIVE-003, ISO-LIVE-006 to ISO-LIVE-008, ISO-LIVE-011, ISO-LIVE-012 and the concurrent-read part of ISO-LIVE-013.
- `make verify-authenticated-ingestion-kind` owns ISO-LIVE-004, ISO-LIVE-005 and the forged, replayed, rate-limited, compressed and oversized write cases.
- Go tests own exact route-to-SubjectAccessReview parity, cross-scope continuation rejection, malformed read bounds, authoriser deny/no-opinion/error behaviour and zero store work on failure.
- The complete `hack/e2e-kind.sh` lifecycle owns ISO-LIVE-010. CI runs all four evidence sources on the extended Kubernetes 1.37 job; install, upgrade and rollback keep the tenant-read contract active.

The E2E runner calculates and supplies the build-identity values shown above. A manual run must do the same. The verifier rejects a dirty repository, a source-commit mismatch, a different image reference or runtime image ID, a binary without the embedded full commit, or a different tracked chart-source hash.

## Direct listener contract

The compromised workload probes these exact paths:

| Target | Expected result |
| --- | --- |
| Collector Pod `:8080/healthz` | `200` with the generic health body |
| Collector Pod `:8080/api/v1/pods` | `404` and no diagnostic data |
| Collector Pod `:8080/api/v1/debug/store` | `404` and no store data |
| Collector Pod `:8080/metrics` | `404` and no metrics |
| Collector Pod `:8081` | No listener |
| Collector Service `:443` and Pod `:8443` with forged identity | `401` or a transport failure, never diagnostic data |
| Agent Pod metrics | Bound to loopback and unreachable from another Pod |

Kind's default networking does not prove NetworkPolicy enforcement. The removal phase proves the stronger property needed here: authentication and authorisation do not change when the policy object is absent. Enforcing-CNI behaviour remains part of provider qualification.

## Before and after traceability

| Threat | Before PROD-005 | PROD-005 control and evidence |
| --- | --- | --- |
| TM-011 | Authenticated ingestion tests covered forged writes from outside a tenant workload. | ISO-LIVE-001 runs from an in-cluster adversary and checks direct and aggregated paths. |
| TM-012 | Two namespaces covered list, detail, history, CLI and revocation. | ISO-LIVE-002 adds compromised-workload, capture and stale-frame checks. |
| TM-013 | A port-forward test rejected forged request headers. | ISO-LIVE-003 repeats Service and Pod-IP attacks with NetworkPolicy present and absent. |
| TM-014 | Wrong-node and bound-token revocation tests existed. | ISO-LIVE-004 retains both as required regression checks. |
| TM-015 | Duplicate, conflicting, lower-sequence and epoch-mismatch checks existed. | ISO-LIVE-005 retains them as required regression checks. |
| TM-016 | Unit tests denied no-opinion decisions. | ISO-LIVE-006 adds delegated-authorisation failure injection and recovery. |
| TM-017 | Extension logs and one capture path had sentinel checks. | ISO-LIVE-007 scans every retained output class and sanitises cgroup errors. |
| TM-018 | Rendered RBAC was reviewed statically. | ISO-LIVE-008 exercises the live collector identity against forbidden resources. |
| TM-019 | Policy code used route-derived attributes. | ISO-LIVE-009 adds route parity and cross-scope continuation tests. |
| TM-020 | Lifecycle tests covered bootstrap cleanup. | ISO-LIVE-010 keeps install, rotation, rollback and uninstall in the release gate. |
| TM-021 | Aggregated metrics used a separate role and direct metrics were disabled in the secure profile. | ISO-LIVE-011 checks tenant, metrics-only, Service-proxy and direct Pod routes. |

## Retained evidence

The live suite writes one sanitised JSON summary. It contains test identifiers, result classes, counts, response hashes, latency summaries, restart deltas, a NetworkPolicy spec hash, Kubernetes version and commit. It does not contain object names, raw responses, tokens, certificates, kubeconfigs, logs or runtime identifiers.

The finding register is [PROD-005 findings](reviews/PROD-005-findings.md). Incident containment steps are in the [tenant isolation incident runbook](../runbooks/tenant-isolation-incident.md).

## Reference result

On 26 August 2026, the standalone isolation suite passed on kind Kubernetes `v1.35.5` at source and harness commit `afe018a`. The local OCI image ID was `sha256:0d91b897cb8dc08c5bdd6da0afbf5590f0ac76d12d0be04952d9a3a9544561fe`; both running binaries reported the same embedded commit.

- Direct forged Service and Pod-IP requests returned `401`; the unbound snapshot write returned `403`.
- The health listener returned `200`; legacy read, write and metrics paths returned `404`; port `8081` had no listener.
- NetworkPolicy removal did not change any authentication result. Exact restoration matched spec hash `bb7fef59935af729426a793cde04c247fbed69061d2ad12dc6b072f87a2be3d3`.
- Delegated-authoriser removal returned `500` without data and recovered after restoration.
- Sixty existing/missing Pod denials had one normalised body hash. Existing p95 was `8.604 ms`, missing p95 was `8.794 ms`, and maximum latency was `9.777 ms`.
- Sixty existing/missing history denials had one normalised body hash. Existing p95 was `8.355 ms`, missing p95 was `9.881 ms`, and maximum latency was `10.895 ms`.
- Of 32 concurrent reads, 29 succeeded and three received bounded admission responses. Maximum latency was `11.540 ms`; recovery took `3.980 ms`.
- Collector working set remained `21,700,608` bytes with zero restarts.

The evidence also records runtime image ID `sha256:aea778a77bf1b990bd59e4699359a03bdec39e936b5fd174ddc00da0f1ce4f45`, tracked chart-source hash `8056db731bdd96978345538a65a4e93220e2fd330b5af5345efea1d963927147`, installed-manifest hash `87d1e32b57debd683756d9da894e14089678be3eb1773bf9751ecbfc26848666` and Helm revision 24. The retained JSON contains only sanitised classes, hashes, timings and counts. Cleanup confirmed that both Helm-owned controls were restored and both fixture namespaces were absent.
