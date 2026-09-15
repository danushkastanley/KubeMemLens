# Bounded local OOM qualification

Date: 15 September 2026. This is local prototype evidence for BPF-006, not a
provider qualification, independent security review, benchmark or R7 go decision.

## Candidate and environment

The maintainer accepted the exact signed OOM candidate before installation or
kernel execution. The optional node/API pair used a matched immutable image and
public policy. Standard agents, charts, captures and release artefacts are unchanged.

| Identity | SHA-256 |
| --- | --- |
| Engine release | 49f6beac38ff9310a3863b7bad3f14854890fba4403d7d49d170b310eb854e08 |
| Programme index | a968adef5da2266edffafe054ab8b3ed782de181a363974636eebda30d9983c2 |
| Local image | 37a401b2689a82b7ecff7e922fa7d71a5d0ea8258d031f5038c25a8360aa58bd |
| arm64 OOM object | 557f50b20858de0806e91cf8bf4ed4a2db005cb664d2bffe750c4c034f5d24e7 |

Execution used the existing two-node local kind cluster: LinuxKit 7.0.12 arm64,
Kubernetes 1.37.0 and Cilium 1.20.1. Only the node service had BPF/PERFMON and the
accepted incident seccomp profile. The API remained capability-free. No production
RBAC, memory.oom.group, oom_score_adj or global OOM policy changed.

Two synthetic Pods were each limited to 64 MiB, zero swap, 250m CPU, 32 PIDs and a
120-second active deadline, with restart Never, RuntimeDefault seccomp, no token,
read-only roots and all capabilities dropped. Six consumption attempts were used
from the approved maximum of twelve. Each allocator had a 96 MiB ceiling and a
five-second external timeout. The only cgroup limit written by the test harness
was pids.max=32 on the exact owned fixture container after identity checks.

## Observed runtime behaviour

| Case | Result |
| --- | --- |
| Selected idle | v3; normal expiry; zero observed/produced/lost/rejected; overlapping cgroup evidence and observed Kubernetes context |
| Selected memory-limit OOM | Four ephemeral victim events; Pod reported OOMKilled; target_changed termination |
| Other-namespace OOM, two runs | Selected target had zero events and zero produced/lost/rejected counters; normal expiry and overlapping evidence; repeated run confirmed other Pod OOMKilled |
| Simultaneous selected/other OOM | Four cgroup-scope events, all with valid PID and fixed-fixture command matches; both Pods reported OOMKilled |
| Selected Pod recreation | target_changed; new Pod UID differed; no following or events from the replacement |
| Explicit cancellation | cancelled; no events; owned teardown confirmed |
| One-event ceiling during selected OOM | Exactly one event delivered; target_changed won the termination race; final engine counters remained unknown |
| File/cache idle under new worker | Both retained v2, normal expiry, zero aggregates/counters and overlapping cgroup evidence |

Every case matched the accepted engine/programme identities. The administrative
census observed the owned control map active before OOM consumption. OOM sessions
had eight maps, five programmes and five links. All previously captured owned IDs
were absent after each run, and worker count returned to zero. The two OOM fixture
Pods were removed; existing ordinary targets were preserved.

The simultaneous OOM case delivered four kill-path observations across three unique
victim PIDs. Group OOM can revisit a victim already chosen for the initial kill.
Observation totals therefore count hook-reported kill decisions, not unique dead
processes or cgroup oom_kill counter increments. PID cardinality and fixed command
matches were computed only in memory; raw process values were not retained.

The node's existing memory.oom.group=1 kills the entire selected container. Its
lifetime change ends the trace before a valid final cgroup sample can be retained.
Those terminal summaries deliberately omit final aggregates/counters/correlation
and report target_changed. The implementation did not change OOM policy, follow a
replacement or manufacture post-kill measurements to obtain a complete summary.
Idle and non-selected OOM cases verified the normal-expiry sampling path, separate
local/hierarchical counters, finite limits, PSI and fresh API context. Non-zero
post-kill correlation under a surviving target remains unqualified on this profile.

## Privacy and non-loading checks

Victim events exist only in the authorised ephemeral stream. The default numeric
accumulator stores no PID/command. Retained local evidence contains counts,
scope totals and fixed-fixture matches. Bounded node/API log inspection found no
victim payload fields or fixture command text. The inspected trace API audit rows
were metadata-only, with no request/response body or victim context. Kubernetes
may separately audit an operator's fixture exec command; this is not a trace payload.

Root, optional-module and SDK-worker native Linux race suites, vet, root builds
and optional/worker Linux amd64/arm64 cross-builds passed. Both worker binaries
and all six objects reproduced twice. The 36-file signed bundle reproduced exactly;
capability-free non-root image checks verified signatures, sealed executable and
all programme manifests without loading BPF. SDK preparation tests ran with BPF
denied and no readers started. Full-rich v3 framing, malformed context, cross-kind
isolation, authority loss, bounds and control-only Kubernetes context tests passed.
Repository contract, formatting and optional Kubernetes renderer checks passed.

Exact native C helper tests exercise ring failure and missing context; protocol
and session tests preserve unknown/lost/rejected states. These are distinct from
real kernel ring saturation, which was not induced by generating thousands of OOMs.
The live one-event test proves the output ceiling, not a particular final loss count.

## Remaining qualification and rollback

No global-OOM test ran: no separate disposable capped Linux VM was available.
The host and shared Docker VM were not exhausted. Non-memcg/unknown source semantics
have native helper coverage, which does not qualify global kernel execution.
Linux amd64 cross-build/object checks do not establish runtime qualification.
No managed-provider test ran; EKS remains deferred until all phases finish.

[Dependency findings](FILE_CACHE_DEPENDENCIES.md), independent review, further
lifecycle testing and benchmark/go-no-go gates remain open. These local results
must not be described as public tracing readiness or complete OOM coverage.

Rollback removes OOM from the installation allowlist, or restores the previous
matched node/API image and BPF-005 policy after confirming owned cleanup. Existing
cgroup-based OOM diagnosis and standard capability-free delivery remain available.
