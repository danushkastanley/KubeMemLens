# Numeric scheduler observation core

This package reconstructs completed runqueue waits from the existing Linux
`sched_wakeup`, `sched_wakeup_new`, `sched_switch` and `sched_process_exit`
tracepoints. The Linux Capture API opens only those existing perf tracepoints. It loads no
BPF programmes. The bounded command lives in ../scheduler-observer. Short native
bring-up is not complete benchmark or provider qualification.

The decoder checks bounded tracefs layouts and projects only numeric task IDs,
timestamps and switch state. Raw task names are not read into strings. Transient
events reject JSON encoding and redact formatting. Evidence contains cumulative
histograms, coverage counters and pending counts, without task identifiers.
The supported amd64/arm64 little-endian switch-state ABI recognises running,
preempted and the existing single-bit sleeping states; new encodings fail. Freeze
the exact kernel and format hashes before capture. The retained LinuxKit schema
fixtures are parser tests, not provider qualification.

A wake starts a wait; a runnable switch-out starts another. Switch-in completes
the latest observed enqueue. Re-enqueues are counted, exit removes a pending
lifetime, and a new task cannot inherit an old pending timestamp. Initial
unmatched switch-ins and unfinished waits remain explicit coverage information.
No wait is inferred for a sleeping or unseen task. Logarithmic nanosecond buckets
produce percentile intervals, never fabricated exact percentile values.

The merger fixes a CPU roster (at most 64), bounded reorder queue (at most
131,072 events), and a 32,768-task pending map. Each CPU has a contiguous sequence.
Cross-CPU arrival order is corrected by monotonic timestamps. An equal timestamp
for the same task on different CPUs is ambiguous and fails. Missing, late,
backwards, malformed, over-budget or lost events permanently invalidate the
stream. The 100-million-event ceiling bounds each observation. No retry hides a
failed window.

## Native capture and remaining integration

The reader opens only the four named tracepoints with CLOEXEC perf descriptors,
one shared 256 KiB ring per frozen CPU, CLOCK_MONOTONIC sample time and numeric
event identifiers. Every CPU is drained before the command advances its fixed
100 ms reorder watermark. Loss, throttling and truncated records fail capture.
Counter coverage/loss errors include the zero-based owned descriptor ordinal,
following the CPU/format opening order. They expose no raw descriptor or task
identity, preserve the original error category and return no usable counts.
The command checks boot/topology bindings and emits one-second aggregates with
its own CPU/RSS. Runtime/output are bounded and descriptors/mappings are closed
on return. The parent controller must verify process/resource cleanup. Raw perf buffers contain kernel task names and
must remain transient: never write them, event records or debug dumps to evidence.
No tracefs enable flags, sysctls, kernel settings or unrelated sessions may change.
Run the observer identically in paired controls and enabled windows; account for
its overhead before relying on the distribution. Ten-second local native capture
and cleanup passed, but longer/stressed operation, campaign integration, evidence
replay and provider verification remain outstanding. No qualification gate is
changed by this helper.

Sources: [Linux scheduling tracepoints](https://github.com/torvalds/linux/blob/master/include/trace/events/sched.h)
and [perf_event_open](https://man7.org/linux/man-pages/man2/perf_event_open.2.html).

`Capture.Stop` disables sampling and validates final loss without releasing the
perf descriptors. This permits controller-coordinated teardown after peer
observations finish. `Close` releases all resources and preserves any stop error.
