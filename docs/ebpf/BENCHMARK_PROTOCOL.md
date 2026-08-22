# Optional eBPF Benchmark Protocol

Status: protocol approved for a future prototype; no eBPF measurements exist
Date: 18 July 2026

## Purpose

This protocol decides whether an optional KubeMemLens tracer is safe enough to continue towards implementation. It does not turn a prototype into a supported feature. Results must be recorded for every declared kernel/provider combination, including failure and event-loss behaviour.

Record a non-eBPF baseline in every claimed environment before measuring an optional tracer. Local fixture timings do not establish Linux node overhead.

## Test matrix

Run on dedicated disposable Linux node pools, then repeat accepted cases on representative shared nodes:

- Upstream kind or kubeadm on each minimum/supported kernel family.
- GKE Standard Linux node pool.
- EKS managed Linux node group.
- AKS Linux node pool.
- amd64 and arm64 where the provider supports both.
- containerd and every other runtime declared compatible.
- BTF present/absent, supported/unsupported tracepoints, cgroup v2, Pod churn and tracer restart.

Restricted/serverless modes must be preflighted and recorded as supported or explicitly unsupported; do not bypass provider policy.

## Workloads

For each environment run:

1. Idle tracer, no trace request.
2. One low-rate selected Pod.
3. One high-rate selected Pod issuing cached and uncached file reads/writes.
4. Ten noisy non-selected Pods in the same and different namespaces.
5. Maximum allowed concurrent selected traces.
6. Event flood beyond the configured ring/output ceiling.
7. Pod deletion/recreation, client disconnect, tracer `SIGTERM` and forced restart.
8. Standard agent scan at each live density claimed for the release.

Use fixed-seed workload generators, immutable image digests and a recorded node image/kernel. Retain scripts in `hack/` and publish raw machine-readable results with the pull request.

## Measurements

Record at 1-second resolution for at least 15 minutes per steady-state case and repeat at least five times:

- Tracer CPU seconds, throttling, RSS/working set and OOM events.
- Node CPU, memory pressure, scheduler latency and workload throughput/latency against a no-tracer control.
- BPF map/ring-buffer memory, attachment count and teardown latency.
- Events produced, delivered, sampled, lost, truncated and encoded bytes.
- First-event and p50/p95/p99 event-to-client latency.
- Authorisation/preflight/attach latency and verifier time/log size.
- Standard agent scan duration and collector latency while a trace is active.
- Zero residual KubeMemLens links, programmes, maps or pins after every termination path.

## Acceptance thresholds

All must pass before implementation can be proposed:

- Idle tracer: at most 5 millicores average and 40 MiB working set per node.
- One normal trace: at most 1% of one node core average, 3% p99, and 64 MiB incremental working set.
- Selected workload throughput or p99 latency regression: below 2% against a paired control.
- Non-selected workload regression: below 1%; zero events from non-selected cgroups in user-visible or diagnostic output.
- Event delivery p95 below 250 ms and p99 below 1 second under the normal case.
- Normal case event loss below 0.1%; any loss is explicitly reported. Flood cases may sample/drop but must stay within resource caps.
- Stop/cancel/disconnect teardown below 2 seconds; forced-restart cleanup below 10 seconds; zero residual owned attachments.
- Hard duration, event, output, map-memory and concurrency ceilings cannot be exceeded by crafted input.
- Standard cgroup scan p95 regression below 5% while one normal trace is active.

A missed security/isolation limit is a hard failure regardless of average performance. Threshold changes require measurement evidence and an ADR, not a larger default.

## Procedure

1. Record cluster, provider, node image, kernel, runtime, architecture, mitigations and KubeMemLens build/provenance.
2. Warm the workload without the tracer and collect the paired control.
3. Install the optional profile by image digest and capture rendered manifests.
4. Run preflight and save supported/degraded fields.
5. Run each workload with fixed bounds and seed; collect Kubernetes, node, BPF and application measurements.
6. Trigger every cancellation/failure case and enumerate BPF state afterwards.
7. Repeat and calculate median plus p95/p99 where sample size permits. Do not hide failed runs.
8. Publish commands, raw results, summaries and limitations; remove the cluster/node pool.

## Review gate

Performance results require review by a maintainer who did not implement the prototype. Multi-tenant isolation and loader teardown require a separate security reviewer. Provider support enters `docs/compatibility.md` only after the corresponding node-pool result is reproducible in CI or a documented release qualification run.
