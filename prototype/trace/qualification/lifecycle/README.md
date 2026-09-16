# Local lifecycle qualification helper

This Linux-only administrative test helper is separate from the incident worker,
standard agent and chart. It supports the BPF-007 fault matrix on the owned local
test cluster. Its inputs come from the qualification operator's fresh Kubernetes
and CRI identity checks and the exact reviewed executable hashes.

`identify` verifies a specific node-service executable through `/proc/PID/exe`,
opens a pidfd and records its process start ticks. Its cgroup must match the full
CRI container ID supplied by the operator's fresh Pod/runtime binding. `snapshot`
requires that same container, PID, start value and executable hash. It reads only that process's per-thread
child lists, with fixed bounds. A child is selected only when its inherited target
descriptor is a cgroup v2 directory with an explicitly permitted fixture ID,
its executable matches the accepted worker hash, its command matches the fixed
worker invocation, and its parent/lifetime checks succeed. Hashes and pidfds bind
ownership; kernel object names only classify the already-owned control map.

Snapshots return counts, captured owned map/programme/link IDs, activation state
and bounded monotonic/realtime clock alignment. Owned map information also supplies
kernel allocation bytes; the helper separately reserves both ring mappings and
four native pages per worker, matching the accepted worker accounting. Missing
allocation information is an observation failure. They contain no victim PID,
command, path, target name or request identity. Excluded or uncertain children
remain explicit and cannot authorise a successful all-workers cleanup claim.
Only control-map element zero is read; other map contents remain private.

`check` receives the canonical captured-ID object, with exactly `link`, `map` and
`prog` arrays. It opens only those IDs to count remaining references and closes
each handle. Empty arrays mean no IDs were captured in that category; they alone
do not prove that no objects were created. Qualification also requires an activation
witness or an explicit before-load rejection, plus a final owned-worker snapshot.
Missing/null categories, duplicate keys, invalid categories and oversized input fail. This operation never
enumerates global BPF IDs, unpins objects, deletes maps or detaches links.

`signal` permits TERM or KILL through retained pidfds for selected workers or the
verified node parent. Any excluded child blocks the action. The harness must
retain the original process identity across an action and resolve a new node
process only when starting a separate case after a verified restart. This helper
is not an unauthenticated server or a general host-process cleanup facility.

Use one or two already approved synthetic targets and freeze the node/worker
hashes, cgroup IDs, image and policy before each case. The harness owns HTTP
admission, transport faults, declared execution deadlines, repeated snapshots,
resource measurements and final cleanup evaluation. A failed snapshot is unknown
evidence, never zero remaining state. Clock uncertainty is capped at 5 ms and
must be included when deriving teardown upper bounds.

Build with the optional module's pinned Go toolchain and CGO disabled. Unit tests
run real Linux child processes with a cgroup descriptor, but load no BPF. When
running a compiled test binary in Docker, use a shell parent so the test process
is not PID 1; the helper deliberately refuses that identity. The runtime test
container can be read-only, network-disabled and capability-free.

`partial-attach` verifies and retains the node's sealed worker executable before
emitting a ready acknowledgement. Its exact accepted hash and immutable seals
establish authority; later child checks use that retained inode/device identity,
pidfds, parent membership and the selected cgroup descriptor. The harness starts
one or two already admitted eight-second fixture streams only after readiness.

This administrative operation traces only those workers' threads. It stops on the
first successful BPF link creation and freezes the owned thread groups to inspect
a stable state. At least one worker must have some but fewer than its accepted two
or five links, with its control map inactive. It then terminates the owned workers
and drains trace-exit notifications so the node supervisor can reap them. Missing
or complete attachment is not a passing partial-attachment witness.

The observer waits at most four seconds for the selected workers, processes at
most 20,000 stop events over three seconds, and bounds exit draining to one second.
`PTRACE_O_EXITKILL` and the unchanged node watchdog bound observer failure. The
observer stays on one OS thread, tracks only verified worker thread groups, and
handles clone notifications without adopting another debugger's tasks. It reads
syscall number, BPF command and return metadata through the Linux
[ptrace UAPI](https://github.com/torvalds/linux/blob/master/include/uapi/linux/ptrace.h).
It does not inspect tenant memory, retain addresses, alter registers or syscall
arguments, or suspend the worker's seccomp policy.

Run this only as an administrator on the owned qualification node. Ptrace access
belongs to the external test helper; no tracing/debug capability is added to the
incident worker, API, standard agent or chart. Retain failed and inconclusive
attempts, captured IDs and the first-link stop clock. Verify teardown and the
unrelated control after every attempt. No programme bytes, map contents, security
profiles or incident limits are changed by this operation.
