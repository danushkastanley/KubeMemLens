# Active qualification accounting

This harness is separate from the frozen historical idle experiment. It currently
runs explicit owned local normal, high-rate mixed and noisy-neighbour cases and validates their measurements. It cannot
issue a full qualification verdict. Running the controller starts bounded fixture
workloads and submits trace requests; importing accounting helpers does neither.

The separate [EKS node-host entrypoint](EKS_HOST.md) binds the same full workload
protocol to an approved AL2023 amd64 host. It requires provider preparation and
live validation; local results cannot establish EKS support.

The separate [storage sizing probe](STORAGE.md) reads owned fixture cgroup and
backing-device counters before and after an approved workload. Its observations
help size the provider host; they do not replace a performance qualification case.

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

Paired evaluators and the sizing replay summarise validated scheduler counters
with observed coverage, missing/disabled source counts, counter resets and cohort
changes. The mean queue-wait ratio uses only observed intervals and preserves its
integer numerator and timeslice denominator. Missing data has no numeric total;
an interval with no timeslices has no derived mean. These summaries do not infer
latency percentiles or supply a scheduler regression verdict. The existing
counter-only percentile disclosure remains unchanged; separate native capture supplies verifier measurements below.

Attachment accounting accepts an explicit expected worker/control count of one
or two. Existing normal, high-rate and noisy profiles retain exactly one. The
two-worker option requires exact complete object inventories and uninterrupted
two-worker/control brackets across each resource interval, workload operation and
scan; a single-worker gap cannot count as simultaneous activity. It additionally
requires the schema-2 `watch-targets` observation with `targetWorkers: [1, 1]`.
Counts correspond to ascending frozen target cgroup IDs; raw IDs stay out of the
stream. The native observer rechecks each worker's target descriptor around the
object census. Missing coverage and two workers on the same target cannot qualify
an interval. Native/live verification of this mode, a concurrent controller,
independent streams and a frozen concurrent profile remain required before a
maximum-concurrency run can qualify. Legacy `watch` keeps its schema-1 output.

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

`mixed_workload.py` accepts the separately versioned fixed cached-read,
uncached-read and write cycle. It compares matching indices for every operation
class as well as the combined population. Each class needs its own predeclared
minimum number of fully attachment-bracketed operations. Both complete and active
subsets must remain below 2% p99 regression in every class; a passing combined
percentile cannot override a failing class. Unproven and transition operations
remain in the results. The comparison alone does not qualify a campaign.

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
loopback listener, and retain every polling interval. Historical latest-gauge
records use attempt counters for deduplication; counter jumps mean missed
durations. Current observations use the bounded atomic scan history described
below. Bracket whole scans against observed attachments with the recorded
completion precision. Collector duration gaps must remain explicit rather than
being presented as a complete latency distribution.

## Live standard-service observation and scan comparison

The [Linux observer](observer/README.md) binds the approved agent and collector
process lifetimes. It reads the agent inside its pinned network namespace and the
collector through the authenticated Kubernetes metrics resource. It retains the
existing listener and permission boundaries and exports numeric observations only.

`standard_window.py` checks every poll's fields, clocks, cadence and counters.
Schema 2 includes the agent's latest 32 scan attempts with contiguous sequences,
individual durations and nanosecond completion times. The validator retains each
new attempt and rejects missing or altered history. Schema 1 keeps its original
latest-gauge rule: the success counter must advance at most once per poll; a
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

Build the chart inventory decoder before freezing the campaign or provisioning
a paid test host. Add `chartInventory` to the private configuration with its
absolute `path` and independently recorded `sha256`. The file must be owned by
the controller user, executable, and not writable by group or others. Symlinks,
changed hashes and missing binaries fail before campaign runtime access. The
decoder is rechecked at each campaign invariant and before and after use.

```sh
go build -trimpath -o /absolute/preparation/kml-chart-inventory ./hack/node-qualification/chart-inventory
shasum -a 256 /absolute/preparation/kml-chart-inventory
```

