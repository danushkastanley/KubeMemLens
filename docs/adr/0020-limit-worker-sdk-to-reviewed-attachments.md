# ADR 0020: Limit the worker SDK to reviewed attachment classes

Status: accepted for implementation; kernel and resource qualification pending.

## Context

The fixed file/cache/OOM worker receives signed, locally verified programme bytes
and an immutable single-container descriptor. Its object policy accepts only
tracepoints and fentry/fexit programmes. It has no Kubernetes credentials or
container-discovery job.

The SDK's generic eBPF operator nevertheless linked container discovery, network,
TC and uprobe backends. Those imports pulled Kubernetes clients and registered
API schemas into the executable. The admitted arm64 worker occupied 48,234,620
bytes; copying it to a sealed executable memfd makes that backing memory part of
the active cost. An incomplete local diagnostic exposed substantial active cost,
but is not a valid benchmark or a replacement for the recorded R6 result.

## Decision

Extend the checksum-bound SDK patch to enforce the already-reviewed attachment
vocabulary before loading maps and again before attaching. Reject malformed
tracepoints, mismatched tracing sections/types, attachment overrides and every
other programme class. Keep the exact signed hook/helper/map policy in the worker.

Remove the generic operator's container/network/TC/uprobe state and callbacks,
and reject nonempty iterator execution. Leave the SDK's discovery implementation
and its tests untouched; the constrained worker no longer imports those backends.
Some generic metadata and symbolisation utilities remain linked. This is not a
claim that the worker contains only attachment code.

Enforce the dependency boundary for both Linux architectures in ordinary worker
checks and reproducible builds. Keep native tests for allowed and rejected
attachment shapes, plus non-loading preparation of the actual file/cache/OOM
objects. Do not change programme instructions, capabilities, seccomp, sealed
execution, pre-attach validation, cancellation, output limits or cleanup controls.

## Alternatives

- Keeping the generic operator retains an unnecessary executable and initialisation
  cost. Stripping alone had already been applied.
- Removing discovery files from the SDK proved the dependency cost, but would
  remove unrelated SDK APIs and disrupt their tests. That private experiment is
  not the shipping patch.
- Removing executable seals or excluding their memory charge would weaken the
  integrity boundary or measurement. Neither is acceptable.

## Consequences and rollout

The constrained SDK cannot be used as a general gadget runner. Unsupported
classes fail explicitly before kernel loading, rather than relying only on the
outer object's acceptance check. No supported fixed programme gains a new hook.

The patch digest changes the engine identity. Previously accepted worker images,
programme bundles and installation policies do not authorise the new executable.
Reproduce both architectures and prepare a new matched immutable candidate before
bounded kernel tests and the unchanged performance/lifecycle matrix. Binary size
and non-loading tests alone do not establish an active-memory pass or R7 readiness.

Rollback requires the complete previous matched image, policy and programme
bundle, followed by its normal admission checks. Never mix old and new digests.
The standard product remains independent of this optional worker.
