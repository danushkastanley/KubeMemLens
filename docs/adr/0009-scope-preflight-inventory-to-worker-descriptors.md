# ADR 0009: Scope preflight inventory to worker descriptors

Status: accepted for the non-attaching baseline, 13 September 2026.

## Context

The accepted [engine evaluation](../ebpf/ENGINE_CONTRACT.md) permits bounded
preflight before custom incident programmes are frozen. The optional worker may
hold BPF and PERFMON capabilities, but must not acquire SYS_ADMIN. Linux requires
SYS_ADMIN for global BPF object enumeration and acquisition of descriptors by ID.
Using global enumeration as a mandatory runtime prerequisite would therefore
prevent every constrained worker from reaching its feature probes.

## Decision

The [preflight contract](../ebpf/PREFLIGHT.md) defines the runtime inventory as
current-worker descriptors and, where a trusted supervisor supplies an ended
worker receipt, that worker's descriptors in the same PID namespace. Bind the
receipt to boot identity, process start ticks and exact descriptor/object identity.
An uncertain or surviving owned object prevents a successful startup report.
Never remove unknown state. Report this limited inventory scope in the profile
whose digest binds every result.

Use a separate, explicitly enabled administrator test binary on disposable local
Linux nodes for the global before/after programme, map, link and pin census. This
binary is excluded from runtime builds and images. Do not deploy an elevated
observer or delegate its privilege to the worker.

Keep runtime BPF syscalls restricted to fixed map creation, fixed programme load
and information about owned descriptors. Programmes are never attached and maps
are never pinned. A fresh process, fixed verifier buffer, output ceiling and
outer deadline bound the diagnostic operation. Read-only metadata mounts and the
small capability set remain explicit in the optional Job.

## Threat considerations

The new active boundary is a trusted administrator invoking diagnostic kernel
operations, not a tenant trace endpoint. Namespace RBAC protects the Job, its
node association and its reports. The worker has no API token, network listener,
host PID view, runtime socket or way to receive caller-selected programme bytes.
Its report discards addresses, verifier text, unrelated symbols and process labels.

A root-private receipt and its parent directory belong to the supervisor. A
namespace caller must never be allowed to supply or modify them. Reject symlink
receipt files, stale boot identities and reused PIDs or descriptors. Unreadable
state is uncertain rather than healthy. A compromised administrator is outside
this boundary; ordinary tenant roles cannot create or inspect these workloads.

The seccomp policy forbids attachment, pinning, global acquisition and socket FD
transfer. These constraints make descriptor closure useful evidence for this
baseline. They do not establish incident-worker teardown: a later attaching
profile needs its own complete ownership and lifecycle contract.

## Alternatives and consequences

Global enumeration in the worker or a privileged helper violates the capability
budget. Names alone cannot prove ownership. Scanning only retained IDs cannot
exclude ID reuse. Deferring all diagnostic probes would lose direct permission
and compatibility evidence, despite their non-attaching scope.

The selected design permits local baseline evidence without broadening runtime
privilege. It cannot claim to find every global attachment or pin, and the
one-shot Job has no shared recovery receipt. Future workers must explicitly
integrate supervisor ownership. Independent security review and custom programme
acceptance remain required before the subsequent incident execution stages.

## Migration and rollback

There is no existing persisted receipt schema or public API to migrate. Remove
the optional Jobs, local image and node seccomp file to roll back. The standard
agent, chart, doctor and release artefacts remain outside this module.
