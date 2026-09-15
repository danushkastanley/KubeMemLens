# File and page-cache programme candidate

Status: BPF-005 local qualification in progress. The accepted path-copy and
normal-exit candidate has loaded on local arm64 Linux and passed selected I/O,
cache addition/removal, path-consent, noisy-Pod isolation and ring-loss cases.
See [local qualification](FILE_CACHE_LOCAL_QUALIFICATION.md) for exact scope and
remaining gates. No complete platform or performance qualification is claimed.
The [engine contract](ENGINE_CONTRACT.md) requires programme-specific acceptance
after the complete source, objects, signatures and worker inventory are reviewed.

## Observations and disclosure

The candidate consists of two fixed objects. The files object observes successful
regular-file `vfs_read` and `vfs_write` returns, keeping requested and completed
bytes separate. Negative returns and missing confirmed-path context are rejected
observations and counted explicitly. Zero completed bytes is a measured result.
Open activity, vector I/O, mmap and io_uring paths that bypass these functions are
not covered. These observations must not be presented as all filesystem I/O or
cache hits/misses.

The cache object observes the kernel's actual
`mm_filemap_add_to_page_cache` and `mm_filemap_delete_from_page_cache` tracepoints.
It converts folio order into base-page count. Its events describe work in the
selected task's cgroup, not ownership of shared pages or complete memcg accounting.
Kernel-worker activity outside that cgroup is excluded. PFNs, inode/device values,
page indexes, PIDs and other task identities never enter its ring records. The
[kernel tracepoint definition](https://github.com/torvalds/linux/blob/v6.17/include/trace/events/filemap.h)
is the initial source reference; qualification must freeze the tested kernel.

Every hook requires a nonzero exact cgroup ID, monotonic deadline and event limit.
The all-zero object cannot emit. A default-off control map gates collection, and
a cgroup array holds the exact target's kernel reference. Ancestor membership is
checked in addition to, never instead of, exact cgroup-ID equality. Retaining that
map through attached programmes addresses ID reuse during forced process exit;
the [kernel cgroup-array implementation](https://github.com/torvalds/linux/blob/v6.17/kernel/bpf/arraymap.c)
acquires and releases the cgroup reference. Live lifetime verification remains
required. These constants must come only from the immutable admitted specification.
Filtering precedes record reservation and any path read.
Hook time is captured before event construction and is bounded by the deadline.

Default file tracing never reads or stores paths. Confirmed mode begins a bounded
per-thread context at VFS entry, captures a path only after successful
`security_file_permission`, and matches the same file pointer and operation at
VFS exit. Kernel pointers stay in the private map. The path helper uses the current
task's root; the source does not walk into the host's global mount ancestry.
The [kernel helper allowlist](https://github.com/torvalds/linux/blob/v6.17/kernel/trace/bpf_trace.c)
includes this permission hook, but not arbitrary VFS exit hooks.

Missing, oversized or failed path resolution rejects the observation instead of
emitting a truncated path as complete. Each event starts fully zeroed, and a
bounded loop copies only the validated path prefix. The kernel's
[path helper](https://github.com/torvalds/linux/blob/v7.0/kernel/trace/bpf_trace.c#L843)
can leave scratch bytes beyond that prefix after moving its backwards-built
string. Copying its whole buffer would violate the event's padding contract.
The decoder independently rejects hidden bytes,
nonzero padding, invalid UTF-8, embedded NULs and policy mismatches. Existing stream
encoding provides visible terminal escapes. Nested operations can lose matching
context; they lower completeness through rejection counts.

The native `qualification/filecache/test_path_copy.py` regression runs the exact
producer copy helper against this dirty-suffix shape, checks prefix lengths up to
512 bytes and protects surrounding guard bytes. The prior full-buffer copy fails;
the prefix copy preserves zero event padding. This test loads no BPF programme.

## Compiled inventory

| Object | Hooks | Event bytes | Maps |
| --- | --- | ---: | --- |
| files | VFS read/write entry and exit; permission exit | 552 | 256 KiB ring; 256-entry hash with 8-byte keys and 536-byte values; 32-byte counters; 568-byte rodata; 40-byte metadata globals |
| cache | filemap add/delete tracepoints | 24 | 256 KiB ring; 32-byte counters; 24-byte rodata; 32-byte metadata globals |

Both objects additionally have a one-entry 32-bit activation array and a one-entry
cgroup-reference array. The latter uses explicit key/value sizes rather than
unsupported BTF key/value metadata for this map type.

The objects use no per-CPU map, general exporter, pin, task enumeration, stack
capture, runtime discovery or writable host path. Sizes above are ELF layouts,
not total kernel allocation. The worker must account for kernel map overhead,
ring storage and userspace mapping, reject a lower admitted budget that cannot
fit, and check owned allocations before attachment. No memory-ceiling qualification
is claimed from these static values.

Counters are cumulative and use atomic updates. `produced` counts target-filtered
candidates reaching the programme. Candidates above the kernel event ceiling are
sampled; ring reservation failures are lost; invalid operation/path context is
rejected. The categories are disjoint. After detachment, the worker must reconcile
remaining ring records and final counters; resetting counters or substituting zero
for failed reads is prohibited. Known hook blind spots and unmeasured kernel
misses prevent a claim of complete filesystem/cache coverage.

## Reproduction and remaining acceptance work

`prototype/trace/programmes/build_filecache.py` compiles each architecture twice
using the accepted builder platform, clean pinned upstream headers, BPF little
endian ISA v3 and fixed Clang/strip flags. Its containers have no network or
capabilities, read-only source/root filesystems and bounded CPU, memory and PIDs.
The command requires a new output directory and records source/header/object
hashes. It performs no kernel load, signing, approval or publication.

`prototype/trace/cmd/inspect-filecache` parses ELF/BTF in userspace and reports
maps, hooks, helpers, constants and event layout. It likewise grants no execution
authority. The candidate sources use dual BSD-2-Clause/GPL-2.0-only terms in their
directory licence. Upstream metadata macros, generated vmlinux types and libbpf
headers require their retained provenance and licence records in the final bundle.

The decoder, signature/acceptance boundary and temporal correlation helper have
offline tests. Correlation retains the full cgroup sampling window and a separate
guaranteed overlap, distinguishing absent counters, measured zero and reset.
It refuses other target lifetimes and uncertain/disjoint observation windows.
The candidate worker now samples before activation and after normal-expiry
detachment, forwarding validated correlation through the private result and v2
summary. Sampling remains tied to its retained cgroup descriptor and signal
cancellation. Overlapping correlation has been exercised in the bounded local workload cases;
other kernels and provider environments remain unqualified.
The [candidate bundle](CANDIDATE_BUNDLE.md) now provides signed offline artefacts
and an exact OCI validation/loading boundary. Signing is separate from independent
installation acceptance and does not enable incident execution.

## Constrained SDK policy proposal

The unmodified v0.56.0 SDK loads collections and attaches inside `Start`. Its
generic ring reader pads short records and truncates long ones; its default
verifier log can grow to approximately 1 GiB through cilium/ebpf. These behaviours
cannot silently become the KubeMemLens boundary. The candidate omits generic
tracer metadata so the worker owns strict raw-ring reads and cumulative counters.

`prototype/trace/worker/sdk-policy.patch` is a separate modification to the
checksum-pinned SDK, accepted only as part of the reviewed local candidate. It disables verifier-log allocation, requires a typed
validation callback before attachment, checks cancellation between attachments,
attaches in stable order, and returns link/perf-descriptor cleanup failures.
The signed programme manifest includes the patch digest; changing the patch
invalidates previously accepted manifests. Compilation checks its policy version.

`prepare_sdk.py` materialises that source in an ignored directory without editing
the Go module cache. The standard image context excludes it. The worker imports
only the selected image operator, supplies an empty fixed configuration and a
read-only in-memory object store, and avoids generic runtime initialisation,
registry access, data operators and exporters. Cleanup attempts disable, reader
close, detach and map close once each, preserving failures. The observation window
uses monotonic activation/detachment measurements and ends at the kernel deadline;
startup and late teardown are excluded. Clock drift makes the window unknown.

The [private worker protocol and supervisor](WORKER_PROTOCOL.md) have focused tests,
including real unprivileged Linux child processes. Connecting the executable,
installation acceptance policy and retained node handle is implemented, with
test-only process fixtures. Version 2 [stream output](STREAM.md) now accumulates
default path-free totals and permits file events only with confirmed path consent.
Before/after cgroup reads and correlation output are wired. Kernel
loading, owned allocation qualification and the final worker image remain open.

Fresh acceptance must cover this SDK patch as well as programme source, objects,
signatures and the final worker dependency/licence inventory. First-load acceptance
must precede real Linux verification of
filtering, raw-path containment, workloads, loss and owned teardown. Independent
review and the later R6 benchmark/watchdog gates remain unchanged.
