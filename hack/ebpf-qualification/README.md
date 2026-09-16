# Local eBPF qualification

This harness measures the optional candidate; it does not grant provider support.
The [accepted protocol](../../docs/ebpf/BENCHMARK_PROTOCOL.md) remains authoritative.
No workload result, review or benchmark pass is implied by a successful sampler.

## Frozen idle experiment

The first necessary performance gate is idle cost. Before observing candidate
measurements, freeze the profile, source and observer binary hashes, candidate
image/worker/programme/policy hashes, rendered configuration, kernel and runtime.
Retain a timestamped manifest outside Git. Candidate or method changes start a new
experiment; keep every failed, invalid or interrupted run.

- Five paired repetitions in fixed order: 15 minutes with both optional services
  absent, then 15 minutes with both services Ready and no trace requests. Wait 60
  seconds after each transition before measuring. Preserve startup/teardown times
  separately; neither disappears into the warm-up record.
- Record a starting sample and 900 subsequent samples, scheduled every second
  against a monotonic clock. Reject missed samples, sampling intervals outside
  0.9–1.1 seconds, observation spans over 100 ms, and windows below 900 seconds.
  Do not interpolate missing samples or replace them with zeroes.
  Wall-clock versus monotonic drift over 100 ms also invalidates the window.
- CPU is the cumulative cgroup CPU delta divided by actual elapsed wall time,
  expressed in millicores of **one core**, without division by node CPU count.
  Retain each one-second rate and nearest-rank p95/p99, plus the time-weighted mean.
- Working set is `max(0, memory.current - memory.stat[inactive_file])`, using the
  cgroup-v2 counters. Retain both operands, anonymous/file/shmem bytes and summed
  process RSS independently. Sealed executable memory and every resident service
  cost stay included; subtract no unrelated baseline from the idle tracer cost.
- Evaluate the node service and the colocated node-plus-API installation against
  average CPU <= 5m and maximum sampled working set <= 40 MiB **in each repetition**.
  Report the API separately and their sum; it is included in the per-node cost.
- A process/cgroup lifetime change, OOM, incomplete ownership proof, missing
  baseline, changed candidate, nonzero owned BPF objects or unknown observation
  invalidates the repetition. A complete over-budget repetition is a failure,
  not an invalid run to discard. Do not retry it to select favourable results.
- Control windows verify zero optional service Pods/processes/owned objects at
  both ends; enabled windows verify the same candidate and idle owned-object
  census at both ends. No trace admission or controlled OOM runs during idle work.
- The observer runs outside measured service cgroups. Record its CPU/memory and
  the node counters; pair the same observer with the no-tracer control. Two kind
  nodes share one Linux VM/kernel, so their host CPU counters are not summed.

After all five idle pairs, an idle failure is sufficient for a candidate no-go.
In that case the remaining workload matrix is explicitly **unrun**, never passed:
closing a failing prototype does not require more privileged load experiments.
If idle passes, run the complete normal/high-rate/noisy/concurrent/flood/scan and
lifecycle protocol with a separately frozen workload profile before proposing go.
The idle experiment alone cannot produce go or conditional qualification.

## Evidence and review

Private orchestration resolves exact Kubernetes/CRI identities and passes explicit
cgroup paths/inodes to the read-only sampler. Raw output uses fixed role names and
numeric measurements: no Pod names, cgroup paths, PIDs, event rows or credentials.
The sampler neither discovers workloads nor loads BPF or changes cgroup controls.
Its output is evidence only after external provenance, ownership and window checks.

Tests use synthetic counters to verify missing-data rejection and exact inclusive
idle boundaries. Live runs must use the production candidate and retain raw samples.
Independent kernel, multi-tenancy and performance reviews remain unperformed until
actual non-implementer reviews arrive. Cloud execution remains separately gated.

Counter semantics: [Linux cgroup v2](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html).

## Commands

Run non-loading tests with `python3 -m unittest discover -s hack/ebpf-qualification`.
Build `prototype/trace/qualification/measure` for the real Linux architecture, then
install it and the accepted lifecycle census helper on the explicitly owned node.
Keep the runtime configuration private: it binds a loopback kubeconfig/context,
Node UID, both deployment UIDs/spec hashes, candidate identities and observer hashes.
The source hash is SHA-256 of the canonical JSON returned by `source_manifest()`.

Run `python3 hack/ebpf-qualification/idle_campaign.py --config <private-config>
--output <new-evidence-directory> --acknowledge-local-idle-campaign`.
The runner creates a frozen source copy before changing replicas, records every
transition and restores the same owned services on completion or failure. It uses
UID/resource-version preconditions and refuses concurrent installation changes.
Never rerun into an existing directory or delete a failed attempt to reuse its name.
Allow about 160 minutes for five pairs plus transitions. Avoid unrelated builds and
new workloads on the shared Docker VM while the experiment runs.

Recompute a completed bundle with `python3 hack/ebpf-qualification/replay_idle.py
<evidence-directory> --expected-freeze-sha256 <recorded-freeze-pin>`.
The default requires the currently reviewed source hash.
For historical measurements, supply `--expected-source-sha256` with the separately
recorded pre-measurement pin. The verifier checks the retained source files and
reports measurement and verifier hashes separately; it does not execute archived
code or pretend the original run used a later validator. Raw window timestamps
must fit their envelopes, all ten windows must be distinct and ordered, and each
controlled service replacement must produce a new lifetime.

`export_idle.py <completed-directory> <new-public-directory> --expected-source-sha256
<recorded-source-pin> --expected-freeze-sha256 <recorded-freeze-pin>` makes a bounded,
validated snapshot and exports only numeric samples,
approved metadata and the reviewed measurement source. Raw JSONL bytes are preserved
under gzip with both compressed and uncompressed hashes. Private runtime configuration,
credentials and logs are excluded. Nonempty diagnostics require review; incomplete
runs cannot be exported as complete evidence. Complete failures remain failures.

Verify an exported bundle without executing its archived source using
`verify_idle_export.py <public-directory> --expected-source-sha256 <recorded-source-pin>
--expected-freeze-sha256 <recorded-freeze-pin>`.
The verifier bounds decompression, checks the exact file inventory and hashes,
reconstructs a temporary evidence tree, then recalculates every pair with the
currently reviewed validator. Environment metadata and source should also be
reviewed before publication; schema validation is not independent human review.
Record both pins independently of the bundle being checked. The freeze pin binds
the selected image, executables, policy and environment as well as the method;
hash consistency alone is not external attestation of a measurement.
