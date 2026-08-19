# Optional eBPF Tracing Design Gate

Status: proposed; no eBPF code or elevated chart profile exists
Date: 18 July 2026

## Decision

KubeMemLens may add on-demand eBPF tracing only as a separately installed, explicitly enabled extension. The standard agent and collector remain the complete default product: non-privileged, capability-free, cgroup-read focused, bounded and useful without eBPF.

The extension targets compatible Linux worker nodes in self-managed Kubernetes and managed GKE, EKS and AKS node pools. Cluster brand is not the compatibility signal. Each node must pass a runtime preflight for architecture, kernel, BTF, attach points, security policy and required capabilities. Restricted/serverless environments such as EKS Fargate, Windows nodes, or provider modes that reject the required workload must report `unsupported` and retain the normal KubeMemLens workflow.

## First use case

The first implementation should do one job: trace file activity and page-cache churn for one authorised Pod/container during a short incident window. OOM-kill tracepoints may follow using the same lifecycle. Allocation tracing and language profilers are separate proposals because their cost and data semantics differ.

The command shape is deliberately explicit:

```text
kubectl memlens trace files pod/POD [-n NAMESPACE] \
  --duration 30s --max-events 10000 [--show-paths]
```

`--show-paths` permits raw process/file paths in the direct result only. It requires an interactive warning unless `--yes` is explicitly supplied. Paths remain excluded from default metrics, logs, captures, dashboards and incident bundles.

## Architecture

Use a small admission/controller component plus a node-local tracer DaemonSet rather than adding BPF loading to the standard agent.

- The CLI submits a trace request through the Kubernetes API under its own identity.
- Admission applies resource-level authorisation, verifies the caller can read the selected Pod, resolves the immutable Pod UID/container/node/cgroup identity, and creates a short-lived request.
- Only the tracer on the resolved node observes that request.
- The tracer loads an embedded, allowlisted, release-matched BPF CO-RE object, configures the target cgroup filter before attachment, and streams bounded events.
- The result records build ID, kernel, target UID, start/end, termination reason, event/loss/truncation counts and unsupported fields.
- All links and maps are owned by the request context and destroyed on expiry, cancellation, target replacement, disconnect or shutdown. Persistent bpffs pins are off by default.

