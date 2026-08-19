# ADR 0001: Defer eBPF Until Security and Benchmark Gates Pass

Date: 18 July 2026
Status: accepted

## Context

eBPF could attribute short-lived file and page-cache activity that cgroup counters cannot explain. It also introduces kernel programmes, stronger node permissions, potentially sensitive paths, compatibility differences, and cross-tenant risk. The current default product is intentionally non-privileged and useful without it.

## Decision

Do not implement eBPF tracing in the standard agent or chart. First validate a separately installed, on-demand prototype against the [design gate](../ebpf/OPTIONAL_EBPF_DESIGN.md), [threat model](../security/KubeMemLens-threat-model.md), and [benchmark protocol](../ebpf/BENCHMARK_PROTOCOL.md). Require an independent security review before declaring it supported.

Compatible Linux worker nodes in GKE, EKS, AKS and self-managed clusters are in scope. Provider-restricted or serverless nodes fail preflight rather than receiving a privileged fallback. Shared multi-tenant isolation is a required acceptance criterion. Raw paths may be deliberately shown in a direct trace, but are excluded from default telemetry, logs, captures and persistence.

## Alternatives

- Add BPF loading to the standard agent: rejected because it expands the default blast radius and couples basic diagnosis to kernel privileges.
- Always-on collection: rejected because it increases overhead, retention and privacy risk.
- Use `privileged: true` or `CAP_SYS_ADMIN` for broad compatibility: rejected because provider support does not justify node-wide authority.
- Permanently omit deep tracing: retained as a rollback if the security or performance gates cannot be met.

## Consequences

- Phase D's eBPF feature remains explicitly deferred; the design work is complete but implementation is not.
- The standard installation, K9s workflow and machine-readable recommendations remain capability-free.
- A prototype must carry strict resource ceilings, in-kernel target filtering, provider qualification and observable loss/teardown evidence.
- Supporting a new provider/node image becomes a tested compatibility claim rather than a Helm-value promise.

## Migration and rollback

There is no current migration. A future optional profile must be removable without changing collector data or standard-agent configuration. If qualification fails, remove the optional profile and continue with cgroup-based diagnosis.
