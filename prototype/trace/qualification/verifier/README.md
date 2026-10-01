# Numeric verifier observation

This package currently contains an offline matcher, fixed probe definitions,
owned registry/counter parsers, a BTF argument check and numeric record projection. It does not install
probes, open perf descriptors or establish runtime measurement coverage. The
active campaign must continue to report verifier measurements as unavailable.

The intended measurement is elapsed monotonic time inside `bpf_check`, with the
kernel-finalised required log size. It is not syscall/admission latency, CPU time,
or proof of log delivery. The finalized size can include a terminator and exceed
the supplied buffer. A non-zero finalizer error remains a separate observation.
No log contents, programme instructions, names or kernel addresses are evidence.
The format and record tests use synthetic layouts; they do not validate the
target kernel's actual tracepoint schemas or argument-fetch behaviour.

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

## Native work required before use

The native reader must verify the exact kernel function signatures and availability,
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
no ambiguous ordering and no pending calls. Perf enabled/running accounting must
respect cgroup scheduling; do not reuse the scheduler observer's unconditional
node-wide equality rule. Match observed loads to controlled admissions and reject
unexplained missing verifier calls. Snapshot final loss/miss counters after disable.

Before campaign integration, run bounded known-zero and known-nonzero log cases
and a rejection case with a fixed small log buffer. These validate saved return
arguments, dereferencing and size semantics; a successful zero-only capture cannot
exclude a broken dereference. Confirm owner-exit, cancellation, loss/overflow,
missed-return and cleanup failures cannot produce a complete receipt. Measure
observer cost and use the same instrumentation in each paired window. No such
native validation has yet been completed.
