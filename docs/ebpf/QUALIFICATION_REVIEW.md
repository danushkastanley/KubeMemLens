# R6 qualification review inputs

Date: 16 September 2026. Status: review packet retained; independent reviews
unperformed. [The local idle experiment](IDLE_LOCAL_QUALIFICATION.md) failed the
mandatory budget in all five pairs; [ADR 0015](../adr/0015-reject-current-ebpf-candidate-on-idle-cost.md)
records no-go. No trace profile is supported and R7 remains blocked.

## Candidate

The reviewed implementation boundary is commit
`57c300f74cfd216fe4c916e8c14dd6ae48b12d94`, merged through PR #119. BPF-008 adds
administrative measurement and evidence tooling without changing the incident
worker, programmes or standard agent. Local certificate rotation changes the
test identities; the measurement manifest records the resulting configuration.

| Component | SHA-256 |
| --- | --- |
| Local image | 4a957b8fa80324e9ce8a66d6876766eb4d1ea13ea85a83973b245e4238b741a3 |
| Node/API executable | e4f071c6eb61aedde3e0ebe12f4713b5bc524b6210e555c83d8fb57433becab3 |
| arm64 incident worker | 6fbd2d17ea799fe84f57113454bf2071ea5cea1c741fa5529eb9fcbc466c1ffd |
| Engine release | 49f6beac38ff9310a3863b7bad3f14854890fba4403d7d49d170b310eb854e08 |
| Programme index | a968adef5da2266edffafe054ab8b3ed782de181a363974636eebda30d9983c2 |

The exercised kernel is LinuxKit 7.0.12 arm64, with Kubernetes 1.37.0 and containerd.
Two kind nodes share that kernel. Cross-builds do not establish native amd64
performance, a second kernel family or a managed-provider result.

## Review responsibilities

| Review | Required independent scope | Current state |
| --- | --- | --- |
| Kernel/eBPF security | Loader and SDK patch, programme filtering, helpers/hooks, bounded maps/ring, seccomp, partial attach and teardown | Reviewer not assigned; no independent verdict |
| Kubernetes multi-tenancy | Aggregation authentication, uncached authorisation, lifetime binding, node TLS, quotas, revocation, namespace and NetworkPolicy boundaries | Reviewer not assigned; no independent verdict |
| Performance | Frozen workload/measurement method, paired controls, complete repetitions, overhead, raw evidence and independent rerun | Reviewer not assigned; no independent verdict |

The implementer's tests and assessment are evidence inputs, not these independent
reviews. Reviewer coordination and managed execution require separate authorisation.
Each eventual review must identify the exact source, artefacts, configuration and
raw evidence digest, distinguish reproduced findings from hypotheses, and retain
finding severity, owner, decision, limitation and verification. Unresolved high or
critical findings block progression; medium findings cannot be accepted without
an owner, decision and stated limitation.

## Control and evidence map

| Boundary | Source and documented contract | Existing verification and limits |
| --- | --- | --- |
| Fixed executable and programme trust | [Engine contract](ENGINE_CONTRACT.md), [SDK patch](../../prototype/trace/worker/sdk-policy.patch), [object policy](../../prototype/trace/filecache/object_policy.go) | Signed/digest-matched candidate, sealed executable/policy and object reproduction; complete redistribution reconciliation remains pending |
| Principal and exact target | [Admission ADR](../adr/0010-admit-traces-through-a-separate-aggregated-api.md), [admission module](../../internal/traceadmission), [node binding](../../prototype/trace/nodebinding) | Local aggregation, forged identity/direct connection denial, revocation and real Pod/container replacement; Node UID replacement has contract coverage |
| Pre-emission isolation | [File/cache programmes](FILE_CACHE_PROGRAMMES.md), [OOM programme](OOM_PROGRAMMES.md), [programme source](../../prototype/trace/programmes/filecache) | Accepted local noisy-neighbour/victim-only cases; no managed-provider qualification or universal hook coverage claim |
| Ephemeral output and privacy | [Stream contract](STREAM.md), [session module](../../internal/tracesession), [worker protocol](../../prototype/trace/workerprocess) | Default path omission, explicit consent, bounded UTF-8 framing, authority-sensitive finalisation and incomplete-disconnect semantics |
| Limits, ownership and faults | [Lifecycle report](LIFECYCLE_LOCAL_QUALIFICATION.md), [administrative census](../../prototype/trace/qualification/lifecycle/README.md) | 97 core local cases and three foreign-control repetitions; captured owned state absent, unknown state preserved; some fault classes remain contract-only |
| Resource cost | [Benchmark protocol](BENCHMARK_PROTOCOL.md), [idle method and harness](../../hack/ebpf-qualification/README.md) | Five complete idle pairs failed; other workload benchmarks and independent rerun remain unperformed |

Read the individual qualification reports for semantics and unsupported cases:
[file/cache](FILE_CACHE_LOCAL_QUALIFICATION.md), [OOM](OOM_LOCAL_QUALIFICATION.md)
and [lifecycle](LIFECYCLE_LOCAL_QUALIFICATION.md). Global OOM was not induced in the
shared Docker VM. A Node condition is not proof of a kernel global-OOM decision.

## Findings and outstanding obligations

The [dependency inventory](FILE_CACHE_DEPENDENCIES.md) retains worker findings
GO-2026-5064, GO-2026-5338 and GO-2026-5622 with scanner call traces, plus the
module-only OpenPGP finding GO-2026-5932. Its import-graph/advisory discrepancy
assessment needs independent resolution. Root/launcher module/package findings
GO-2026-6094 and GO-2026-6107 remain recorded. No suppression or waiver is granted
by this packet.

The final licence/source reconciliation must cover embedded/non-Go inputs,
modified SDK source, programme source/build material and package-specific terms.
The current inventory does not grant image publication approval.

Review exact ephemeral raw-path, PID/command, diagnostic, audit and retention
boundaries. Public evidence must contain numeric measurements and opaque instance
proofs only; private credentials and runtime identity configuration stay local.

## Decision boundary

Local idle evidence can reject this candidate. Passing idle alone cannot qualify
normal/high-rate/noisy/concurrent/flood workloads, delivery/loss/scan regressions,
independent security review or provider support. Retain complete failed runs and
record unrun cases explicitly in the final ADR. A conditional result leaves R7
blocked; a no-go closes the candidate without weakening the standard cgroup product.
