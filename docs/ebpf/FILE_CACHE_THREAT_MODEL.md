# Candidate file/cache worker trust boundaries

Status: implementation review for BPF-005. Bounded local workload isolation and
owned-object teardown cases have passed; allocation ceilings, the full failure
matrix and independent review remain separate gates. See
[local qualification](FILE_CACHE_LOCAL_QUALIFICATION.md). This supplements the
repository threat model and does not grant programme acceptance or R7 readiness.

## Assets and authority

Protect neighbouring tenants' activity, admitted file paths, immutable target
lifetime, node integrity, bounded kernel resources and cleanup ownership. A
namespace requester may choose only admitted intent and lower limits. The
maintainer controls installation trust keys, exact engine/programme digests and
revocation. Signed candidate files are review inputs, not installation authority.

The standard agent, collector and chart have no SDK dependency or incident-worker
installation. The optional node and worker share only the fixed private protocol
and owned descriptors. The worker imports the SDK in a separate Go module.

## Abuse paths and controls

| Abuse path | Implemented control | Remaining proof |
| --- | --- | --- |
| Replace executable, policy or programme after admission | Hash/signature checks; immutable policy/executable memfds; bounded verified ELF/OCI copies; no request-selected paths or keys | Exercise accepted deployment and revocation |
| Retarget another Pod or reused cgroup | Server-resolved lifetime, retained descriptor, cgroup ID plus kernel reference, pre-activation revalidation | Noisy-Pod and lifetime-change kernel tests |
| Leak another tenant before userspace filtering | Fixed in-kernel target checks before ring emission; source/ELF allowlist | Real kernel isolation under load |
| Use paths as a persistent or terminal-control channel | Omit-paths default, explicit consent, bounded UTF-8 records, visible escaping, ephemeral output and redacted diagnostics | Actual workload/path fault tests |
| Supply malicious worker output | Strict bounded pipe and NDJSON decoding, counts/window checks, pinned stream identity/version, no retries after partial delivery | Inject faults through deployed worker |
| Extend sampling after cancellation | Independent signal lifetime, deadline-bound final sampling, revocation suppression at session/controller | Live permission-revocation test |
| Escape through child processes or namespace creation | Exact parent clone flags; child TSYNC restriction denies exec, sockets and process clones; no namespace syscalls | Deployed parent-death and resource-loss tests |
| Exhaust node or retain attachments | Kernel deadline/event limit, fixed maps/ring, pre-attach accounting, bounded supervisor kill/reap and cleanup quarantine | Real allocation/loss/teardown census; later watchdog/benchmarks |
| Substitute an unapproved SDK operator | Fixed read-only ELF store, exact map/hook validation, no data sources, fixed parameters, patched mandatory pre-attach callback | Review and observe actual loader/probe operations |

The seccomp profile cannot inspect pointed-to BPF/perf attributes or stop misuse
of every operation granted to a compromised node. Exact signed source, fixed
entrypoints, descriptor ownership and kernel tests remain necessary. BPF/PERFMON
are powerful capabilities; absence of SYS_ADMIN is not proof of node safety.

## SDK loading review

The candidate calls only the registered eBPF image operator for its verified ELF.
It supplies an empty fixed configuration, disables trace-pipe output and rejects
generic data sources. The custom objects omit generic tracer declarations and
have no socket-enricher replacement maps or network/TC programme types. SDK
PreStart subscribes configured formatters; it does not load the incident object.
Start performs collection loading and attachment, so it is behind the acceptance
gate. The patch disables verifier logs, validates owned maps before attachment,
orders/cancels attachment attempts and preserves cleanup failures.

The SDK and cilium loader can issue feature probes. Embedded SDK objects and
dependency findings remain in the source inventory even when their operators
are not selected. The candidate profile denies unlisted commands, pinning and
global ID enumeration. An unexpected required operation is a qualification
failure requiring review; there is no automatic profile expansion or fallback.

## Evidence and release limits

Tests cover immutable installation, strict protocols, target-handle ownership,
local TLS/HTTP transport, cancellation, aggregate/correlation privacy, native Linux
supervision and seccomp. The assembled arm64 image was checked with all
capabilities dropped: its signatures, sealed descriptors and programme manifests
verify without preparing a target or loading incident BPF. These tests do not
prove kernel semantics. The amd64 worker is reproduced but has no native kernel
qualification. Source/licence and vulnerability records accompany the candidate.

First incident loading requires fresh maintainer source/digest/signature/SDK-patch
acceptance. Independent security review and later R6 gates remain separate.
Disable new admissions and cancel affected sessions on revocation. Retain quotas
and handles while cleanup is uncertain; remove optional resources only after
owned cleanup is confirmed. No trace data or filesystem schema migration is used.
