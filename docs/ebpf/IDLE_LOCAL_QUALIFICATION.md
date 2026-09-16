# Local idle qualification: candidate rejected

Date: 16 September 2026. Five complete paired repetitions failed the mandatory
idle-memory gate. This is a no-go for the evaluated candidate, not supported
tracing, full benchmark qualification or R7 readiness.

## Result

Each repetition contains a 15-minute control with both optional services stopped
and a 15-minute enabled window with no trace request. Both transitions have a
60-second warm-up. All ten windows contain 901 samples, including the initial
counter sample: 9,010 samples and 150 minutes of measurement in total.

The node service alone exceeded **40 MiB working set in every repetition**.
The colocated API plus node also exceeded the frozen **5 millicore mean CPU** gate.
CPU is normalised to one core, not divided by the machine's core count.

| Pair | Node mean CPU (m) | Node peak WSS (MiB) | Combined mean CPU (m) | Combined peak WSS (MiB) | Idle result |
| --- | --- | --- | --- | --- | --- |
| 1 | 2.361 | 85.84 | 7.346 | 105.30 | Fail |
| 2 | 2.172 | 88.14 | 7.046 | 108.79 | Fail |
| 3 | 2.100 | 88.92 | 6.670 | 107.59 | Fail |
| 4 | 2.070 | 86.07 | 7.323 | 105.07 | Fail |
| 5 | 2.043 | 87.80 | 6.772 | 106.97 | Fail |

Working set is `max(0, memory.current - memory.stat[inactive_file])`; raw operands,
RSS, shmem, CPU/throttling counters, OOM events, node pressure and observer cost
are retained. No control subtraction or removal of executable memory was applied.
Windows lasted 900.000–900.004 seconds. The largest sampling interval was 1.010
seconds and the largest counter-read span was 7.693 ms, inside the predeclared
0.9–1.1 second interval and 100 ms read limits. No failed repetition was discarded.

## Candidate and environment

The incident candidate is implementation commit
`57c300f74cfd216fe4c916e8c14dd6ae48b12d94`, merged through PR #119.
The environment was Kubernetes 1.37.0, containerd 2.3.4 and LinuxKit 7.0.12 arm64
in a disposable two-node kind cluster. Both nodes share one Linux VM/kernel.
No native amd64, managed-provider or second-kernel qualification is claimed.

| Artifact | SHA-256 |
| --- | --- |
| Local image | 4a957b8fa80324e9ce8a66d6876766eb4d1ea13ea85a83973b245e4238b741a3 |
| Node/API executable | e4f071c6eb61aedde3e0ebe12f4713b5bc524b6210e555c83d8fb57433becab3 |
| Incident worker | 6fbd2d17ea799fe84f57113454bf2071ea5cea1c741fa5529eb9fcbc466c1ffd |
| Engine release | 49f6beac38ff9310a3863b7bad3f14854890fba4403d7d49d170b310eb854e08 |
| Programme index | a968adef5da2266edffafe054ab8b3ed782de181a363974636eebda30d9983c2 |
| Read-only sampler | bc2ad494a4032d7ea61db176c49834f76977f7b4bbbddfc4356d7791d41231c1 |

Services retained their existing two-CPU/512 MiB containment limits, one installation
replica each and the accepted node BPF/PERFMON profile. The API was capability-free.
The sampler ran outside both service cgroups. Source, image, policy, executable,
process lifetime and cgroup identity checks bound every window to the candidate.

## Why memory fails

The [installation code](../../prototype/trace/workerinstall/executable_linux.go)
copies the verified worker into a sealed executable memfd. The
[node runtime](../../prototype/trace/workerruntime/runtime_linux.go) retains that
copy while idle to prevent path replacement and in-place mutation.

The accepted worker is 72,338,686 bytes (68.99 MiB). Its page-rounded copy is
72,339,456 bytes; every enabled sample recorded 72,343,552 shmem bytes, consistent
with that copy plus one 4 KiB policy page. This is a source/size correlation,
not independent page-by-page attribution. Process RSS alone omits retained
unmapped backing pages and cannot replace the specified working-set measurement.