No API accepts arbitrary BPF bytecode, map definitions, attach points, kernel symbols, path glob programmes or user-supplied C. The kernel [verifier](https://docs.kernel.org/bpf/verifier.html) validates programmes, while [libbpf CO-RE](https://docs.kernel.org/bpf/libbpf/libbpf_overview.html) and BTF provide a portable build path; neither replaces KubeMemLens authorisation and bounding.

### Candidate execution engine

[ADR 0003](../adr/0003-evaluate-inspektor-gadget-behind-kubememlens-admission.md) selects a reproducible Inspektor Gadget evaluation set for the prototype. Inspektor Gadget documents in-kernel filtering for Kubernetes namespace, Pod and container fields, signed image-based gadgets, digest allowlists, and support for standard AKS, EKS and GKE nodes while excluding Fargate, virtual-container and Autopilot-style environments. Those capabilities reduce bespoke kernel work but do not replace this design.

In particular, KubeMemLens must not expose a direct `kubectl gadget` passthrough and call the result multi-tenant safe. Client-side Pod checks are bypassable. The KubeMemLens admission service must remain the server-side authorisation and immutable-target boundary, with the upstream engine reachable only through the reviewed node-side integration. File-operation gadgets also do not prove page-cache hits or misses and have documented blind spots such as `io_uring`; the result contract must name observed file activity precisely and expose incompleteness.

## Security profile

The optional profile must:

- Be absent from default Helm rendering and require a separate installation command/value file.
- Use a dedicated namespace, ServiceAccount, RBAC and NetworkPolicy.
- Use a dedicated node pool, taints/tolerations and affinity when operators need stronger tenant separation.
- Keep `allowPrivilegeEscalation: false`, a read-only root filesystem and a provider-supported LSM profile where possible.
- Request only capabilities demonstrated necessary on every supported kernel. Prefer `CAP_BPF` and `CAP_PERFMON`; never silently fall back to `CAP_SYS_ADMIN` or `privileged: true`.
- Mount only the read-only kernel metadata needed by the reviewed implementation. No writable `/sys`, `/proc`, host root or container-runtime socket.
- Avoid `hostPID`, `hostNetwork` and `hostIPC` unless a separately reviewed tracepoint proves one indispensable.
- Authenticate and authorise every request. NetworkPolicy is defence in depth, not the authorisation boundary.

Kubernetes warns that privileged Pods override several kernel confinement controls and receive all Linux capabilities. Kubernetes also treats privileged-workload creation as potential node access. This extension therefore cannot claim compliance with the Restricted Pod Security Standard, and installation must require a deliberate platform-admin exception scoped to its namespace and image digest.

## Multi-tenant isolation

Filtering must happen before an event is emitted to userspace. A userspace-only namespace or PID filter is insufficient because the tracer would already have received another tenant's data.

- Authorisation is checked against namespace, Pod UID and container at request creation and stream attachment.
- The tracer accepts only the server-resolved cgroup identity; clients cannot submit raw cgroup IDs or node names.
- A Pod UID/container-start change terminates the request rather than retargeting it.
- One request cannot broaden its scope after admission.
- Per-namespace and per-node concurrency quotas apply independently.
- Results are streamed only to the requester and are not stored by the collector.
- Audit events include caller, namespace, target, requested capabilities, bounds and termination reason, but never raw paths.

## Hard limits

Initial defaults and absolute ceilings:

| Control | Default | Hard ceiling |
|---|---:|---:|
| Duration | 30 seconds | 5 minutes |
| Events | 10,000 | 100,000 |
| Encoded output | 8 MiB | 32 MiB |
| BPF map memory | 8 MiB | 32 MiB per request |
| Concurrent traces | 1 per node | 2 per node |
| Path bytes | 256 per event | 512 per event |
| Process name bytes | 16 | 16 |

The tracer must also have Kubernetes CPU/memory requests and limits, a bounded ring buffer, event sampling, a loss counter and a per-request watchdog independent of client connectivity. Hitting any ceiling terminates or samples predictably and marks the result incomplete. Limits are compile-time/server-side maxima; CLI flags can only reduce them.

## Compatibility preflight

`kubectl memlens trace doctor` should report, per node:

- Linux/architecture and managed-provider/node-pool identity when available.
- Kernel release and BTF at `/sys/kernel/btf/vmlinux`.
- Required programme type, helper and tracepoint availability.
- Capability and seccomp/LSM admission outcome.
- bpffs/link lifecycle support without mutating the node.
- `supported`, `degraded` or `unsupported`, with the precise reason.

The CI matrix must cover supported upstream Kubernetes nodes plus representative GKE, EKS and AKS Linux node pools before those providers appear in the compatibility table. GKE Autopilot privileged workloads use provider allowlists and reject most privileged Pods by default, so Autopilot support requires a provider-approved path rather than a chart workaround ([GKE documentation](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/about-autopilot-privileged-workloads)). EKS Fargate permits capabilities to be dropped but not added, so it cannot host this tracer ([EKS Pod security](https://docs.aws.amazon.com/eks/latest/best-practices/pod-security.html)).

## Failure and rollback

- Any preflight, verifier, attach, authorisation or target-resolution error fails closed before streaming.
- Unsupported eBPF never disables cgroup-based diagnosis.
- Upgrade uses an idle-first rollout: reject new requests, expire active traces, verify zero owned links/maps, then replace Pods.
- Uninstall runs a bounded ownership-specific cleanup check; it never removes unknown BPF objects.
- A feature flag and optional profile removal are the rollback. No schema or persistent-data rollback is required.

## Implementation gates

Implementation must not start until all of these are approved:

1. This architecture and the [threat model](../security/KubeMemLens-threat-model.md).
2. The [benchmark protocol](BENCHMARK_PROTOCOL.md) and quantitative acceptance thresholds.
3. A programme/attach-point data inventory proving in-kernel tenant filtering.
4. Kubernetes manifests reviewed against managed GKE, EKS and AKS policies.
5. BPF source/gadget licence, signature policy, version skew and helper compatibility reviewed.
6. An independent security review of loader, RBAC, multi-tenant isolation, teardown and supply chain.

The feature remains deferred until these gates and the prototype benchmarks pass.
