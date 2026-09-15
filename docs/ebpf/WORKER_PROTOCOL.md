# Private incident worker protocol

Status: accepted for bounded local file/cache tests through the optional launcher.
See [local qualification](FILE_CACHE_LOCAL_QUALIFICATION.md) for results and limits.
OOM support has separate [bounded local qualification](OOM_LOCAL_QUALIFICATION.md).

The dedicated `worker/cmd/memlens-filecache-worker` executable now builds for
Linux arm64 and amd64. Its fixed inherited descriptor contract is:

| FD | Purpose |
| --- | --- |
| 0 / 1 | Private request / response pipes |
| 2 | Supervisor redirects diagnostics to `/dev/null` |
| 3 | Retained exact target cgroup |
| 4 | Sealed accepted executable, matched to the running process |
| 5 | Retained candidate bundle directory |
| 6 | Sealed non-executable installation policy |

It accepts no command-line choices or installation paths. All writes have a
one-second deadline, including the terminal result after observation expiry.
The parent enforces its separate lifetime and reap deadlines.

The optional node component supervises one installation-selected executable per
trace. `prototype/trace/workeripc` and `prototype/trace/workerprocess` contain no
BPF loader. The isolated worker module owns the SDK. Ordinary collector binaries,
snapshots, captures, charts and release artefacts do not use this protocol.

## Request and observations

The parent sends one canonical JSON request through a private pipe and closes its
write end. A four-byte big-endian length precedes every message; the receiver
rejects lengths above 4,096 bytes before allocation. Unknown fields, aliases,
duplicate keys, omitted fields, noncanonical encodings and trailing requests fail.
The protocol version is independent of the public trace-stream version.

The request carries the immutable specification, issue time, absolute deadline
and accepted programme-manifest digest. It has no executable path, gadget
reference, bytecode, attach point or arbitrary parameter dictionary. It is not an
authorisation decision: the worker must match the digest against independently
installed acceptance policy and revalidate inherited descriptor 3 against the
target's exact cgroup identity. The supervisor retains its own cgroup reference.

The worker sends readiness before observations, then typed file/cache/OOM records,
then exactly one result followed by EOF. Startup failure may send an engine-failed
result without readiness. File requested/completed byte counts remain separate;
cache records report additions/removals in base pages. These messages describe
individual observations. The public v2 session accumulates them into default
aggregates; confirmed paths additionally permit public file event frames.

The terminal result may include the bounded canonical cgroup `correlation` object
described in [STREAM.md](STREAM.md). The worker samples through its retained
descriptor before activation and after detachment on normal expiry. Private
validation binds evidence to the request issue time and fixed one-second normal-exit
grace, its observation window and admitted duration. Invalid, stale or oversized
correlation fails the protocol. Authorisation-loss results carry no correlation.

OOM messages preserve private protocol version 1 and its 4,096-byte ceiling.
Their kind-specific event carries only bounded victim context; wrong-kind events,
invalid process context and out-of-window timestamps fail. The optional terminal
`oomCorrelation` replaces file/cache correlation for OOM requests and is capped at
2,560 bytes. Mixed-kind correlation is rejected. Kubernetes context is assembled
by the control service, never by this worker. Public OOM streams use version 3;
private and public versions are deliberately independent.

Writer and reader independently enforce event and private event-byte budgets.
Readiness and the result each have one additional 4,096-byte message reserve.
The public session separately counts escaped HTTP output bytes. Short or failed
writes are never retried. An exhausted budget stays exhausted while preserving
the terminal reserve. Terminal counts cannot contradict already delivered events.
Unknown counters remain unknown.

Default file records must have an empty path. Confirmed paths require valid UTF-8,
no NUL and the admitted byte limit. The private pipe preserves exact path text;
public encoding remains responsible for visible terminal-control escaping.
Ordinary formatting/JSON of requests and writers cannot expose their contents.
Raw messages and callback errors must never enter logs or persisted evidence.

## Supervision and ownership

Startup is bounded at five seconds, even for a five-minute request. The supervisor
enforces the earlier parent/request deadline, uses private OS pipes and sends
stderr directly to `/dev/null`. Signals use the owned process handle, avoiding
numeric PID reuse after reaping. Linux parent-death SIGKILL is set at launch, with
the launching OS thread retained throughout the worker lifetime.

Cancellation or invalid output sends SIGTERM, escalating to SIGKILL after 500 ms.
A valid terminal message or natural request expiry permits one second for normal
exit, allowing final counters to drain after the observation deadline. An earlier
parent deadline remains cancellation. Event callbacks stop at the deadline;
terminal draining cannot forward late events or accept malformed output.
The grace period and reaping after SIGKILL share a two-second process-exit bound.
A result is accepted only after successful process exit;
a result followed by non-zero exit fails. The pipe reader is also joined, allowing
one second for an outstanding output callback. No growing queue retains events.

The normal-exit change was accepted and exercised in local kernel tests; see
[ADR 0013](../adr/0013-bound-normal-worker-exit-within-teardown-budget.md).

An unconfirmed reap or retained callback raises `ErrCleanupUnconfirmed` as a panic.
It reaches the node's adapter-panic boundary, which retains the execution lease
and marks cleanup unavailable. Returning an ordinary engine error would wrongly
permit that boundary to release its cgroup handle. The waiter remains responsible
for eventually reaping an unresponsive process.

The supervisor owns only its direct child. The worker adds a seccomp layer,
synchronised across existing threads, which denies process clones, fork/vfork,
further exec and new sockets while permitting the required Go thread flags.
Clone3 returns ENOSYS so thread creation must use inspectable clone flags. The
filter rejects incompatible audit architecture and the high syscall-number ABI
flag. It requires an existing container seccomp filter and does not replace it.
The worker disables dumpability/core files and requires exactly BPF/PERFMON in
effective/permitted capabilities, with no excess bounding/inherited/ambient caps.

These restrictions have passed unprivileged Linux tests, including existing and
new Go threads. The SDK incident path, full deployment profile and worker image
remain unqualified; no incident-runtime or kernel-teardown
qualification is claimed from startup helpers alone.

## Verification boundary

Protocol race tests cover canonical requests, malformed input, path consent,
typed records, byte/event limits, short writes, terminal ordering, contradictory
counts and redaction. Decoder fuzzing includes a valid request and malformed seeds.

Process tests use real Linux children in a network-disabled, read-only container
with all capabilities dropped. They cover startup/expiry, ignored SIGTERM,
malformed output, callback panic/retention, non-zero exit and terminal output
without process exit. Started fixture processes were reaped. The unreapable-state
branch uses a controlled wait channel; no unkillable process is created.

No test described here loads incident BPF. Parent-death behaviour, actual kernel
resources, real target forwarding and complete node/SDK integration still require
qualification under the programme acceptance gate. The classic BPF seccomp policy
is a process restriction, not an incident tracing programme.