For the planned EKS host, build for `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` and
verify the installed bytes there. The fixture installer no longer compiles Go
code. The frozen source inventory now includes the decoder and every standard
chart file, including values, schema and templates. Prepare a fresh configuration
and source hash for new runs; historical frozen campaigns remain unchanged.

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

## Explicit local high-rate controller

The same owned controller accepts `--acknowledge-local-high-rate-campaign` instead
of the normal acknowledgement. Exactly one is required. It loads the separate
`high_rate_profile.json`; the original normal profile and acknowledgement remain
unchanged. Freeze the source, profile, configuration and newly built fixture image
before measurement; the older fixture image does not implement `mixed-series`.

The high-rate profile requires five paired 1,350-second windows, 32 workload
containers, and 13,200 operations at 100 ms, cycling cached read, uncached read and
write. At least 3,000 operations of each class must be fully bracketed by unchanged
owned attachments. The 36 separate 30-second admissions keep the same duration,
event, output, map and path ceilings. Measurement, observation, ownership and
restoration checks use the same implementation as the normal campaign.

Replay compares latency per class and combined. CPU, memory, scan and event
results conservatively apply the existing normal thresholds to this higher rate;
`measuredNormalBudgetsPassed` retains that precise meaning. No threshold is relaxed
or inferred from these measurements. `high-rate-result.json` records this case
only. It cannot establish noisy-neighbour, concurrency, flood, lifecycle, provider
or full qualification. Missing scheduler/verifier observations remain explicit.

## Ten non-selected noisy neighbours

`--acknowledge-local-noisy-campaign` selects the separate frozen
`noisy_profile.json`. Five peers share the selected target's namespace and five
use the other owned namespace. The total stays at 32 fixture containers; the ten
peers replace passive fixtures. They are never admission targets. Each peer runs
1,320 fixed `noise` operations at 1 Hz, reading and writing 32 MiB per operation,
while the selected target retains the normal cached-read schedule and trace limits.

All ten process/cgroup identities and resource streams join the paired control and
enabled accounting. Their complete operation streams are hashed in each envelope.
Every peer must have at least 900 operations fully bracketed by the selected trace
attachments, with complete and active-subset p99 regression strictly below 1%.
Exactly 1% fails. The selected workload still uses its below-2% limit. No combined
peer average can hide a single failure; transitions and gaps remain in the report.

The existing bound event receiver rejects events that differ from the admitted
target. This case does not grant a standalone isolation verdict or prove absent
events in unobserved channels. CPU, memory, scan and event checks conservatively
retain normal limits. The full noisy campaign still requires live paired execution;
offline validators, fixture inventories and a `noisy-result.json` filename alone
cannot establish qualification. Concurrency, flood and remaining lifecycle/provider
cases remain separate requirements.

## Maximum concurrent selected traces

`--acknowledge-local-concurrent-campaign` selects `concurrent_profile.json`: five
paired 1,350-second windows, 32 fixture containers, two independent cached-read
workloads, and 36 pairs of 30-second trace sessions. Both workloads perform 1,320
operations at 1 Hz. Each requires at least 900 fully bracketed operations; resource
accounting requires at least 900 seconds of simultaneous two-target attachments.
The installation, memory, scan and per-receiver event limits conservatively retain
the normal thresholds. No budget is multiplied by two.

The guarded API profile transition changes the Node limit from its default one
to the supported maximum two and enables confirmed paths for the fixed fixture.
Its exact original specification is restored using UID/resource-version checks.
The campaign creates a probe ServiceAccount and binding only in the owned fixture
namespace. Both workloads have separate resource roles (`selected` and
`selected-peer`), and all 72 delivery streams are hashed in the enabled envelope.

