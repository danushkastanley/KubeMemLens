# ADR 0013: Reserve bounded time for normal worker exit

Status: accepted for bounded local candidate tests on 15 September 2026.
Normal-expiry workload cases passed; broader lifecycle and benchmark gates remain.

## Context

Real Linux arm64 file traces loaded and attached the accepted programme set,
observed selected I/O and removed all captured owned objects. Some normal runs
nevertheless returned unknown terminal counters: the supervisor killed the
worker after 500 ms while its last link close was still in the kernel.
In one captured run, serial close calls took about 116 ms, 223 ms and 221 ms;
the final close resumed after the kill. A separate node-context expiry race was
also corrected so normal lease expiry retains `context.DeadlineExceeded`.

The R6 lifecycle acceptance criterion allows normal teardown within two seconds.
The former 500 ms normal-exit allowance was an implementation choice that did not
cover the measured detach path. Cancellation and observation deadlines serve
different purposes from the time needed to close already-owned resources.

Concurrent closure is not a reliable latency fix: Linux v7.0
[unregister_ftrace_direct](https://github.com/torvalds/linux/blob/v7.0/kernel/trace/ftrace.c#L5785)
holds the shared direct-call mutex while unregistering a function. The local
kernel configuration enables dynamic ftrace direct calls. This source evidence
does not establish a universal latency bound for other kernels.

## Decision

Use a shared, fixed one-second `workeripc.NormalExitGrace` for natural expiry,
normal process exit and the latest permitted post-detachment cgroup evidence.
Keep the 500 ms SIGTERM-to-SIGKILL allowance for cancellation and invalid output.
Bound waiting for process exit plus reaping to two seconds in total, rather than
adding a separate two-second reap allowance after the grace period.

The kernel observation deadline, admitted duration, event callbacks, permission
checks, map/ring/output/path limits, capabilities and seccomp policy do not change.
Only normal expiry may obtain a final cgroup sample. SIGTERM still cancels that
sampling lifetime; expired grace never starts a sample. Every result is checked
against its observation window and accepted only after successful process exit.
Malformed results, late events, non-zero exit and unconfirmed reaping remain
failures. The existing separately bounded pipe-reader join remains mandatory.

This exit-budget decision leaves the SDK patch and BPF objects unchanged; the
accepted combined candidate also includes the separately documented path-copy
correction. The changed worker hash requires a
new signed engine release, matched node/worker installation and maintainer review
before loading. Signature verification alone is not acceptance.

## Alternatives

- Retain 500 ms for normal exit: measured ordinary detach can be killed before
  counters and correlation are delivered, despite eventual kernel cleanup.
- Close links concurrently: shared kernel locking prevents assuming that this
  removes serial wait time; it would also introduce more cleanup concurrency.
- Sample before detachment or return before resource release: this would weaken
  the established evidence and ownership guarantees.
- Extend the observation or overall teardown ceiling: unnecessary; the selected
  change fits the existing two-second normal-teardown criterion.

## Validation and consequences

Regression fixtures model a 650 ms normal close delay while retaining the original
observation end. Protocol tests accept bounded post-detachment evidence and reject
evidence beyond one second. Cancellation, earlier parent deadlines, late events,
malformed output, signal-cancelled sampling and uncertain cleanup remain tested.
These capability-free tests do not prove real kernel latency. The accepted
replacement also passed local normal-expiry workload and owned-object cleanup
cases, retaining counters and overlapping cgroup evidence. See
[local qualification](../ebpf/FILE_CACHE_LOCAL_QUALIFICATION.md). The full failure
matrix and later benchmark qualification remain required.
Kernels or load conditions that still exceed the bound remain unqualified.

## Migration and rollback

Install the matched optional node image, worker executable and independently
accepted signed policy together. Public v2 frame structure and persisted data do
not change. Standard agent/chart artefacts are unaffected. Roll back the complete
optional installation to the prior accepted image and policy, or remove its
acceptance policy to disable new admission. Confirm owned cleanup before removing
the old optional installation.
