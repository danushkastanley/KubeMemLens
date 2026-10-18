# Local replica-baseline qualification — 29 September 2026

The optional replica profile passed the functional checks in
[summary.json](summary.json), including allocation release, rollback and cleanup.
The runtime used the existing two-node kind Kubernetes 1.37.0 arm64 cluster with
LinuxKit 7.0.12 and containerd 2.3.4. These are our own local results.

## Candidate

[source.json](source.json) records production inputs built from
`c0fe709843a9f54eec217a12de724085c767c8e7` plus the replica implementation.
[test-source.json](test-source.json) records the native test inputs. The summary
retains binary hashes and the local image identity; it is not a registry reference.

The full host race suite passed, followed by race regressions for the corrected
agent ID/completeness adapters. Full vet passed. Native Linux arm64 race tests and
vet covered the engine, view, Kubernetes metadata, extension API, collector, client,
CLI, TUI, flags and allocation fixture. Linux amd64 cross-build and replica, existing
history and support contracts passed. Response tests exercise maximum bounds,
reordering, missing evidence, malformed inputs and altered statistical claims.

## Live evidence

Five Pods shared one unchanged Deployment template, ReplicaSet, immutable image and
128 MiB container limit. Running the same fixture binary inside one existing Pod
held an additional 64 MiB for ten minutes. The standard cgroup agent, collector and
authenticated API supplied all memory values; no synthetic metric service was used.

After the stability window, the candidate charge was **69,931,008 bytes**, compared
with a **1,009,664-byte** median from four references. The difference was
68,921,344 bytes and the result was higher than peers, with explicit limited-change-
history confidence. Actual retained samples subsequently qualified the bounded
growth calculation. After the allocation process exited normally, observed charge
returned to **999,424 bytes**.

Checks also covered tenant isolation, endpoint-only permission denial, missing
named access to a listed peer, invalid queries, Pod and workload API selection,
actual authenticated CLI decoding, permission revocation and restoration, unchanged
fixture identities/template during allocation, and absence of container restarts
or OOM terminations. Replacing one reference removed its old identity and prevented
its successor from inheriting stability. The reported ReplicaSet change correctly
suppressed the affected cohort during its new stability window.

Actual terminal journeys covered a Pod at 80×24 and a workload at 120×36: open,
validated evidence, final-page confidence caveat, refresh, return to live evidence
and clean exit. Unit tests cover cancelled/obsolete responses, replacement identity,
revocation clearing, stale results and wrapping.

Disabling the profile removed discovery, the endpoint, its viewer role and collector
flags. An independently authorised endpoint-only fixture confirmed HTTP 404 after
removal; ordinary replica viewers had already lost their optional route permission.
The release, three owned fixture namespaces and memory APIService were removed.
Both shared Nodes remain Ready for later qualification.

## Retained harness corrections and limits

The first terminal driver expected a Pod-table header after closing the workload
panel. It was corrected to recognise the actual workload header. A replacement
assertion initially expected three remaining references; real ReplicaSet Events
correctly suppressed all affected members instead. The assertion now checks that
policy outcome and the absence of an outlier. The first rollback probe used the
removed viewer role and was denied before reaching the endpoint; a separate named
endpoint-only role proved removal. These driver failures were retained privately;
none required relaxing a product threshold or permission check.

This run does not establish managed-provider support, independent review, adoption,
throughput or tail latency. Source caveats and informational-only behaviour remain
as documented in [replica comparisons](../../replica-baselines.md).
