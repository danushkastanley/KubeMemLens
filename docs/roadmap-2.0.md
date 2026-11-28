# KubeMemLens 2.0.0 plan

Status: proposal, not yet accepted. Written on 28 November 2026 against `main`
at `4907fa2` (release `v1.0.6`).

This page folds the planned `v1.1.0` to `v1.5.0` releases and the open feature
issues into one `v2.0.0` programme, and adds a new workstream: making
KubeMemLens the tool an AI agent reaches for when it needs to know what is
wrong with memory in a cluster. Progress is tracked in GitHub issue
[#450](https://github.com/danushkastanley/KubeMemLens/issues/450); see
[Tracking on GitHub](#tracking-on-github). The public summary stays in the
[roadmap](roadmap.md).

## Summary

- **One target, several phases.** Everything in [#309](https://github.com/danushkastanley/KubeMemLens/issues/309)
  to [#313](https://github.com/danushkastanley/KubeMemLens/issues/313) and
  [#319](https://github.com/danushkastanley/KubeMemLens/issues/319) lands in
  `v2.0.0`. Work is ordered so each phase merges to `main` working and
  verified; see [Phases](#phases).
- **Agents ask one question, not a hundred.** A new `triage` command and a
  local MCP server return a ranked, evidence-backed list of memory findings for
  a namespace, workload, Node or the whole authorised cluster in one call, with
  drill-down handles for the next call.
- **One output contract.** Every command gets the same versioned JSON envelope,
  published JSON Schemas, typed error codes and documented exit codes. This is
  the breaking change that makes the release a major version.
- **Evidence that outlives the Pod.** Collector state survives restarts
  (ADR 0033 stages 2 and 3) so OOMKill and eviction post-mortems can be read
  after the container is gone.
- **The same principles.** Read-only, no telemetry, no outbound calls, no new
  install cost, the caller's own kubeconfig and RBAC.

## Where 1.0.6 leaves us

What an agent can already use:

- `explain pod|workload|node -o json|yaml` emit a versioned explanation schema
  (3 or 6) with composition, diagnosis, severity, independent confidence,
  evidence, caveats, observation windows and copyable next commands. See the
  [explanation schema](explanation-schema.md).
- `top pods|containers|workloads|ns -o json|yaml|csv`, `recommend`, `node`,
  `volumes` and `history trends` have structured output.
- `explain node --rank total|anon|cache|shmem|residual|psi|oom --limit N`
  already ranks contributors on one Node.
- Restricted mode answers from caller-authorised Kubernetes data when the
  collector is absent; see [restricted mode](restricted-mode.md).
- `nextcommand.SafeNames` keeps tenant-supplied names out of suggested
  commands unless they are plain Kubernetes names.

What gets in an agent's way:

| Gap | Effect on an agent | Evidence |
| --- | --- | --- |
| No question-shaped entry point | To answer "what is wrong in `payments`?" the agent runs `top pods`, then `explain pod` once per suspicious Pod, then `history`, then `doctor` | `internal/cli` has no command that ranks findings across a scope |
| Inconsistent structured output | `top`, `status` and `doctor` JSON are bare and unversioned; `history pod`, `compare`, `sample`, `session show` and `replay` have no `-o`; `replay -o` means a legacy export file, not a format | [#309](https://github.com/danushkastanley/KubeMemLens/issues/309), `internal/cli/replay.go` |
| Errors are prose | The agent cannot tell "forbidden" from "collector down" from "Pod not found" without parsing text | `cmd/kubectl-memlens/main.go` exits only 0, 1 or 130 |
| No size control | A cluster-wide `top pods -A -o json` returns every row; an agent pays for all of it in context | `top` has no limit or detail level |
| Evidence disappears | "Why was this Pod OOMKilled last night?" has no answer once the container and collector state are gone | [#310](https://github.com/danushkastanley/KubeMemLens/issues/310), [#319](https://github.com/danushkastanley/KubeMemLens/issues/319) |
| Semantics are easy to misread | Agents conflate working set, RSS, `anon` and file cache, and recommend the wrong limit | Common failure; the [memory semantics](memory-semantics.md) page exists but is not delivered with the data |
| No agent packaging | No MCP server, no agent skill, no machine-readable usage guide | [#309](https://github.com/danushkastanley/KubeMemLens/issues/309) |

## Why this matters for agents

An agent asked "why is memory high in `payments`?" without KubeMemLens
typically does this:

1. `kubectl get pods -n payments -o wide`, then `kubectl top pods`.
2. `kubectl describe pod` for each restarting or high Pod, reading
   `lastState.terminated.reason` for `OOMKilled`.
3. `kubectl get events`, filtered by hand.
4. `kubectl exec ... cat /sys/fs/cgroup/memory.stat` per container, which fails
   on distroless images, needs `pods/exec` (a write-equivalent permission) and
   returns raw counters the agent must interpret.
5. Repeat for the Node with `kubectl describe node` and guess at pressure.

That is tens of calls, thousands of lines of output, a dangerous permission,
and an interpretation step where models often go wrong (treating file cache as
a leak, or working set as RSS).

KubeMemLens already holds the interpreted evidence in one place: the collector
behind the tenant-scoped aggregated API. The agent workstream exposes that as a
small number of high-level, read-only calls whose output is already the
diagnosis.

Target interaction:

```text
agent → memlens_triage { namespace: "payments" }
      ← 3 findings (1 high, 2 medium), 41 Pods healthy, coverage 100%
agent → memlens_explain { pod: "payments/api-7f9c" }
      ← anon growth 1.2 GiB/h, 3 OOMKills in 6h, limit 2 GiB, confidence high
agent → memlens_history { pod: "payments/api-7f9c", since: "6h" }
      ← points and markers confirming steady anon growth after the 14:02 roll-out
```

## What other tools give agents

Checked on 28 November 2026. Each tool below is useful and KubeMemLens
complements rather than replaces it.

| Tool | What it exposes to an agent | Memory depth |
| --- | --- | --- |
| [kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server) (Red Hat) | Fine-grained resource tools (`pods_list`, `pods_top`, `nodes_stats_summary`), `limit`/`continue` paging, byte caps | Metrics-server and kubelet summary numbers; no diagnosis |
| [aks-mcp](https://github.com/Azure/aks-mcp) (Azure) | Broad `call_kubectl` and `call_az`, AKS detectors | No memory-specific tools documented |
| [EKS MCP](https://docs.aws.amazon.com/eks/latest/userguide/eks-mcp-tools.html) (AWS, preview) | Resource tools, CloudWatch metrics such as `memory_rss`, EKS insights | Agent writes its own metric queries |
| [GKE MCP](https://docs.cloud.google.com/kubernetes-engine/docs/reference/mcp/tools_overview) (Google) | Read-only resource, log and event tools | No metrics |
| [k8sgpt](https://github.com/k8sgpt-ai/k8sgpt) | `analyze`, logs, events, troubleshooting prompts | No memory or OOM analyser listed |
| [HolmesGPT](https://github.com/HolmesGPT/holmesgpt), [kagent](https://github.com/kagent-dev/kagent) (CNCF) | Agents that call MCP toolsets | No memory toolset; natural clients for KubeMemLens |
| [kubectl-ai](https://github.com/GoogleCloudPlatform/kubectl-ai) | `kubectl` and `bash` as tools | Whatever the agent can work out |
| [mcp-grafana](https://grafana.com/docs/grafana/latest/ai-tools/mcp/guides/query-metrics-with-prometheus/) | PromQL queries | Whatever is in Prometheus |

The gap: none reads `memory.stat` composition (anonymous, file, kernel),
`memory.events` (high, max, oom, oom_kill), per-cgroup memory PSI or the reason
for an OOM kill, and none returns a diagnosis with confidence and caveats. Both
aks-mcp and kubernetes-mcp-server describe their read-only switches as
guardrails rather than security boundaries; KubeMemLens has no write path at
all, which is a real difference worth keeping.

## Goals and non-goals

Goals:

- An agent answers the common memory questions (who is at risk, why, since
  when, what to check next) in three calls or fewer, with output small enough
  to keep in context.
- Every command has one documented, versioned, schema-validated output shape,
  and every failure has a typed code.
- Findings carry their own meaning: severity, confidence, evidence, caveats and
  the semantics of each number, so the agent does not have to infer them.
- Post-mortem evidence for OOMKills and evictions survives the Pod and a
  collector restart within a documented bound.
- CPU throttling, node pressure and cost appear as further finding kinds in the
  same contract, not as separate tools.

Non-goals, unchanged from the [roadmap](roadmap.md#not-planned):

- No mutation of workloads, no automatic resizing, no patches applied.
- No hosted service, web dashboard, telemetry or outbound calls. The MCP server
  talks only to the agent that started it and to the caller's cluster.
- No embedded model or LLM call inside KubeMemLens. It is the evidence tool the
  agent calls, not an agent itself.
- No logs, tracing or network monitoring.

## Workstreams

### A. Agent interface (new)

| ID | Item | What it delivers |
| --- | --- | --- |
| A1 | Output envelope | One versioned envelope for every structured output: `apiVersion`, `kind`, `schemaVersion`, `generatedAt`, `scope`, `source` (deep, restricted, replay), `coverage`, `warnings`, `truncated`, `data`. Existing explanation and incident schemas sit inside `data` unchanged. |
| A2 | `-o` everywhere | `-o json\|yaml` on `status`, `doctor`, `history pod`, `compare`, `sample`, `session show` and `replay`; `replay`'s legacy export moves to `--export-file`. |
| A3 | Published schemas | JSON Schema (2020-12) files under `schemas/`, embedded in the binary and printed by `kubectl memlens schema [kind]`. CI validates every command's output against them. |
| A4 | Typed errors and exit codes | Structured error object (`code`, `message`, `remedy`, `nextCommand`) on stderr in JSON mode. Codes include `forbidden`, `not-found`, `collector-unavailable`, `stale-evidence`, `unsupported-schema`, `invalid-argument`, `timeout`. Documented exit codes, distinct for usage errors, access errors and "evidence unavailable". |
| A5 | `triage` command | `kubectl memlens triage` over `-n`, `-A`, `--node`, `--workload` or `-l`. Returns findings ranked by severity then risk, each with target, diagnosis, severity, confidence, a one-line summary, the few numbers that justify it, and the next call. Healthy Pods are counted, not listed. Includes coverage (Nodes reporting, stale agents, unmapped cgroups) so the agent knows what it cannot see. |
| A6 | Output budget | `--max-findings`, `--detail summary\|full` and cursor pagination on list outputs. A truncated result says so and gives the call that returns the rest. |
| A7 | Local MCP server | `kubectl memlens mcp` over stdio, using the caller's kubeconfig, context and RBAC, or in-cluster ServiceAccount credentials when run inside a Pod. A handful of high-level, read-only tools backed by the same code paths and schemas as the CLI. See [MCP design notes](#mcp-design-notes). |
| A8 | Semantics delivered with data | A compact `glossary` in the envelope (or an MCP resource) that defines each number used in the response: what `anon`, file cache, working set and `memory.high` events mean, and what they do not prove. |
| A9 | Agent skill and guide | A `SKILL.md` following the [Agent Skills](https://agentskills.io/specification) format and an `AGENTS.md` snippet showing the triage → explain → history → recommend workflow, the meaning of severity and confidence, and what not to conclude. A listing in the MCP Registry once A7 ships (the registry is in preview). An `llms.txt` is optional and low value: no major provider says it reads one. |
| A10 | Untrusted-data hygiene | Treat every tenant-controlled string (names, owner names, node names from labels) as data: control and invisible Unicode characters stripped, length-capped, never placed in instructions or tool descriptions, never placed in suggested commands unless `SafeNames` passes. No labels, annotations, environment, logs or file paths in agent output. A short threat model under `docs/security/` maps this to OWASP MCP06 and MCP10 and Agentic ASI01 and ASI06. |
| A11 | Agent evaluation harness | A set of scripted incidents on a disposable kind cluster (anon leak, cache-heavy, tmpfs fill, OOMKill loop, limit too low, noisy neighbour, throttling once C1 ships) with questions and expected answers. Measures calls, output tokens and correctness for an agent with KubeMemLens against the same agent with `kubectl` alone. Runs on demand, not in CI, and needs the maintainer's own model credentials. |
| A12 | Remote MCP for in-cluster agents | Optional, design-gated. Streamable HTTP served by a separately installed component for agents that run as Pods (kagent, HolmesGPT). Needs its own threat model and ADR before any code; see [Remote MCP](#remote-mcp-a12). |

### B. Evidence that survives

| ID | Item | Issue | What it delivers |
| --- | --- | --- | --- |
| B1 | Collector retention rule | [#319](https://github.com/danushkastanley/KubeMemLens/issues/319), ADR 0033 stage 2 | A documented bound for how long evidence for a removed container stays readable. |
| B2 | Agent replay to a new collector | [#319](https://github.com/danushkastanley/KubeMemLens/issues/319), ADR 0033 stage 3 | Agents keep a bounded window and replay it after a collector restart or Pod replacement. Does not survive an agent restart; that limit is documented. |
| B3 | OOMKill and eviction post-mortems | [#310](https://github.com/danushkastanley/KubeMemLens/issues/310) | Bounded evidence for a terminated container, plus eviction-specific handling (eviction reason, Node pressure at the time). Surfaced by `triage --since` and `explain pod` for a Pod that no longer exists. |

### C. New signals in the same contract

| ID | Item | Issue | What it delivers |
| --- | --- | --- | --- |
| C1 | CPU throttling | [#311](https://github.com/danushkastanley/KubeMemLens/issues/311) | Parsers for `cpu.stat` and `cpu.max`, a throttling diagnosis, and a `cpu-throttled` finding kind in `triage` and `explain`. |
| C2 | Node pressure from PSI | [#312](https://github.com/danushkastanley/KubeMemLens/issues/312) | `cpu.pressure` and `/proc/pressure` readers; `explain node` and `triage --node` rank the Pods that contribute, without claiming causation the evidence does not show. |

### D. Budget

| ID | Item | Issue | What it delivers |
| --- | --- | --- | --- |
| D1 | Right-sizing with cost | [#313](https://github.com/danushkastanley/KubeMemLens/issues/313) | `recommend` adds an estimate of cost difference from prices the operator supplies in a local file. Prices are never fetched. Surfaced to agents through the recommend tool. |

### E. Release and compatibility

| ID | Item | What it delivers |
| --- | --- | --- |
| E1 | Breaking-change list | The final, reviewed list of what 2.0.0 breaks; see [Breaking changes](#breaking-changes). |
| E2 | Migration guide | `docs/migrating-to-2.0.md` with each break, its replacement and a jq one-liner where a script needs updating. |
| E3 | Qualification | The existing kind, terminal, scale and restricted qualifications rerun on the release candidate, plus the A11 evaluation. |
| E4 | Terminal image base | The Ubuntu 24.04 hold ends at 2.0.0; decide the new base in this release. |

## MCP design notes

These are starting positions for the A7 design document, not decisions.

**Protocol.** Target MCP revision 2026-07-28, which removes sessions and the
`initialize` handshake and deprecates Roots, Sampling and Logging. Do not use
the deprecated features: take the kube context and namespace as tool
parameters and write diagnostics to stderr. A newer revision is already in
draft, so the design records the revision it targets.

**Tools.** Few, high-level and question-shaped, rather than one per CLI
command; six to eight is the aim. Each returns `structuredContent` that
validates against the published schema and an `outputSchema`, plus the same
JSON as a text block as the specification recommends. `tools/list` returns
tools in a fixed order. Every tool is annotated `readOnlyHint` and
`idempotentHint` true, `destructiveHint` false. Bad input and unavailable
evidence come back as `isError` results with the A4 code and a corrected
example, so the agent can retry sensibly.

| Tool | Answers | Backed by |
| --- | --- | --- |
| `memlens_triage` | What is wrong in this scope, ranked? | A5 |
| `memlens_explain` | Why is this Pod, workload or Node using this memory? | `explain` |
| `memlens_history` | How did it change, and what happened around then? | `history`, markers, B3 |
| `memlens_recommend` | What should the request and limit be, and what would it cost? | `recommend`, D1 |
| `memlens_compare` | How does this Pod differ from its replicas or from before? | `compare`, `replicas` |
| `memlens_status` | What can I see, how fresh is it, and what is missing? | `status`, `doctor` |

**Resources.** The JSON Schemas, the glossary (A8) and the runbooks under
`docs/runbooks` as read-only resources, so an agent can look up meaning without
another tool call.

**Prompts.** A small set of named workflows such as "diagnose namespace
memory" and "OOMKill post-mortem", matching the agent skill.

**Transport and identity.** Stdio only for A7. The specification says stdio
servers take credentials from the environment rather than OAuth, which matches
a kubeconfig. The server runs as the user, adds no permission the user lacks
and holds no credential of its own. It never forwards tokens to another
service.

**Handles.** Without sessions, anything that spans calls (an incident session,
a capture, a pagination cursor) is an explicit handle the agent passes back.
The caller's RBAC is checked again on every use; holding a handle grants
nothing. Long captures may use the optional Tasks extension if the SDK
supports it.

**Output size.** Defaults tuned for context windows: summary detail, a finding
cap, and pagination with a cursor. Truncation is explicit and actionable.

**Untrusted content.** Tool descriptions are static strings. Tenant-controlled
values appear only in typed fields, never in free text that could read as an
instruction (A10). This is not hypothetical: CVE-2026-47250, in an unrelated
Kubernetes MCP server, chained instructions hidden in Pod logs with kubectl
flag injection to leak a bearer token.

**Field names.** Where a value matches an OpenTelemetry Kubernetes metric
(`k8s.pod.memory.working_set`, `rss`, `usage`), name and define it the same way
so agents that know OpenTelemetry read it correctly. PSI, `memory.events` and
OOM-kill reasons have no OpenTelemetry equivalent yet and keep KubeMemLens
names.

**SDK.** Prefer the official [Go SDK](https://github.com/modelcontextprotocol/go-sdk):
`v1.0.0` promised API stability, `v1.7.0` added 2026-07-28 support (stateless
mode is needed for the new revision over HTTP), and `v1.8.0` is the latest
stable release. The design records the version and the removal path. A new dependency needs the
justification [CONTRIBUTING.md](../CONTRIBUTING.md) asks for: maintenance,
licence, transitive footprint, security history and removal path.

### Remote MCP (A12)

An in-cluster agent can already run `kubectl memlens mcp` inside its own Pod
with a ServiceAccount, which needs no new KubeMemLens component. A served HTTP
endpoint is only worth it if that proves insufficient. If it goes ahead it must
act as an OAuth 2.1 resource server (Protected Resource Metadata, audience-bound
tokens), and the specification forbids passing the caller's token on to the
Kubernetes API. Reading as the caller would then need token exchange or
impersonation, and impersonation is a much larger privilege than KubeMemLens
holds today. It must also stay outside the default chart and pass a threat
model review. It does not block 2.0.0.

## Breaking changes

Candidates; each needs a decision before E1 closes.

| Change | Why | Who notices |
| --- | --- | --- |
| `top`, `status`, `doctor` JSON move into the A1 envelope | Bare, unversioned output cannot evolve safely | Scripts that parse those outputs |
| `replay -o` stops meaning "legacy export file" | `-o` means output format everywhere else | Anyone exporting legacy schema 1 or 2 |
| `--sort-by rss` alias removed | `anon` replaced it; one name per field | Scripts using the old name |
| Error output in JSON mode becomes structured | A4 | Scripts that match error text |
| Exit codes beyond 0 and 1 | A4 | Scripts that treat any non-zero as one failure |
| Aggregated API `v1alpha1` to `v1beta1` | Signals contract stability | Direct API callers; the CLI negotiates both during one release |

Explanation, incident and snapshot schemas keep their own version numbers and
negotiation. They are not broken by 2.0.0.

## Phases

Each phase ends merged on `main`, verified on a real path and documented. A
phase that only adds behaviour can ship as a `1.x` minor if the maintainer
wants it out sooner; breaking items wait for `2.0.0`.

| Phase | Contents | Depends on | Done when |
| --- | --- | --- | --- |
| 0. Designs | Output contract ADR (A1, A4), `triage` design (A5), MCP design (A7, A10) | — | The three documents are reviewed and merged |
| 1. Contract | A1, A2, A3, A4, A6 | 0 | Every command's JSON validates against a published schema in CI; error codes documented |
| 2. Triage | A5, A8 | 1 | `triage -n` on the kind harness ranks the seeded incidents correctly with coverage reported |
| 3. Agents | A7, A9, A10, A11 baseline | 2 | An agent using only the MCP server answers the A11 questions; baseline numbers recorded |
| 4. Survival | B1, B2, B3 | 1 | A collector restart and a Pod replacement on kind keep post-mortem evidence within the bound |
| 5. Signals | C1, C2 | 2 | Throttled and pressure fixtures appear as findings in `triage`, `explain` and the TUI |
| 6. Budget | D1 | 2 | The recommendation corpus has cost cases and passes |
| 7. Release | E1 to E4, A11 rerun | 1 to 6 | Release gates in [release process](release-process.md) pass for `v2.0.0` |

Phases 4, 5 and 6 can run in parallel once their dependency is merged. A12 is
outside the critical path.

## Tracking on GitHub

Status lives in GitHub, not on this page. The tracking issue
[#450](https://github.com/danushkastanley/KubeMemLens/issues/450) lists every item as a sub-issue, the
[v2.0.0 milestone](https://github.com/danushkastanley/KubeMemLens/milestone/1)
holds them all, and each issue records what it is blocked by and what
"done" means. This page keeps the reasoning behind the plan.

| ID | Item | Issue | Phase |
| --- | --- | --- | --- |
| A1 | Output envelope | [#309](https://github.com/danushkastanley/KubeMemLens/issues/309) | 1 |
| A2 | `-o` on every command | [#451](https://github.com/danushkastanley/KubeMemLens/issues/451) | 1 |
| A3 | Published JSON Schemas | [#452](https://github.com/danushkastanley/KubeMemLens/issues/452) | 1 |
| A4 | Typed errors and exit codes | [#453](https://github.com/danushkastanley/KubeMemLens/issues/453) | 1 |
| A5 | `triage` command | [#455](https://github.com/danushkastanley/KubeMemLens/issues/455) | 2 |
| A6 | Output budget and pagination | [#454](https://github.com/danushkastanley/KubeMemLens/issues/454) | 1 |
| A7 | Local MCP server | [#458](https://github.com/danushkastanley/KubeMemLens/issues/458) | 3 |
| A8 | Glossary with data | [#456](https://github.com/danushkastanley/KubeMemLens/issues/456) | 2 |
| A9 | Agent skill and guide | [#459](https://github.com/danushkastanley/KubeMemLens/issues/459) | 3 |
| A10 | Untrusted-data hygiene | [#457](https://github.com/danushkastanley/KubeMemLens/issues/457) | 3 |
| A11 | Agent evaluation harness | [#460](https://github.com/danushkastanley/KubeMemLens/issues/460) | 3, 7 |
| A12 | Remote MCP | [#461](https://github.com/danushkastanley/KubeMemLens/issues/461) | — |
| B1 | Collector retention rule | [#462](https://github.com/danushkastanley/KubeMemLens/issues/462) | 4 |
| B2 | Agent replay to a new collector | [#319](https://github.com/danushkastanley/KubeMemLens/issues/319) | 4 |
| B3 | OOMKill and eviction post-mortems | [#310](https://github.com/danushkastanley/KubeMemLens/issues/310) | 4 |
| C1 | CPU throttling | [#311](https://github.com/danushkastanley/KubeMemLens/issues/311) | 5 |
| C2 | Node pressure from PSI | [#312](https://github.com/danushkastanley/KubeMemLens/issues/312) | 5 |
| D1 | Right-sizing with cost | [#313](https://github.com/danushkastanley/KubeMemLens/issues/313) | 6 |
| E1 | Breaking-change list | [#463](https://github.com/danushkastanley/KubeMemLens/issues/463) | 7 |
| E2 | Migration guide | [#464](https://github.com/danushkastanley/KubeMemLens/issues/464) | 7 |
| E3 | Qualification | [#465](https://github.com/danushkastanley/KubeMemLens/issues/465) | 7 |
| E4 | Terminal image base | [#466](https://github.com/danushkastanley/KubeMemLens/issues/466) | 7 |

Outside 2.0.0:

- [#315](https://github.com/danushkastanley/KubeMemLens/issues/315) `go install`
  stays parked because the repository and module names stay as they are.
- [#324](https://github.com/danushkastanley/KubeMemLens/issues/324), the audit
  tracker, closes when the issues above close.

## How we will know it worked

- **Calls:** in the A11 scenarios, an agent with KubeMemLens reaches a correct
  diagnosis in three tool calls or fewer, against the measured `kubectl`-only
  baseline.
- **Context:** a default `triage` response for a namespace of 500 Pods stays
  within a fixed token budget, set in the triage design and checked by a test.
  For scale, Claude Code caps one tool response at 25,000 tokens by default;
  the budget should be a small fraction of that.
- **Correctness:** no A11 answer confuses file cache with a leak or working set
  with RSS.
- **Permissions:** no scenario needs `pods/exec`, `nodes/proxy` or any write
  permission.
- **Contract:** every structured output validates against its published schema
  in CI.

## Decisions needed

1. Is `v2.0.0` one release at the end, or do additive phases ship early as
   `1.x` minors? This page assumes phases merge to `main` in order and the
   maintainer chooses per phase.
2. Which [breaking changes](#breaking-changes) to accept.
3. Whether `triage` is the right name (`diagnose` is the alternative).
4. Whether A12 (remote MCP) is in scope for 2.0.0 or a later release.

These are also listed on [#450](https://github.com/danushkastanley/KubeMemLens/issues/450).

## Sources

Read on 28 November 2026. Recheck before each design document, because the MCP
specification and SDK move quickly.

- MCP specification 2026-07-28: [changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog),
  [tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools),
  [authorisation](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization),
  [Tasks extension](https://modelcontextprotocol.io/extensions/tasks/overview),
  [security best practices](https://modelcontextprotocol.io/docs/tutorials/security/security_best_practices),
  [registry](https://modelcontextprotocol.io/registry/about).
- [MCP Go SDK releases](https://github.com/modelcontextprotocol/go-sdk/releases).
- Anthropic, [Writing effective tools for agents](https://www.anthropic.com/engineering/writing-tools-for-agents):
  consolidate workflows into tools, concise and detailed response formats,
  truncation with guidance, actionable errors, evaluation-driven design.
- OpenAI, [function calling guide](https://developers.openai.com/api/docs/guides/function-calling):
  keep the tool set small and use enums to rule out invalid input.
- [Agent Skills specification](https://agentskills.io/specification).
- [OWASP MCP Top 10](https://owasp.org/www-project-mcp-top-10/) (beta) and
  [OWASP Top 10 for Agentic Applications 2026](https://genai.owasp.org/2025/12/09/owasp-top-10-for-agentic-applications-the-benchmark-for-agentic-security-in-the-age-of-autonomous-ai/).
- [CVE-2026-47250](https://osv.dev/vulnerability/CVE-2026-47250).
- OpenTelemetry [Kubernetes metrics](https://opentelemetry.io/docs/specs/semconv/system/k8s-metrics/)
  and [container metrics](https://opentelemetry.io/docs/specs/semconv/system/container-metrics).
- The tools in [What other tools give agents](#what-other-tools-give-agents),
  linked from that table.
