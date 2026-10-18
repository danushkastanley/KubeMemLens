# Active qualification observer

This separate Linux command extends the administrative measurement surface for
active-workload qualification. The historical `../measure` source, schema 1,
900-second limit and two-service role vocabulary remain unchanged.

Schema 2 samples at 1 Hz for 1–1,800 seconds, including the initial sample. It
accepts at most 16 distinct bound cgroups: `node`, `api`, `selected`, `selected-peer`, `agent`,
`collector`, `probe`, and `nonselected-0` through `nonselected-9`. Each binding
supplies a private absolute cgroup-v2 path and expected inode. Path replacement,
malformed counter values or output failure aborts the stream. Duplicate roles,
paths and inodes are rejected. Configuration is a private regular file, bounded
to 16 KiB; unknown fields and trailing input are rejected. Records are bounded to
256 KiB each and 256 MiB total. SIGINT/SIGTERM stops observation, never indicating
a completed window.

The observer records cgroup CPU/throttling, memory/current/events, process RSS,
Node/VM CPU ticks, available memory, pressure and its own CPU/peak RSS. It grants
no permissions, writes no kernel controls and loads no BPF. It does not run a
workload, start a trace or determine a benchmark verdict.

## Scheduling source semantics

Node-wide `/proc/schedstat` versions 15–17 expose cumulative CPU runtime, runqueue
wait and timeslice counters. They are reported only when the runtime setting
`/proc/sys/kernel/sched_schedstats` is enabled. Missing, disabled and unreadable
sources remain distinct and contain no numeric substitute. CPU domain counters
are outside this source's scope.

For each bound cgroup, the observer also reads every currently observed task's
`/proc/PID/task/TID/schedstat`, verifying task/process lifetimes and membership.
These are cumulative counters for the observed task cohort, not a complete
Node-wide distribution and not a latency percentile. Deltas require identical
cohort tokens and monotonic counters. Short-lived tasks between samples are not
observed. A changing/unreadable process set or task cohort produces unavailable
fields; evaluators must retain these gaps and cannot turn them into zero.

Task identifiers, names, paths and start ticks are never output. The cohort token
is keyed with fresh per-group randomness retained only in the observer process;
it is stable within that observation run, not a cross-run identity.

The [Linux scheduler documentation](https://docs.kernel.org/scheduler/sched-stats.html)
defines these counters. Local kind on LinuxKit 7.0.12 exposes per-task counters but
no node-wide schedstat file. Per-task readings do not silently replace the absent
Node-wide source.

## Verification

Run native Linux race tests and vet, and cross-build both supported architectures.
A local read-only run must verify the actual cgroup bindings, observed source
availability, 1 Hz cadence, bounded read duration and absence of private identifiers
in output. Preserve failed/partial runs. Qualification requires additional workload,
BPF lifecycle, transport, scan and collector measurements and a paired evaluator;
this observer alone cannot satisfy the benchmark protocol.

## Flood containment observations

The explicit configuration field `"observation":"containment"` selects schema 3.
It retains every schema-2 usage, process, pressure and scheduling observation and
adds a `containment` object to each cgroup. The default remains schema 2, and its
strict readers reject schema 3 rather than silently ignoring the extra fields.

Every sample reads `cpu.max`, `cpu.max.burst`, `memory.max`, `memory.peak` and
`pids.max` through the same bound cgroup root, then checks its lifetime again.
Numeric zero, an explicit `max` setting and a missing file have distinct output.
Malformed or unreadable values abort; absent optional files remain unavailable.
No kernel setting is written and no peak is reset. The records contain no paths.

The [kernel cgroup-v2 documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html)
defines `memory.peak` as a cgroup-lifetime high-water mark when read without a
reset. It is not a window-local measurement. `cpu.max` describes bandwidth for
applicable scheduling classes; the setting alone does not prove an instantaneous
rate ceiling or the scheduling class of every task. One-second observations also
cannot exclude a setting change between reads.

`hack/ebpf-active-qualification/flood_resources.py` separately replays schema 3
against independently frozen CPU and memory settings. Missing peaks or settings,
changed limits, OOMs, invalid cadence and observed limit violations cannot produce
a pass. Normal workload budgets and validators are unchanged. This observation
slice still requires native Linux tests, a real read-only stream and integration
with a complete five-pair flood campaign; it does not grant qualification.
