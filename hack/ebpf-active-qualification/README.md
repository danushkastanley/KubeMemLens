# Active qualification accounting

This harness is separate from the frozen historical idle experiment. It currently
runs the explicit owned local normal case and validates its measurements. It cannot
issue a full qualification verdict. Running the controller starts bounded fixture
workloads and submits trace requests; importing accounting helpers does neither.

The schema-2 `active-measure` command samples explicit cgroup roles at 1 Hz for up
to 1,800 seconds. `samples.py` checks exact fields, counters, order, duration,
clock drift, read spans and the requested role inventory. A reset, OOM or missing
sample fails validation. Missing process or scheduler observations remain explicit
states, never zero counters. Task scheduling deltas require the same opaque cohort;
changes and counter resets remain gaps. Cumulative runqueue wait is not a latency
percentile. The local kernel's absent node-wide scheduler source stays unsupported.

The administrative lifecycle helper's bounded `watch` mode preserves executable,
process, target and object ownership checks on every sample. `activity.py` requires
unchanged complete owned map/programme/link inventories, one selected worker, an
active control and no excluded worker to bracket a whole resource interval. It
covers the counter reads and clock uncertainty. Partial, unavailable, inactive,
replaced or missing brackets are unproven time. Expected object counts must come
from the independently frozen accepted programme inventory, not from the run.
The current helper records its own hashing/census CPU and RSS. Preserve that cost
in Node and workload comparisons; do not present it as zero overhead.

`resources.py` retains **all** installation CPU, including loader starts and gaps.
It reports both wall-time cost and all CPU divided by observed active time. The
normal CPU check keeps the conservative average <=10m and p99 <=30m. Memory is
reported as current minus inactive file, floored at zero, summed at each sample;
`memory.py` adds a separately validated paired observation check. A resource
check does not establish workload regression, event latency/loss, scan/collector
performance, lifecycle cleanup or a supported profile.

Before calling the accounting functions, validate the full streams and verify
candidate, source and observer hashes and externally bound lifetimes. A proposed
normal window uses repeated **30-second** admissions, matching the actual API's
`DefaultPolicy`, rather than the constructor's five-minute hard ceiling. Freeze
the complete schedule and evaluator before observing campaign results. Do not
extend production limits or count this unit-test corpus as runtime qualification.

```sh
python3 -m unittest discover -s hack/ebpf-active-qualification -p 'test_*.py'
```

Tests exercise missing data, reset/cohort handling, clock/read gaps, disclosure
rejection, attachment replacement and the inclusive CPU limit. They also verify
that inactive time cannot dilute startup/gap cost. Native observer tests and a runner integration check are required before a normal
campaign. Remaining protocol measurements are still required for qualification.

## Paired operation latency

`workload.py` validates both complete persistent C workload series against the
same mode, operation count and period. It compares exact operation indices.
Enabled operations use their actual monotonic start/end times and must be fully
bracketed by unchanged owned attachments to enter the active subset. Workload and
witness clock alignment is checked. The original all-operation comparison and
all unproven/transition operations remain in the result; they are never discarded.

Both the all-operation and active-subset p99 regressions must be **below 2%**.
Exactly 2% fails; the decision uses integer arithmetic. Minimum active operations
must be declared before the run alongside its minimum observed active duration.
Incomplete, retimed, missing or altered-I/O series cannot produce a passing pair.
The fixed completed byte rate is explicitly paced workload delivery, not maximum
storage throughput. Operation elapsed time is not event-to-client latency.

These functions still need the full frozen orchestration/provenance envelope,
kernel allocation attribution, event latency/loss, standard scan/collector observations
and lifecycle verification before any campaign can qualify. The partial accounting
result deliberately cannot stand in for those unrun gates.

## Paired working set

`memory.py` requires a complete no-tracer control and an enabled window with the
same explicitly declared shared cgroup roles plus the Node and API services. It
validates both full streams, including every startup and transition sample. For
each shared role, its lowest control working set is the reference. Positive
enabled growth is added to the full tracer working set; a workload's lower memory
cannot offset tracer or collector cost. Sum roles at each observation before
selecting the peak. The unchanged observed working-set check is inclusive at
64 MiB; even a one-byte excess fails. RSS is never substituted for working set.

This is conservative accounting for the observed cgroups, not a whole-node memory
measurement or causal attribution. Freeze the role set, source/candidate hashes,
workload schedules and lifetime bindings before a campaign. Verify the kernel's
BPF allocation charging separately; absent allocation attribution or unobserved
process cost prevents a complete memory qualification. The returned result cannot
issue a qualification verdict. Boundary, startup-spike, role/reset/clock/OOM and
control-peak regressions are covered by offline tests; no live paired campaign has
yet supplied this function with qualified inputs.