Read-only ELF inspection found 50,887,604 bytes (48.53 MiB) of loadable file content.
Removing debug/symbol sections alone would not make this eager-copy design fit
40 MiB. No alternative build or lifetime design was benchmarked, and no integrity
check or budget was weakened to obtain a pass.

## Evidence and replay

The [public evidence manifest](evidence/r6-idle-2026-09-16/manifest.json) indexes all
ten raw windows, their envelopes, every failed pair result, the frozen measurement
source and the recalculated result. JSONL is gzip-compressed with compressed and
raw-byte hashes. It contains numeric counters and opaque component-lifetime proofs,
not Pod/Node names, cgroup paths, PIDs, credentials or event rows.

The original measurement source was frozen throughout all ten windows. A subsequent
validator-only repair rejects duplicate JSON keys, relabelled/reused windows,
mismatched absolute timestamps, repeated replacement lifetimes and stale census.
Measurement and validator identities are reported separately; archived code is
not executed by replay. The frozen-metadata receipt was verified during the campaign
against its original configuration/source/artifact inputs, not externally attested.

From the repository root:

```sh
python3 hack/ebpf-qualification/verify_idle_export.py \
  docs/ebpf/evidence/r6-idle-2026-09-16 \
  --expected-source-sha256 4bca6c839a0b23f5b9c3d61a0f4f4681d65ebb6db89ffb418ffb1f69caafdcfd \
  --expected-freeze-sha256 98efc7e12b06331ca57cb3336a37a402a46e6e4f11f74a3f90956fc9950b3559
```

See the [frozen method and tooling](../../hack/ebpf-qualification/README.md).
Complete failing results remain failures through export and replay. Private runtime
configuration, credentials and logs are excluded from the public bundle.

## Limits, teardown and review

Controls stopped service Pods and verified their CRI instances exited, while keeping
APIService registration, RBAC/configuration, ordinary fixtures and Cilium. Discovery
retries may affect control-plane CPU. These controls do not qualify general node,
selected/nonselected-workload or collector-scan regressions. The absolute idle-memory
failure does not depend on subtracting that baseline.

No incident trace or new OOM attempt ran during this experiment. Before and after
each enabled window, the owned worker/object census was empty and service lifetimes
matched. The final sweep found all 1,389 captured maps, 774 programmes and 725 links
from retained lifecycle records absent. Known Cilium map/link/programme controls
remained unchanged through optional-profile shutdown. The owned APIService was
removed with a UID precondition and both optional services were stopped.
The dedicated cluster was then removed after verifying its fixture inventory and
absence of PVs/PVCs. Both owned node containers were removed; the three unrelated
running Docker containers were preserved. No global BPF cleanup operation ran.

All three Go modules passed native Linux module verification, race tests, vet and
Linux arm64/amd64 builds. The sampler reproduced byte-for-byte in the pinned native
compiler. The evidence tooling passed 32 tests, including complete-failure retention,
historical source/candidate binding and corrupt/private-payload rejection. Independent
human review remains separate from these implementer-run checks.

The normal/high-rate/noisy/concurrent/flood workload benchmarks, delivery/loss and
attach/verifier/scheduler latency measurements, scan regressions, independent reruns
and managed-provider cases remain unrun under BPF-008. Existing
[lifecycle](LIFECYCLE_LOCAL_QUALIFICATION.md) and semantic reports retain their own
scope and do not substitute for those performance measurements.

Independent kernel, multi-tenancy and performance reviews remain unperformed;
[review inputs and unresolved findings](QUALIFICATION_REVIEW.md) are retained.
No finding or redistribution obligation is waived. EKS remains deferred. See
[ADR 0015](../adr/0015-reject-current-ebpf-candidate-on-idle-cost.md) for the decision.
