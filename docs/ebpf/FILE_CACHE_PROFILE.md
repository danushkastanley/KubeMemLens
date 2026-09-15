# Candidate file/cache node profile

Status: accepted for bounded local candidate tests. Capability-free launch and
containment tests pass. The accepted worker now passes pre-attach map validation
and the local file/cache workload cases; see
[local qualification](FILE_CACHE_LOCAL_QUALIFICATION.md). This profile is installed
only in the isolated test cluster, outside the standard chart and preflight.

`prototype/trace/seccomp/filecache-node.json` is separate from the accepted
non-attaching `binding-node.json`. It preserves the latter's default-deny policy,
TCP listener rules and process supervision calls. The incident worker adds its
own synchronised seccomp restriction before accepting input; that layer prevents
process descendants, further exec and new sockets. The node can still launch and
reap its direct worker. Do not use the incident profile for ordinary preflight.

The candidate also replaces unrestricted parent `clone` with exact Go 1.27
thread flags and VFORK/VM/SIGCHLD child-launch flags, with optional PIDFD. Namespace,
ptrace and arbitrary clone modes are denied in the parent as well as the child.
Clone3 retains the baseline ENOSYS response so Go uses the inspectable clone path.

## Added operations

| Operation | Argument restriction | Implementation reason |
| --- | --- | --- |
| `pread64` | Existing file descriptor | Read immutable installation policy and executable sections without sharing seek position |
| `memfd_create` | Flags exactly 11 or 19 | CLOEXEC, sealing and explicit non-executable policy or executable worker image |
| `fchmod` | Mode exactly 0400 or 0500 | Set policy/image permissions before final sealing |
| `seccomp` | SET_MODE_FILTER with TSYNC | Apply the worker restriction to all existing threads |
| `bpf` | Commands 1, 2, 18, 22, 28 | Owned map lookup/update, BTF loading, map freezing and link creation |
| `perf_event_open` | pid -1, CPU 0, group -1, FD_CLOEXEC | The pinned cilium/ebpf tracepoint attachment implementation |

The full BPF command set is 0, 1, 2, 5, 15, 18, 22, 28: map creation, lookup,
update, programme load, owned-descriptor information, BTF load, freeze and link
creation. Pin/get, programme/map/BTF/link ID enumeration, global ID-to-FD lookup,
raw tracepoint fallback, link update and BPF tokens remain denied. Unsupported
loader paths must fail qualification rather than cause an automatic permission
expansion. Cleanup closes owned link/perf/map descriptors; it does not enumerate
or unpin unrelated objects.

Seccomp cannot inspect the pointed-to perf attributes or BPF command structures.
Accepted programme/source digests, fixed worker entrypoints, static hook/map
validation and immutable target references provide those restrictions. The profile
alone does not prove exact hook selection or target filtering. It also does not
grant capabilities: the incident worker must still have the accepted BPF/PERFMON
budget, without SYS_ADMIN, tracing mount writes, runtime sockets or host namespaces.

## Local verification and remaining gate

Static tests compare the candidate additions with the unchanged baseline and
check exact argument restrictions. Real local Linux tests under this JSON profile,
with all capabilities dropped, exercised sealed executable creation/launch and
worker restriction across existing/new threads, including incompatible ABI denial.
Those tests load no incident BPF and establish no BPF/perf success claim.

The first incident candidate loaded all five file programmes and seven maps;
the pre-attach check rejected the loader's `BPF_F_MMAPABLE` flag on `.bss`.
The pinned loader adds that flag to `.bss` and `.rodata` on supported kernels.
The corrected check permits only that addition on those two data sections,
retains every other flag and shape check, and reserves two extra native pages
for their userspace mappings. Static ELF policy remains exact. The correction
changes the worker identity and requires acceptance before another incident load.
Observed maps and programmes were absent after worker reap; no link was observed
and no link-creation call occurred. This is failure-path evidence, not successful
attachment or workload qualification. The seccomp profile remains unchanged.

The actual worker import graph includes loader feature probes as well as the
custom programme path. Review these source paths and actual kernel operations
with the final acceptance package. Qualify the profile with the approved source,
objects and SDK patch before deployment; unexpected operations remain a failed
gate. The preflight baseline and its digest stay unchanged. Removing the optional
installation policy stops new incident admission; confirm owned cleanup before
removing its image/profile.