## Standard service metrics projection

`standard_metrics.py` accepts bounded, complete OpenMetrics responses and exports
only the existing agent's numeric scan/post counters, scan duration, completion
time and inventory counts, plus collector ingestion result counters and latest
duration. Durations are converted to integer nanoseconds with upward rounding.
Missing required families, duplicate selected series, unknown result labels,
non-finite values, imprecise large counters and truncated responses fail. Other
metric families, which can contain Node or workload identities, are discarded.

The collector renderer creates each result series on its first occurrence. Only
absent members of its explicit closed result vocabulary become zero; a missing
entire family cannot become a successful empty observation. Unrelated ingestion
freshness gauges are not ingestion events. Both production renderers were tested
with synthetic inputs; this does not establish live scrape or runtime performance.

The parser does not compute latency distributions. A later sampler must bind the
exact service process and endpoint, cap transport reads, keep the existing
loopback listener, and retain every polling interval. Deduplicate the latest scan
gauge using its attempt counters; counter jumps mean missed durations. Bracket
whole scans against observed attachments, including the completion timestamp's
one-second precision. Collector duration gaps must remain explicit rather than
being presented as a complete latency distribution.

## Live standard-service observation and scan comparison

The [Linux observer](observer/README.md) binds the approved agent and collector
process lifetimes. It reads the agent inside its pinned network namespace and the
collector through the authenticated Kubernetes metrics resource. It retains the
existing listener and permission boundaries and exports numeric observations only.

`standard_window.py` checks every poll's fields, clocks, cadence and counters. A
scan's latest gauge is counted only when the success counter advances once; a
jump means missing durations, not two copies of the same duration. A changed gauge
without a counter advance, a reset, or a new scan/post/ingestion failure prevents a
normal observation from qualifying. Collector count jumps retain the last duration
and the number of missing durations; no complete collector percentile is inferred.

`scans.py` compares matched complete scan populations against a paired control,
then compares the subset whose entire duration and completion-time uncertainty are
bracketed by unchanged owned attachments. Starts, gaps and transitions remain in
the whole-window comparison. Both p95 regressions must be strictly below 5%; exactly
5% fails. Minimum total/active counts and role/density bindings must be frozen by
the controller before measurement. These arithmetic checks do not replace the
remaining resource, event, provenance, isolation or lifecycle gates.

## Explicit local normal controller

`campaign.py` requires a private owned configuration, independently frozen source
hash and `--acknowledge-local-normal-campaign`. The configuration binds the exact
local kind kubeconfig, Node lifetime, immutable images, service and helper hashes,
and three new owned namespaces. Mounted certificates must cover the full campaign
and restoration margin. It does not accept a shortened profile.

`normal_profile.json` specifies five paired 1,350-second windows, 32 fixed workload
containers, a persistent 1 Hz cached-file series and 36 separate 30-second trace
admissions. The files programme has seven maps (`.bss`, `.rodata`, `control`,
`counts`, `events`, `paths`, `target_ref`), five programmes and five links, as defined
by `prototype/trace/filecache/object_policy.go`; the OOM programme's different map
inventory must not be used for this case. Freeze the inspected candidate ELF and
its index alongside the programme inventory before measurement.

Each window checks actual collector mappings for every fixture UID, container,
cgroup and Node, both before and after sampling. A global count cannot substitute
for these checks. Service/workload lifetimes, source, profile, configuration,
kernel boot and numeric stream hashes bind paired replay. Missing allocation
samples stay explicit. Complete delivery budget failures remain failures; partial
transport, observer loss or changed inputs invalidate the window.

Fixture deletion uses captured resource UIDs. On failure, old source processes
are stopped to invalidate observer bindings, then the trace API is restarted so
Kubernetes can discover its resources while finalising namespaces. Final restoration
returns the original default API profile. No namespace finaliser is bypassed.

The kernel configuration check follows [cilium/ebpf memory accounting guidance](https://github.com/cilium/ebpf/blob/main/docs/ebpf/concepts/rlimit.md) and the
[Linux kernel parameters](https://docs.kernel.org/admin-guide/kernel-parameters.html).
The live watcher also verifies that the worker remains in its parent's cgroup.
This supports the measured cgroup accounting; it does not turn missing node-wide
scheduler percentiles or isolated verifier timing into measured evidence.
