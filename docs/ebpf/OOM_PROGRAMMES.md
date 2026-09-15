# OOM programme candidate

Status: accepted and exercised for bounded local BPF-006 tests. Both architecture
objects reproduce; native kernel execution is qualified only for the local arm64
profile described in [local qualification](OOM_LOCAL_QUALIFICATION.md).

## Event semantics

The pinned [Linux 7.0.12 OOM implementation](https://github.com/gregkh/linux/blob/v7.0.12/mm/oom_kill.c)
marks victims both after its kill operation and on already-exiting paths that
send no new kill. The candidate pairs `oom_kill_process` and
`__oom_kill_process` entry/exit hooks with `mark_oom_victim`. Only a mark inside
the paired kill path can emit. This observes a kernel kill decision, not proof
that userspace has exited. Group OOM can revisit an initially selected victim, so
observation counts are not unique victims or cgroup oom_kill counter increments. Other processes killed solely for sharing the victim's
address space do not pass this marking hook and remain a coverage blind spot.

The decision's non-null memcg identifies cgroup-local scope. Non-memcg allocation
scope remains separate from Kubernetes MemoryPressure, which is not a kernel
OOM classifier. Forced SysRq and absent decision context report unknown scope.
A mark without paired kill context is rejected, covering already-exiting tasks
and missed context without falsely labelling them new kills. Memcg scope can
refer to an ancestor limit; the selected leaf limit is context, not proof that
that particular limit caused the kill.

## Filtering and resource inventory

Filtering follows the victim's exact cgroup v2 ID, read through its task/cgroup
association. The retained target cgroup reference prevents ID reuse while the
emitting programme exists. Invoking-task membership does not select an event.
The selected victim's membership is checked before reading PID/command data and
again before reserving a ring record. Non-selected marks cannot increment the
request's event counters or deliver another tenant's process fields.

Two fixed 64-entry hash maps correlate invoking-task keys with scope and bounded
monotonic timestamps. They contain no command, path or victim pointer. These
private kernel keys are never read out or included in events. Fexit clears the
paired context even after collection is disabled. Context-map failure becomes
unknown scope or a selected-mark rejection, not guessed attribution.

| Resource | Fixed shape |
| --- | --- |
| Hooks | Five fentry/fexit programmes |
| OOM record | 40 bytes: monotonic time, scope, optional kernel PID, context flags, zero padding, 16-byte command slot |
| Context maps | Two hashes, each 64 entries with 8-byte keys and 16-byte values |
| Ring | 256 KiB |
| Counts/control/target reference | Existing fixed incident-worker shapes |

A kernel PID is not a PID translated into the container's namespace. Process
fields belong only to the authorised ephemeral stream. Native kernel commands
that are unreadable, empty or invalid UTF-8 remain unavailable; no replacement
string or raw invalid bytes is retained. Trailing command bytes remain zero.
The decoder rejects contradictory flags, hidden bytes, malformed sizes and
out-of-window timestamps. Loss and unsupported coverage remain explicit.

## Verification and remaining gate

`qualification/oom/test_semantics.py` compiles the exact hook functions against
native test-only helpers, checking unrelated invokers, non-selected victims,
already-exiting paths, scope distinctions, missing context, ring failure,
expiry and cleanup. It does not prove kernel helper or verifier behaviour.
The ELF policy separately bounds hooks, helpers, map layouts and record size.

`build_filecache.py --set all` builds all six file/cache/OOM objects twice.
The default set preserves the four-object file/cache workflow. A version 2
candidate index contains exactly the six kind/architecture combinations; a
version 1 index cannot include OOM. Neither format grants installation acceptance.

Worker, private IPC, public version 3, bounded cgroup sampling and authenticated
control-service Kubernetes context passed non-loading tests and the accepted local
OOM/isolation/recreation/teardown matrix. Group-kill target exit leaves final
counters and correlation unavailable; see the linked results for that boundary. Global-OOM
execution requires a separate disposable Linux VM with declared resource caps;
never exhaust the host or the shared Docker VM. EKS remains deferred until all
phases finish. Independent security and benchmark gates remain unchanged.