`concurrent_workload.py` compares exactly two selected operation streams against
the same schema-2 attachment witness. Each target must independently meet the
full-window and simultaneously bracketed below-2% p99 limit, including its own
minimum active operation count. A faster peer cannot offset a failing target.
Single-worker intervals, duplicate-target coverage and missing observations
remain unproven; their operations stay in the full-window results.

Target ordinals are bound to distinct owned fixture identities in the private
window receipt; replay rejects changed identities or duplicate observer ordinals.
It compares both delivery summaries against their raw streams and retains failed
latency/loss/teardown verdicts separately. Offline checks do not establish live
maximum-concurrency qualification.

`ConcurrentAdmissions` retains the two selected sessions and uses a separate
`limit-probe` ServiceAccount in the first namespace for the third request. Its
namespaced grant reuses the existing target-only fixture Role. Both selected
admissions must report the same active identities before and after the exact
capacity error response. An unexpectedly accepted probe is retained for cleanup,
including when its candidate digest is wrong. A lost response or missing identity
requires the enclosing controller's guarded source teardown; it cannot be counted
as denial. The capacity response alone cannot identify the exhausted quota: the
live controller must also freeze the Node limit at two, prove that actor/namespace
and global limits are not exhausted, and retain simultaneous target attachments.
The helper does not create grants or change the installation policy on its own.

Receiver configuration selects the matching admission client and pinned workload
by target index. A namespace, session or candidate mismatch fails before starting
the receiver. The private window receipt records each target's identity and its
ordinal in the native observer's ascending cgroup-inode order. Duplicate Pod,
container or cgroup identities cannot represent two targets. These bindings
preserve the existing single-target path. Live two-receiver verification remains
required before recording a concurrency verdict.

`ConcurrentTracePair` implements one bounded two-receiver session. It starts both
streams before waiting for either, checks the combined owned attachment inventory,
probes the third principal, and retains each target's delivery result and expiry.
Both streams share one absolute completion deadline. Failed budgets remain failed
results; incomplete delivery or residual owned state invalidates the pair. Errors
escape to the enclosing Window's admission cancellation and guarded source
teardown. Continuous schema-2 target coverage is still needed to establish the
simultaneously active intervals; an aggregate startup census cannot prove them.

The frozen combined inventory expects 14 maps, 10 programmes and 10 links for the
candidate's two workers. Those counts alone are insufficient: the native schema-2
witness must observe exactly one worker per target, unchanged object identities
and complete controls across each counted interval. Build and verify the updated
native observer and its separate resource role before running this case. A full
paired campaign, native/live verification and remaining provider/lifecycle gates
are still required; this controller's presence does not establish qualification.

## Ceiling-result replay preparation

`ceiling_result.py` validates the separate native ceiling-observation document
against its requested event or output bound and process exit status. It checks
frame/byte accounting, the terminal reserve, same-kernel evidence and nullable
engine counters. A complete expiry or a different observed ceiling remains
inconclusive for the requested ceiling. Missing transport, contradictory counts,
extra fields and inconsistent exit/verdict combinations cannot produce a pass.
Unknown counters are retained without calculating loss or latency percentiles.
`evaluate_ceiling_session` also requires exactly one successful readiness record
before the final observation. The live controller must consume that readiness
before launching the flood; replayed record order alone cannot establish when
the workload actually started.

This replay establishes only a reported admitted ceiling. It does not prove ring
saturation, resource containment, isolation, cleanup or the five-pair flood case.
Native parser/transport/CLI execution and the flood controller remain required.


## Paired bounded flood controller

`campaign.py --acknowledge-local-flood-campaign` selects a separate frozen
`flood_profile.json`: five pairs, 900-second control and enabled windows,
60-second warmups, 32 owned fixture containers and 20 slots at fixed offsets.
Each window repeats the event-limit, output-limit, ring-loss and paused-reader
cases five times. Workload preparation has a two-second lead; a slot more than
250 ms late fails without retiming or retrying it. Control windows run the same
independent fixed-volume inputs without an optional tracer.

