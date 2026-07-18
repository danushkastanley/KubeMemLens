# ADR 0003: Evaluate Inspektor Gadget Behind KubeMemLens Admission

Date: 18 July 2026
Status: accepted for prototype evaluation; not approved for public tracing support

## Context

KubeMemLens needs bounded, on-demand file activity and OOM attribution without becoming a general eBPF platform. Inspektor Gadget already supplies image-based gadgets, [in-kernel Kubernetes target filtering](https://inspektor-gadget.io/docs/latest/reference/run/), [signed OCI artefacts and digest allowlists](https://inspektor-gadget.io/docs/main/reference/restricting-gadgets/), [standard AKS/EKS/GKE support](https://inspektor-gadget.io/docs/main/reference/requirements/) and explicit duration controls. Reusing reviewed upstream programmes could reduce custom loader and kernel-compatibility work.

A direct CLI passthrough is not an acceptable tenant boundary. A local check that the caller can read a Pod can be bypassed, and it does not prove that the server resolves an immutable Pod UID/container/cgroup or prevents a request from broadening after admission. Shared multi-tenant clusters remain in scope.

## Decision

Evaluate Inspektor Gadget as the execution engine behind the KubeMemLens trace admission and result contracts. Do not add `kubectl gadget` passthrough, Inspektor Gadget deployment, elevated permissions or tracing values to the standard KubeMemLens chart.

The first reproducible evaluation set is Inspektor Gadget `v0.54.1` with these multi-architecture OCI index digests:

- `top_file@sha256:89b0ec13fb1478ab940b94949313d1a1edf56dc3179ee44754fb0e0ea3d81520`
- `trace_open@sha256:37d740a233db1c8fb6b66951c932b878036504ec50d4e22c570bc8a70fddcea2`
- `trace_oomkill@sha256:b0fbbb1650379f088875d35506069d187914de7d0bae59681c46d2961a313cd8`

These are prototype inputs, not automatically trusted production dependencies. Any accepted extension must:

1. authorise the caller server-side and resolve the exact Pod UID, container start and cgroup identity;
2. configure the target filter before events reach userspace;
3. allow only reviewed gadget digests and keep upstream signature verification enabled;
4. enforce KubeMemLens duration, event, byte, map-memory and concurrency ceilings server-side;
5. translate upstream output into a versioned, minimised KubeMemLens result contract;
6. terminate on target replacement, cancellation, disconnect, timeout or tracer restart;
7. pass the existing multi-tenant threat model, managed-provider matrix and independent review.

## Alternatives considered

- A bespoke CO-RE loader and programmes give maximum control but create a larger verifier, compatibility, lifecycle and supply-chain surface.
- A direct `kubectl gadget` wrapper is smaller but cannot enforce the KubeMemLens server-side tenant boundary and would expose upstream output contracts directly.
- Deferring all prototype work avoids new risk but leaves no reproducible implementation candidate for the documented Phase D gate.

## Consequences

- The prototype can reuse established, signed programmes while KubeMemLens owns authorisation, bounds, privacy and evidence semantics.
- Inspektor Gadget version skew, daemon privileges, gadget licence, signature policy, output stability and managed-provider behaviour become review inputs.
- File gadgets report observed file operations, not page-cache hit/miss causality. Their [documented `io_uring` limitation](https://inspektor-gadget.io/docs/main/gadgets/gadget_limitations/) must remain visible; KubeMemLens must not relabel file I/O as complete page-cache attribution.
- The standard product remains unchanged and capability-free.

## Rollback

Remove this candidate from the prototype plan and retain the standard cgroup-only product. No runtime resource, schema or stored data changes exist to roll back.
