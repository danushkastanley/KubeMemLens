# Bounded scheduler observer

This qualification-only Linux amd64/arm64 command reads the four existing
scheduling tracepoints through perf. It loads no BPF, creates no dynamic probe
and changes no global kernel setting. The parent controller must explicitly
select the owned host and freeze its boot, CPU roster, tracepoint format hashes,
source and native executable.

Supply a regular private file (no symlink, mode 0600, maximum 16 KiB) with exactly
`seconds`, `bootID`, `onlineCPUs`, `tracepointSHA256` and `anchor`. The anchor
contains exactly `pid` and process-start ticks (`start`) from the controller
validated standard-service process. A pidfd pins that lifetime; capture stops
when the owner exits. This lets fixture teardown stop the observer promptly. The hash map must contain
exactly sched_wakeup, sched_wakeup_new, sched_switch and sched_process_exit.
Hashes cover the complete corresponding tracefs format bytes. Duration is
1–1,800 seconds and the CPU roster is bounded to 64 distinct online CPUs.

```sh
scheduler-observer --config /private/frozen.json --acknowledge-owned-node
```

The command opens at most 256 perf descriptors and maps 256 KiB plus one metadata
page per CPU. It limits concurrent Go execution to two CPUs and uses a 65,536-event reorder queue, a
32,768-task pending map and a 100-million-event ceiling. It disables core dumps
for its own process so raw kernel records cannot be saved in a core file.

Schema-2 JSON rows contain perf enabled/running/lost counters as well as
cumulative histogram/coverage counts and own
CPU/RSS, with exact monotonic cutoffs and read intervals. A fixed 100 ms reorder
allowance handles cross-CPU arrival order. Samples more than 250 ms late or reads
longer than 50 ms fail. Each row is at most 8 KiB, total output at most 16 MiB.
The hard process lifetime is the requested duration plus ten seconds. Errors or
interruption close owned descriptors/mappings and invalidate incomplete streams.
The parent must retain failures and independently verify process/resource cleanup.

Only aggregate numeric observations are emitted. Task identifiers are temporary
matching inputs; raw perf task names remain in volatile rings and are not copied
into strings or written to evidence. Initial unmatched switch-ins, re-enqueues
and unfinished waits are explicit. Percentiles derived from logarithmic buckets
are ranges, not exact values. Shared kind kernels cover the shared Linux VM and
must not be described as isolated provider-node evidence.

The initial schema-1 observer passed native tests and short/long continuity runs.
Schema 2 adds actual PERF_FORMAT_LOST and enabled/running counter checks, including
final disable, and requires Linux 6.0 or newer. It passed native tests, short and
fifteen-minute capture/replay, and SIGTERM cleanup. Schema-1 rows cannot prove
these additional integrity checks. The subsequent pidfd owner guard passed native
unit, ten-second capture/replay and controlled owner-exit tests. Campaign wiring,
paired workload overhead, stressed operation and provider verification remain.
This helper does not measure verifier duration or verifier log size.