The controller selects schema-3 resource observations and retains every workload,
delivery, activity, standard-service and private window receipt in its hashed
envelope. Each enabled session records the complete attachment census before
releasing I/O and the captured-object census after cancellation. Replay recomputes
the writer/kernel/pause verdict, checks workload timing inside the resource window,
and rejects altered counts, omitted attachments or remaining captured objects.
A complete failed mechanism remains a failed paired result; transport, cadence,
identity or cleanup failures abort and retain the partial window.

Flood replay checks the actual frozen service CPU/memory settings, available
memory lifetime peaks and sampled BPF map allocations. It compares each matched
burst's application-read throughput using I/O time, excluding time waiting for
controller commands. A throughput regression of exactly 2% fails; favourable
bursts cannot hide a failed one. Standard scan comparisons and collector gaps are
retained separately. The normal-case CPU, loss and latency gates are unchanged.

This controller is prepared and covered by host tests, but has not run a full
paired campaign. The inputs are finite bursts; they do not establish sustained
producer saturation throughout the window. Continuous-pressure coverage still
needs its own bounded workload before full flood qualification can be claimed.
Native schema-3 observer tests, real controller integration, event-latency and
verifier measurements, isolation, lifecycle and provider checks remain required.
Previously completed single-session checks used their own frozen source and do
not establish this new campaign or its additional replay fields.

`pressure_metrics.py` prepares the continuous producer's separate replay seam.
It validates schema-3 resources and the complete pressure stream, requires the
producer to cover both resource windows, then selects the same contiguous whole
producer intervals in both phases. Boundary intervals are excluded by clock
bounds alone, with a five-millisecond margin; partial bytes are never estimated.
The comparison retains every selected interval and applies the strict 2% limit
to their aggregate measured read throughput. It reports matched-interior
throughput only, not an active-trace-only or full-qualification verdict.

The producer now records a wall/monotonic alignment at each interval boundary.
Replay rejects more than five milliseconds of alignment uncertainty or clock
offset drift from its start, in addition to the original contiguous-duration and
counter checks. This avoids treating an initial timestamp as sufficient evidence
for the entire pressure run. These paths are host-tested only; native execution
and live campaign verification remain pending.

## Sustained-pressure window preparation

`PressureWindow` now owns a 910-second continuous producer around a 900-second
resource window, with a one-second lead. Both control and enabled phases use the
same producer. Required observers and producer lifetime are checked throughout
sampling; failure uses the existing owned tracer/fixture teardown before closing
local workload handles. Successful collection retains the validated pressure
stream and completes its Q handshake.

The prepared `pressure_profile.json` has five pairs and 20 fixed 30-second trace
slots, alternating ring-pressure and paused-pressure observations. These sessions
retain the authenticated active-admission check and complete attachment census.
A fast writer that finishes before those observations is not silently accepted.
Known produced/sampled/lost/delivered counters must establish actual ring loss and
the exact kernel candidate cap. Both post-cancel and expiry-relative physical
cleanup observations must be below two seconds; equality fails.

`campaign.py --acknowledge-local-pressure-campaign` now selects this sustained
profile. Its paired replay binds each session to that session's exact attachment
IDs and independently measured read-count bounds. At least 20 complete producer
intervals must be bracketed by those attachments; a pause must contain independent
read volume beyond the admitted candidate cap. It joins these checks to resource,
map, standard-service and paired-throughput observations. Native observer/producer
execution and real controller integration remain unverified. The finite writer-limit and output-limit checks remain separate;
continuous producer traffic would invalidate their exact fixed-burst accounting.


Pressure provenance inventories the continuous producer, resource and standard
streams in both phases, plus every delivery and activity witness in the enabled
phase. A synthetic pair containing complete 900-second record sequences exercises full
replay; it is not a timed runtime run. Mutating its memory peak while
preserving successful mechanisms yields a failed pressure budget; altering a
stream without updating its envelope is rejected. These host tests establish
replay behaviour only. They do not replace native or real five-pair execution.

