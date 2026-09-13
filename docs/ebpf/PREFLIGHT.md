# Bounded node preflight contract

The separate [prototype module](../../prototype/trace/README.md) implements the
non-attaching stage accepted in the [engine contract](ENGINE_CONTRACT.md).
`internal/tracepreflight` owns typed checks, stable reason codes and report
validation; the optional Linux adapter owns OS access. CLI formatting consumes
validated reports. Ordinary diagnostic commands do not call this module.

## Meaning and limits

Schema 1 reports the engine baseline scope, a digest of its fixed profile, the
observation time, individual checks and the aggregate supported/degraded/
unsupported state. The profile binds engine identity, ordered checks and the
inventory scope `current-and-recorded-worker-descriptors`. Every report states
that custom programme approval is pending. Reference OCI manifests are hashed
against the exact accepted inputs; they are never treated as a runtime allowlist.

The adapter checks native Linux architecture, kernel >= 5.10, cgroup v2 memory,
parseable BTF, fixed hooks, capabilities, seccomp/LSM metadata, bpffs and ownership.
A missing BTF file differs from malformed or unreadable BTF. Missing hooks and
helpers differ from policy denial and inaccessible inventories. Unknown values
remain unavailable. No raw symbol inventory, kernel address, process label,
verifier log, unrelated BPF name or process identity enters the report.

Five fixed programme loads use Kprobe/TracePoint types and at most sixteen
instructions, then close their descriptor without attaching. They test the two
programme types and current-cgroup, kernel-read and boot-time helpers. One fixed
ring-buffer map uses a single host page, bounded to 1 MiB, and closes immediately.
The map library also creates and closes two fixed one-entry, four-byte array
maps for object-name support. These transient maps are included in ownership
and administrator-census checks. No event reader is started. The programme loader uses one BPF_PROG_LOAD syscall
with a fixed 4 KiB verifier log; it never grows or exports that buffer. Only
necessary helper arguments are encoded. The general library's growing feature
probe is deliberately excluded.

Each invocation runs serial checks in a new child process. The typed checker
rejects concurrent use of its instance. Its deadline is at most fifteen seconds;
the parent terminates and reaps an overdue process. Kernel syscalls do not accept
Go cancellation, so the process boundary is required. Deployment CPU/memory and
outer Job deadlines bound the remaining execution. There is no automatic retry.
Reports contain at most 24 checks and 16 KiB of JSON, with values bounded to
128 printable ASCII bytes. BTF reads are capped at 32 MiB; hook files at 8 MiB;
reference manifests at 16 KiB each. Failed prerequisites skip subsequent loads.
Optional bpffs/LSM inventory gaps still permit the real permission probes, but
keep the aggregate result degraded.

## Descriptor ownership and administrator census

Linux's [BPF syscall implementation](https://github.com/torvalds/linux/blob/v7.0/kernel/bpf/syscall.c)
requires SYS_ADMIN for global object enumeration and descriptor acquisition by
object ID. Giving this to the worker would violate the accepted privilege
contract. Runtime preflight therefore scans only `/proc/self/fdinfo`, and, when
a trusted supervisor receipt exists, that recorded worker's descriptor view in
the same PID namespace. It never opens unrelated global BPF objects.

The scan is bounded to 256 descriptors and 4 KiB per fdinfo file. It records only
kind, ID and programme tag internally. An inherited BPF descriptor makes startup
uncertain. After probes, the owned descriptor set must be unchanged. New owned
descriptors are reported as an orphan. The seccomp policy excludes attach, pin,
global object enumeration/acquisition, perf-event creation and FD transfer by
socket; the source never performs them. An entry under the reserved
`/sys/fs/bpf/kube-memlens-tracer` namespace is uncertain state, never permission
to delete it.

An optional receipt at `/run/kube-memlens-tracer/ended-objects.json` must be a
root-owned private regular file, at most 16 KiB, beneath a supervisor-controlled
directory. The final path may not be a symlink. It binds boot identity, ended
worker PID, process start ticks, descriptor number, object kind/ID and programme
tag. The start ticks are checked before and after the scan to reject PID reuse.
An exact surviving match reports an owned orphan. A mismatch or unreadable owner
reports uncertainty. An absent ended worker proves only that its descriptors
are gone; it is not a global proof about pins or attachments. No recovery code
closes another worker's descriptors or deletes an unknown object.

A separately built, opt-in administrator test performs global programme/map/link
enumeration before and after diagnostic loads. It runs on an owned disposable
Linux node with the administrator's existing permissions. It is neither a
runtime dependency nor an elevated helper in the profile. Global state can change
when Kubernetes creates container cgroup programmes; the zero-state test holds
its container fixed while comparing inventories. Future incident workers need
ownership records and attachment-specific teardown proof; this baseline does
not qualify that lifecycle.

## Local verification boundary

On Linuxkit 7.0.12 arm64, a two-node kind v1.37.0 test exercised an all-supported
baseline and a second node whose seccomp policy denied programme loading. Only
BPF and PERFMON were available to either workload. Both nodes share one kernel;
this is mixed-policy evidence, not multiple kernel-family or provider support.

The local test also exercised capability denial, an owned held-map receipt,
process-identity mismatch, worker timeout with a held map, and Kubernetes denial
of tenant access to node-associated results. The worker's held map was absent
from the subsequent administrator census. A controlled six-probe run left the
same 237 programmes, 11 maps and six links in the global census. No incident
programme was attached. Missing BTF, unavailable hooks and LSM metadata are also
covered by fixtures; no AppArmor/SELinux enforcement qualification is claimed.

The standard repository checks and a separate nested-module gate cover this
code. Linux runs are necessary because macOS tests cannot exercise the adapter.
The nested module has its own pinned dependencies and vulnerability scan; its
code and licences remain outside the standard release distribution. Independent
security and performance review, custom programme freeze and R7 acceptance are
still separate gates.
