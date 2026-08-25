# Tenant isolation validation

This plan exercises the production Kubernetes aggregated API. It assumes a disposable kind cluster, the authenticated chart profile and two tenant namespaces. The test never treats NetworkPolicy as an authorisation decision.

Run the live suite with:

```bash
ISOLATION_KUBECONFIG=<disposable-kind-kubeconfig> \
ISOLATION_CONTEXT=kind-<name> \
ISOLATION_CLI=<absolute-path-to-kubectl-memlens> \
ISOLATION_ACKNOWLEDGE=remove-and-restore-kube-memlens-network-policy \
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
| ISO-LIVE-005 | TM-015 | An agent repeats a sequence, changes a repeated payload, lowers the sequence and retries a request from an old collector epoch. | Exact duplicates are idempotent. Conflicting, lower and old-epoch requests are rejected without store mutation. |
| ISO-LIVE-006 | TM-016 | The suite removes the collector's delegated-authorisation binding during an otherwise valid read. | The read returns no diagnostic data. Restoring the exact binding restores normal access. No direct fallback appears. |
| ISO-LIVE-007 | TM-017 | The suite scans status bodies, captures, metrics, collector logs, agent logs and retained evidence for synthetic secrets and runtime identifiers. | No token, certificate, raw identity, Pod UID, container ID, cgroup path, label sentinel or data outside the principal's authorised scope appears. |
| ISO-LIVE-008 | TM-018 | The collector ServiceAccount attempts to read Pods, Nodes, Secrets and workloads or mint credentials. | Kubernetes denies every operation outside the documented request-header ConfigMap read and SubjectAccessReview create permissions. |
| ISO-LIVE-009 | TM-019 | A tenant changes namespace, object name, selector and continuation scope across read routes. | Route-derived SubjectAccessReview attributes remain exact. Cross-scope tokens and selectors return no foreign data. |
| ISO-LIVE-010 | TM-020 | Install, certificate reuse or rotation, rollback and uninstall exercise the bootstrap resources. | Trust is not widened, temporary RBAC is removed and the APIService uses the expected CA bundle. |
| ISO-LIVE-011 | TM-021 | A tenant requests metrics, calls Pod-local `/metrics` and tries a Service proxy path. A metrics-only principal requests the aggregated metrics resource. | Tenant and direct routes return no workload metrics. The metrics-only principal can read only the aggregated metrics resource. |
| ISO-LIVE-012 | TM-012, TM-017 | Existing and missing out-of-scope Pod and history requests run in an interleaved timing sample. | Status, reason and normalised response hash match. Median and p95 deltas stay within the predeclared materiality budget. |
| ISO-LIVE-013 | TM-016, TM-017 | Thirty-two concurrent authorised reads and malformed bounded requests target the API. | Results are bounded success or admission rejection, every request completes within the server timeout, the collector does not restart and a normal read recovers within two seconds. |

The content test replaces only the caller-supplied object name before hashing. It requires the same HTTP status, Kubernetes reason, content type and normalised hash. For 30 interleaved samples per target, the median delta must not exceed 50 ms, the p95 delta must not exceed 250 ms and no request may exceed two seconds. These thresholds detect a material existence oracle without turning scheduler noise into a flaky gate.

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
| TM-015 | Duplicate, conflicting and lower sequences were covered. | ISO-LIVE-005 adds old-epoch rejection after collector restart. |
| TM-016 | Unit tests denied no-opinion decisions. | ISO-LIVE-006 adds delegated-authorisation failure injection and recovery. |
| TM-017 | Extension logs and one capture path had sentinel checks. | ISO-LIVE-007 scans every retained output class and sanitises cgroup errors. |
| TM-018 | Rendered RBAC was reviewed statically. | ISO-LIVE-008 exercises the live collector identity against forbidden resources. |
| TM-019 | Policy code used route-derived attributes. | ISO-LIVE-009 adds route parity and cross-scope continuation tests. |
| TM-020 | Lifecycle tests covered bootstrap cleanup. | ISO-LIVE-010 keeps install, rotation, rollback and uninstall in the release gate. |
| TM-021 | Aggregated metrics used a separate role and direct metrics were disabled in the secure profile. | ISO-LIVE-011 checks tenant, metrics-only, Service-proxy and direct Pod routes. |

## Retained evidence

The live suite writes one sanitised JSON summary. It contains test identifiers, result classes, counts, response hashes, latency summaries, restart deltas, a NetworkPolicy spec hash, Kubernetes version and commit. It does not contain object names, raw responses, tokens, certificates, kubeconfigs, logs or runtime identifiers.

The finding register is [PROD-005 findings](reviews/PROD-005-findings.md). Incident containment steps are in the [tenant isolation incident runbook](../runbooks/tenant-isolation-incident.md).