Sustained-session saturation requires known counters, an exhausted kernel
candidate budget, observed ring loss, expiry and exact accounting of delivered,
lost and rejected candidates. Reported rejections remain visible; they do not
negate observed ring loss or establish complete delivery. Unknown counters,
unaccounted candidates and rejection without ring loss cannot establish
saturation. This follows the benchmark protocol's allowance for disclosed drops
in flood cases; it changes no normal loss, resource, duration, isolation or
cleanup threshold. It grants no paired performance qualification by itself.

Active paired campaigns create controller-owned fixture Jobs with one child, no
retries and the existing fixed deadline and Pod security/resource settings. They
bind each logical roster entry to its real generated Pod name and UID before
granting tenant Pod-read permission or admitting traces. Parent UID/spec changes,
replaced/restarted children, multiple children and expanded admission-time bounds
fail the campaign. Requests, native receiver identities, selected/noisy workload
commands and collector density checks use those same bound Pod identities. The
new controller path requires a live local check after any timed campaign ends;
unit fixtures alone do not establish provider policy enforcement.

## Failed observer diagnostics

An incomplete standard observation reports a fixed failure category for process
bindings, agent or collector requests, parsing, clocks, or output. Underlying
errors, URLs, tokens and response bodies are omitted. The controller records
first-detected failing process labels and exit codes in
`failed-processes.private.json` before teardown; an observer that exits early
with code zero is also invalid. This receipt identifies detected exits, not an
assumed root cause or exit ordering between polls. Keep all partial streams.
No diagnostic changes permit retries or relax observation and workload limits.

## Scheduler measurement in every window

All active cases require a fifth immutable `scheduler` helper. The controller
freezes the scheduler packages, records the exact private native input and stream
in the envelope, and pins the validated standard collector as its process owner.
The helper runs in both control and enabled windows. Early exit, owner death,
loss, inactive perf time or incomplete cleanup invalidates the window.

Replay checks the input boot/duration, collector identity and pid/start binding,
CPU roster and tracepoint hashes, then validates the full schema-2 histogram
stream. Paired CPU/format bindings must match. Scheduler cutoffs must be within
one sampling period (one second) of the standard-observer window; the actual
start skew is reported. This guards against a different window being substituted,
without claiming simultaneous process startup. Results retain percentile ranges,
coverage counters and the observer's own cost. They do not change existing tracer
or workload budgets. Verifier measurements use the separate bounded capture below.
Historical evidence must use its frozen older verifier; missing new streams
cannot be treated as measured zeros or silently supplied during replay.


### Verifier capture in every active case

All six active profiles require a sixth immutable `verifier` helper built from
`prototype/trace/qualification/verifier-observer`. Its package and verifier library
are included in the source inventory. Each window freezes private input and an
exact probe ownership journal before launch; both files and `verifier.jsonl` are
mandatory in its evidence envelope. Historical runs retain their original frozen
replay code and cannot acquire these measurements retrospectively.

Enabled windows bind to the trace Node service process and cgroup, including its
worker descendants. Control windows bind to the existing capability-free standard
collector and explicitly prove that the tracer is absent. This is a distinct
negative control; the evaluator does not subtract measurements from different
process scopes. It requires zero completed control calls and positive enabled
calls. Missing or early-exited capture is an error, not a zero result.

Replay checks the process identity, cgroup inode, boot, online CPUs, kernel BTF,
format hashes, fixed timing bounds, counters and completed cleanup. It reports
completed/rejected calls, verifier elapsed time and finalizer byte counts without
log contents. Probe overhead and observer CPU/RSS remain explicit. Every capture
has to account for its final records; profiles retain their existing idle tail.
After observer exit, the controller reconciles only unchanged definitions from
that window's journal. A live helper or changed definition prevents cleanup and
invalidates qualification. Full paired execution and provider evidence are still
required; successful component tests do not establish those outcomes.
