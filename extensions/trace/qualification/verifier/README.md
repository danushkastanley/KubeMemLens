# Numeric verifier observation

This package contains a numeric matcher, fixed probe definitions, ownership and
counter checks, BTF argument validation, and a bounded Linux tracefs/perf reader.
It is not integrated into the active campaign. Verifier measurements there remain
unavailable until the bounded observer CLI, lifetime checks and campaign integration are complete.

The intended measurement is elapsed monotonic time inside `bpf_check`, with the
kernel-finalised required log size. It is not syscall/admission latency, CPU time,
or proof of log delivery. The finalised size can include a terminator and exceed
the supplied buffer. A non-zero finalizer error remains a separate observation.
No log contents, programme instructions, names or kernel addresses are evidence.
Format tests include synthetic layouts and three real LinuxKit 7.0.12 arm64
schemas from a disabled register/read/remove cycle. This validates the layouts,
not the saved argument-fetch behaviour.

## Source basis

Linux 6.12 calls `bpf_vlog_finalize` from its verifier. That function writes a
32-bit size through its second argument before returning, including a measured
zero when logging is disabled. Its result can report a truncated buffer or a
copy fault. Read the returned size; do not derive it from the SDK logging policy.
See [the verifier](https://github.com/torvalds/linux/blob/v6.12/kernel/bpf/verifier.c)
and [log finalisation](https://github.com/torvalds/linux/blob/v6.12/kernel/bpf/log.c).

The [Linux 6.12 kprobe documentation](https://github.com/torvalds/linux/blob/v6.12/Documentation/trace/kprobetrace.rst)
describes return-probe access to saved function arguments, numeric dereferences
and missed-return counters. These are leads for a bounded native reader; they
are not evidence that the target kernel permits attachment or has matching ABI.

## Matching contract

The caller supplies already validated events from the owned tracer cgroup and
descendants, merged across every online CPU by monotonic timestamp. Each thread
must have one verifier entry, one finalizer return, and one verifier return in
strict order. Interleaved threads are matched independently. Finalizers outside
a verifier call are counted separately because BTF also uses them. They cannot
fill in missing observations. Calls with missing finalization remain incomplete,
including early verifier error paths which legitimately bypass it.

The matcher bounds active calls to 64, completed calls to 4,096, input events to
32,768 and each call to 30 seconds. Exceeding a bound invalidates capture rather
than sampling. These are observer integrity limits, not product budget changes.
Completed summaries expose numeric counts, total/maximum duration and log size,
rejections, finalizer failures and pending calls. TIDs remain transient and refuse
JSON or formatted output. Any integrity failure permanently invalidates results.
The reader must advance the merged-stream watermark each second even without
events; this rejects late records and bounds calls with a missing return.

Probe definitions accept only a 32-character lowercase hexadecimal ownership
token. Both group and event basenames are unique to it because kernel probe
profiling omits the group. The registry parser rejects altered targets, offsets,
fetch expressions, duplicate events and changed return-instance limits. The
profile parser requires all three unique hit/miss counters. Definitions are
limited to 128 return instances per return probe. This is a bound, not proof
that return probes never miss an invocation.

The BTF check requires the exact finalizer function, signed 32-bit return type,
verifier-log pointer and unsigned 32-bit size-output pointer, resolving typedefs.
It uses the existing BTF dependency and neither loads a programme nor attaches a
probe. Passing this type check alone does not validate a return-argument fetch.

## Native reader and remaining validation

The Linux adapter pins tracefs, appends only the three fixed registration or
removal commands, and never opens the registry with truncation or writes enable
flags. An existing owner group is refused. Partial writes are reconciled before
cleanup; a changed definition prevents deletion. Cleanup errors remain persistent.

Capture duplicates an inode-bound cgroup-v2 directory and refuses the filesystem
root. Three close-on-exec perf descriptors per CPU filter that cgroup and its
descendants. Each CPU has a 64 KiB ring; records are capped at 512 bytes and only
numeric projections enter the 4,096-frame ordering queue. Stop disables capture,
drains the final records and requires the kernel event count to match all decoded
and explicitly discarded startup records. Loss, throttling, backwards counters,
counter overflow and missing coverage invalidate the observation. Cancellation
must still close descriptors before removing definitions.

Race tests, native ring/ownership tests, disabled registration and idle capture
have passed locally. A six-load calibration then captured exactly the three
selected calls, matched their syscall log-size oracle (0, 98 and 188 bytes),
recorded the rejected call, and excluded all three loads from the other cgroup.
It exercised CPUs 0 and 13 with zero loss/misses and verified removal of programmes,
probes, processes and cgroups. These component checks are not paired qualification.

The enclosing controller must verify the exact kernel function signatures and availability,
boot, architecture, CPU topology and an inode-bound owned cgroup/process lifetime.
The proposed probe set is fixed: verifier entry, verifier return, and log finalizer
return with only its signed result and dereferenced 32-bit size. No arbitrary
functions, expressions, strings or user pointers may be accepted in configuration.

Use uniquely owned tracefs event definitions without touching global enable flags
or existing definitions. Open only cgroup-filtered, close-on-exec perf descriptors.
Bound rings, records, queueing, reordering, output and elapsed time. Raw tracepoint
headers contain transient addresses and identities: never dump them; disable core
dumps and project only allowed numeric fields. Close all descriptors before
removing exactly the owned definitions, with a cleanup receipt.

Final coverage requires zero lost/throttled records, zero missed return probes,
no ambiguous ordering and no pending calls. Perf accounting uses scheduled cgroup
context time, which can be zero on an idle CPU. The reader requires equal enabled
and running totals for that context; positive-capture validation remains required.
Match observed loads to controlled admissions and reject
unexplained missing verifier calls. Snapshot final loss/miss counters after disable.

The fixed calibration runner validates saved return arguments with known-zero,
known-nonzero and rejected cases; a zero-only idle capture cannot establish those
semantics. It uses only a fixed small log buffer. Confirm owner-exit, cancellation, loss/overflow,
missed-return and cleanup failures cannot produce a complete receipt. Measure
observer cost and use the same instrumentation in each paired window. Those
remaining native checks have not yet been completed.

Perf sample TIDs belong to the descriptor creator's PID namespace; tracepoint
`common_pid` uses the kernel identity. Both are range-checked, but they are not
required to be equal. Matching uses the perf TID consistently; ownership comes
from the pinned cgroup and descriptor, not a cross-namespace numeric comparison.
