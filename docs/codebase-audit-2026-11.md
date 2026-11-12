# Codebase audit and implementation plan (November 2026)

Audit of `main` at `620a160` (tags `v1.0.4` and `v1.0.4-rc.1`), carried out on
12 November 2026. It lists the defects to fix, the improvements worth making,
the features to add, an assessment of the test suite, and a phased
implementation plan with the tests for each change.

Nothing in the repository was changed by the audit apart from adding this file.

## 1. Summary

- **The codebase is in good shape.** Build, `go vet` and the full Go suite pass
  (85 packages, 0 failures; `GOOS=linux` build and vet are clean). No
  authentication bypass, tenant-scope leak or data race was found. Supply-chain
  posture (SHA-pinned actions, digest-pinned images, scratch non-root image) is
  solid.
- **There are 10 defects worth a `v1.0.5` patch.** Two can take the collector
  down or blind monitoring (B-01, B-02), four make a healthy node report
  nothing or report degraded (B-03, B-06, B-07, B-08), and four give the user a
  wrong answer during an incident (B-04, B-05, B-09, B-10).
- **The test suite is not too large.** Test-to-code ratio is 0.67, coverage is
  80.9% cross-package, there are no duplicated test bodies and no
  assertion-free tests. About 200–250 test lines (0.5%) are removable. The
  waste is in how CI runs the suite, and the larger gap is tests that are
  missing. See section 6.
- **The largest structural question is the trace extension.** It is about a
  third of the Go code, its server is never published, and the released CLI
  ships six `trace` commands that cannot succeed. See I-12 and decision D-1.

## 2. Method and confidence

Six parallel reviews covered the server, the node agent, the CLI/TUI, the trace
subsystem, the test suite, and packaging/CI/docs. Each finding carries one of
these labels:

| Label | Meaning |
| --- | --- |
| **Run** | Reproduced by running code, a throwaway test (`go test -overlay`, outside the repo) or `helm template`. |
| **Read** | Confirmed by reading the code path and its callers; not executed. |
| **Plausible** | Consistent with the code but depends on a condition not reproduced (for example a kernel setting or a multi-node clock). |

Findings marked ✔ were additionally re-checked against the source during
consolidation. Line numbers refer to commit `620a160`.

Not done: no cluster, kind, network or privileged run. Linux-only tests
(`prototype/trace` supervisor, worker, eBPF) did not run on the macOS host.
`internal/volumecontext`, `volumehealth`, `memorytopology` and parts of
`internal/kube` were read in part only.

## 3. Defects

### 3.1 Priority 1: fix in `v1.0.5`

| ID | Defect | Where | Evidence |
| --- | --- | --- | --- |
| B-01 | A 4 MiB cgroup snapshot can allocate several GiB before any bound is checked | `internal/extension/handler.go:295-307`, `internal/extension/coordinator.go:117-121`, `internal/collector/server.go:210-223` | Run ✔ |
| B-02 | Metrics resource returns 507 for roughly 1,000–1,920 Pods at chart defaults | `internal/extension/read_handler.go:310-314`, `internal/collector/metrics_view.go:12-15,137-143` | Run ✔ |
| B-03 | An unreadable `memory.pressure` discards every container on the node | `internal/cgroup/signals.go:74-91` | Run (code path) ✔; kernel trigger Plausible |
| B-04 | Pod diagnosis hides a container that is at its own limit | `internal/explain/explain.go:72`, `internal/model/aggregate.go:33-35` | Run ✔ |
| B-05 | Replay and capture comparison recompute the diagnosis against the current time | `internal/cli/explain.go:149`, `internal/cli/replay.go:85,90`, `internal/cli/compare.go:243` | Run ✔ |
| B-06 | A container exiting mid-scan withholds the whole node snapshot | `internal/cgroup/walker.go:56-59,83-87`, `cmd/memlens-agent/reliability.go:9-11` | Read ✔ |
| B-07 | Owner resolution spends the scan timeout on dense nodes | `cmd/memlens-agent/main.go:173-201`, `internal/kube/owner_resolver.go:137-147` | Run |
| B-08 | Sandbox of a Pod with no running container marks the cluster degraded | `internal/agent/scanner.go:138-150,187-211` | Run |
| B-09 | `doctor` passes agent coverage when nodes have never reported | `internal/cli/doctor.go:98-118` | Read ✔ |
| B-10 | A crafted schema 1 or 2 incident bundle injects terminal escapes on replay | `internal/api/incident_validation.go:9-33`, `internal/incident/document.go:93-103` | Run |

**B-01. Unbounded allocation on ingest.** Only the Node-context producer gets
the bounded pre-scan. A cgroup agent's body is decoded into typed structs and
re-marshalled before `ValidateSnapshot` checks the container count. A body of
empty container objects fits about 1.4 million entries in 4 MiB; measured cost
was a 2.4 GiB live slice and a 3.4 GiB canonical buffer. One request from any
holder of an agent token (or anyone who can reach `:8081` in legacy mode) can
OOM-kill the collector, which resets all history.
*Fix:* run a streaming token pre-scan that rejects when `snapshot.containers`
exceeds `MaxContainers`, for both roles and the legacy decoder, and move the
count check ahead of canonicalisation in `Accept`.
*Tests:* `internal/extension/handler_test.go` posts a 4 MiB body of empty
containers as the cgroup agent and asserts 400 plus a `TotalAlloc` delta under
64 MiB; mirror it in `internal/collector/server_test.go`; add a fuzz target for
the pre-scan.

**B-02. Metrics budget mismatch.** The entity limit is derived from the full
16 MiB response budget (1,920 entities at an assumed 8 KiB), but the renderer
is capped at `nestedReadBudget()` (8 MiB) and a realistic Pod costs about
8.6 KiB. Between about 1,000 and 1,920 Pods the whole resource returns 507, so
the scraper loses every series including collector state. Above 1,920 the Pod
level is dropped and it works again.
*Fix:* compute the limit from the bound the renderer enforces; on
`ErrOutputTooLarge` re-render with the Pod level (then namespace level) dropped
and emit a dropped-entities sample.
*Tests:* `internal/extension/read_handler_test.go` seeds 1,500 realistic Pods
and asserts 200 with either Pod series or the dropped-entities sample. Fix
`TestMetricsViewRendersWithinBudget`, which never sets `MaxBytes`.

**B-03. Pressure read failure is fatal.** Any `memory.pressure` read error
other than "not found" fails the container parse. Where PSI is compiled in but
disabled at boot the read returns `EOPNOTSUPP`, every container fails and the
node reports `scan_failed` permanently. `io.pressure` in the same package
already degrades, under a comment that says enrichment failure must not discard
a valid sample.
*Fix:* on a read error leave `PressureKnown=false` and continue. Reject
non-finite PSI averages at the same time (a `NaN` currently fails JSON
marshalling of the whole publish).
*Tests:* `internal/cgroup/signals_test.go` with an `fs.FS` returning
`syscall.EOPNOTSUPP`; extend `parser_fuzz_test.go` for `NaN`/`Inf`.

**B-04. Pod-level limit risk.** The Pod figure is the sum of container limits,
but the kernel kills at each container's `memory.max`. A Pod with `app` at
99 MiB of 100 MiB and a sidecar at 100 MiB of 1 GiB reports `normal`, 18%.
Two related aggregation errors share the code: one known limit plus one unknown
reports "150% of memory.max", and a member with `memory.events.local` masks a
hierarchical `oom_kill` delta from another.
*Fix:* in `AnalyzePodAt` evaluate limit risk per container and promote the
worst, naming it; treat the Pod limit as known only when every container's is;
stop `LocalEventsKnown` on one member masking another's deltas.
*Tests:* `internal/explain/explain_test.go` (two-container case returns
`limit-risk` naming `app`; partial-known case does not);
`internal/model/aggregate_test.go` (mixed-source sum keeps `HasOOMRisk()`).

**B-05. Replay uses now.** A capture whose Pod was OOMKilled two minutes
before `capturedAt` replays two days later as `normal`, because the 15-minute
recency window is measured from `time.Now()`. `compare` prints the same wrong
diagnosis. This contradicts the deterministic-replay promise in
`docs/restricted-incidents.md`.
*Fix:* pass `bundle.CapturedAt` to `explain.AnalyzePodAt` in replay and
compare, and base age text on the capture time.
*Tests:* `internal/cli/replay_test.go` with a fixture far in the past asserts
`oom-risk` and `critical`; a matching `compare` case.

**B-06. Vanished cgroups block publishing.** `hasMemoryFiles` stats, then
`ParseDirectory` reads about 14 files; a container exiting in between returns
"not found", which sets `WalkError` and withholds the whole snapshot as
`partial_scan_not_published`. A directory vanishing during the walk does the
same, including unrelated systemd scopes.
*Fix:* treat `fs.ErrNotExist` and `ENODEV` as "cgroup gone" and skip without
recording an error; make `CgroupReadErrors` a real count.
*Tests:* `internal/cgroup/walker_test.go` with a parse seam that removes the
directory before parsing; assert the other entries return with a nil error.

**B-07. Owner resolution starves the scan.** `ResolveIndex` and the node GET
run on the same 10 s `scanCtx` as the cgroup walk, at client-go's default
5 QPS, with no negative cache and one shared expiry. Measured with 100 distinct
ReplicaSet owners: 9.8 s spent, 41 errors, 200 ms left for the walk, repeating
at each 5-minute expiry.
*Fix:* give enrichment its own budget (about 2 s), add a short negative cache,
jitter expiry, and serve stale on error.
*Tests:* `internal/kube/owner_resolver_test.go` (second call inside the
negative TTL issues no GETs; expiries differ); an agent main-loop test that the
scanner keeps its full timeout when enrichment is slow.

**B-08. Lone sandbox counted as unmapped.** A Pod in `ImagePullBackOff` or
`CreateContainerConfigError` has a live sandbox cgroup and no container IDs, so
the sandbox is emitted as an unmapped container and the collector marks the
node partial and the cluster `degraded` for as long as the Pod exists.
*Fix:* index all Pods on the node by UID, including those without container
IDs, and classify a lone unmapped cgroup under a known Pod with no
expected-running containers as infrastructure.
*Tests:* `internal/agent/scanner_test.go` (one Waiting container, one sandbox:
`InfrastructureCgroups==1`, `Unmapped==0`); keep
`TestScannerDoesNotHideUnknownCgroupWhenRunningSiblingIsMissing` passing.

**B-09. `doctor` ignores missing nodes.** Coverage counts only nodes that have
reported, so a crash-looping or unscheduled agent is invisible and `doctor`
exits 0 even with `--strict`. The collector already computes `ExpectedNodes`
and `MissingNodes`, and `status` prints them.
*Fix:* fail or warn when `Reliability.MissingNodes > 0` or state is not ready.
*Tests:* `internal/cli/doctor_test.go` with three fresh nodes and
`MissingNodes: 1` asserts a non-pass check and non-zero exit.

**B-10. Escape injection on replay.** Schema 3 rejects control characters;
schemas 1 and 2 check only version and resource context, and names are printed
raw. Incident files are shared in tickets, so they are untrusted input.
*Fix:* in `ValidateIncidentSchema` reject control and format runes in every
name, reason and caveat string.
*Tests:* `internal/incident` test asserting `Read` fails for `\x1b` in
`podName`; add the case to the incident fuzz seeds.

### 3.2 Priority 2: fix in the following patch

| ID | Defect | Where | Evidence | Fix | Test |
| --- | --- | --- | --- | --- | --- |
| B-11 | Empty namespace renders "collector rebuilding"; hint says `go run ./cmd/kubectl-memlens status` | `internal/tui/observations.go:26-34`, `internal/tui/render.go:303-315` | Run ✔ | Show "No Pods in namespace X"; do not infer `rebuilding` from an empty scoped list; correct the hint | `internal/tui/model_integration_test.go`: empty successful fetch contains neither string |
| B-12 | Kubeconfig and context errors are swallowed as "request-failed" or "invalid response" | `internal/client/options.go:198-202`, `internal/client/evidence.go:41-46,110-128` | Run | Return `kube.BuildConfig` errors unchanged; append the underlying error | CLI test with an empty kubeconfig and unknown context |
| B-13 | Every read copies every container record; docs say shards are scanned without copying | `internal/collector/scoped_page.go:180-198`, `internal/collector/store.go:208-209` | Run | Return shard views over the immutable slices | `internal/collector/scale_test.go`: single-Pod read at 100,000 containers allocates under 1 MiB |
| B-14 | Global read gate is held while the response is written, and the body is encoded twice | `internal/extension/read_handler.go:90-98,400-413` | Read ✔ | Encode once into a bounded buffer, release the gate, then write | Blocking `ResponseWriter` test: second request completes first |
| B-15 | Per-node rate limiter makes the agent's third and fourth retries certain to fail | `internal/extension/handler.go:266-271,394-421`, `internal/agent/retry.go:18-24` | Read | Charge after admission, or burst ≥ attempts; key per producer instance | Handler test: fourth post within one second is not `rate_limited` |
| B-16 | `helm install -n <other>` renders into `kube-memlens` while Helm creates `<other>` | `charts/kube-memlens/values.yaml:10-12`, all templates | Run | `fail` on mismatch, or default `namespace.name` to the release namespace | CI: `helm template -n other` fails clearly or renders into `other` |
| B-17 | Serving CA and key are regenerated on every client-side render (Argo CD, `helm template`) | `charts/kube-memlens/templates/extension-tls.yaml:3-22` | Run | Document "Helm CLI only" now; move generation to `memlens-cert-bootstrap` later | Render twice and diff |
| B-18 | Chart schema accepts four combinations that break at runtime | `values.schema.json`, `templates/deployment.yaml:52-54`, `templates/prometheusrule.yaml:45-46` | Run | Four `fail` guards (rotate ≥ validity, port clash, TTL < interval, rules without metrics) | Add each to CI "Reject values outside the chart schema" |
| B-19 | `docs/roadmap.md` says the current release is `v1.0.3`, and claims machine-readable `history`; README says "No eBPF attribution yet" while `trace` ships | `docs/roadmap.md:4,35,39,41`, `README.md:603,613` | Read ✔ | Correct the text; add a roadmap version assertion to `hack/check-release-contract.sh` | `make check-release-contract` fails on the old file |
| B-20 | Ctrl+C and `q` are ignored in filter input and action screens | `internal/tui/model.go:303-324`, `internal/tui/action_model.go:46-102` | Run ✔ | Handle `ctrl+c` once before mode dispatch | TUI test across search, capture-path and result modes |
| B-21 | `top` prints "unknown" for an unlimited limit and omits it from JSON | `internal/cli/top_rows.go:81-86,134-139` | Read ✔ | Carry a limit state (`unknown`, `unlimited`, `finite`) | `internal/cli/top_test.go` |
| B-22 | Deep `top --watch` exits on the first transient error; writes clear-screen to non-terminals | `internal/cli/top_options.go:91-111` | Read | Reuse restricted-mode behaviour | Render function fails once then succeeds; watch continues |
| B-23 | `status -o json` rejects `-o`; `--sort-by anon` rejected; `explain` still prints an `RSS` header | `internal/cli/status.go:77`, `internal/cli/top_options.go:61,83`, `internal/cli/explain.go:129,155` | Run ✔ (`-o`) | Add shorthand, alias and rename | One table-driven CLI test |
| B-24 | Container ID extraction lets an ancestor scope win on nested runtimes | `internal/cgroup/walker.go:67,117-146` | Run | Extract relative to root, below the last `pod<uid>` component | `walker_test.go`: nested path and Docker-in-Docker path |
| B-25 | Missing `kernel` key (Linux before 5.18) is reported as zero kernel memory | `internal/cgroup/parser.go:81`, `internal/model/memory.go:126-128` | Read | Add `KernelKnown` | `parser_test.go` |
| B-26 | Node-context failure is cached for the full 30 s TTL, including an empty node UID | `internal/kube/node_context.go:34-51` | Read | Retry after about 5 s on error | `node_context_test.go` |
| B-27 | Withheld publishes are invisible in agent metrics | `cmd/memlens-agent/main.go:203-224`, `internal/agent/telemetry.go:31-57` | Read ✔ | Add `kubememlens_agent_snapshots_withheld_total{reason}` | Telemetry render test plus main-loop test |
| B-28 | `prototype/trace/releasebundle` test fails on any host not at the pinned Go version | `prototype/trace/releasebundle/executable_test.go:11-33` | Run | Skip or derive from one shared constant | The fix is the test |
| B-29 | `go build ./...` fails on macOS (`main` declared only on Linux) | `hack/ebpf-active-qualification/observer/main_linux.go` | Run ✔ | Add a non-Linux stub `main` | `go build ./...` on darwin in CI or a `make` target |

### 3.3 Priority 3: low severity

| ID | Defect | Where | Evidence |
| --- | --- | --- | --- |
| B-30 | Counter families are named with `_total` under an OpenMetrics content type | `internal/metrics/render.go:118,125`, `operational.go:28-45` | Read |
| B-31 | `transitionedAt` records when someone read the state, not when it changed | `internal/collector/reliability.go:71-74` | Read |
| B-32 | `trends-context` runs for 12 s inside a 10 s server timeout | `internal/extension/memory_history_read.go:97-100` | Read |
| B-33 | Same `capturedAt` with different content is reported as accepted and discarded | `internal/collector/store.go:125-127` | Read |
| B-34 | Pod and container series can collide because Pod UID is not a label | `internal/collector/metrics_view.go:96-111` | Plausible |
| B-35 | History times print in unlabelled local time | `internal/cli/history.go:86` | Read |
| B-36 | `oomRisk` always labels signals `memory.events`; `compare` shows unknown PSI as 0.00% | `internal/explain/explain.go:206-209`, `internal/cli/compare.go:238` | Read |
| B-37 | Chart renders agent RBAC when `agent.enabled=false`; collector token lifetime reuses the agent value | `templates/serviceaccount.yaml:1-6`, `templates/deployment.yaml:214` | Run |
| B-38 | Three unreferenced scripts; `.gitignore` does not cover `*.exe` | `hack/node-qualification/check_public_transfer.py`, `hack/verify-evidence-discovery-kind.sh`, `hack/test_format_go.py` | Run |

### 3.4 Trace extension (unpublished)

These affect only the separately built trace server, so they do not block a
product patch. All are Read; Linux-only tests could not run on the audit host.

| ID | Defect | Where |
| --- | --- | --- |
| T-01 | Events in the worker's 5 ms buffer at natural expiry discard the verified final counts | `prototype/trace/worker/outputbuffer/writer.go:15,57`, `workerprocess/output_linux.go:18-30` |
| T-02 | Zero tolerance for controller/node clock skew fails admission as "target changed" | `internal/traceadmission/reservations.go:32`, `prototype/trace/nodebinding/service.go:158-159`, `activation.go:47` |
| T-03 | A non-UTF-8 file name aborts a confirmed-paths trace | `prototype/trace/filecache/records.go:84` |
| T-04 | Requests whose bounds cannot run are admitted, then fail as `engine_failed` | `internal/traceadmission/request.go`, `prototype/trace/worker/sdk/clock_linux.go:22` |
| T-05 | One global admission limiter is shared by all principals and verbs, including cancel | `prototype/trace/admissionapi/handler.go:29,44` |

## 4. Improvements

| ID | Improvement | Value | Notes |
| --- | --- | --- | --- |
| I-01 | Cache the TUI's sorted, analysed Pod list per data/query/sort change | High | Measured 302 ms per keypress at 5,000 Pods in the wide layout; the comparator re-runs `explain.AnalyzePod` on every comparison (`internal/tui/view_data.go:29-52`) |
| I-02 | Scheduled certificate rotation and an expiry gauge | High | Rotation runs only as a Helm hook; an install not upgraded for a year loses the aggregated API |
| I-03 | Default collector `-ingestion-mode` to `authenticated` | Medium | Binary default is `legacy` (`cmd/memlens-collector/main.go:39`); the chart overrides it |
| I-04 | Require explicit opt-in for the legacy plaintext client path | Medium | `MEMLENS_COLLECTOR_URL` silently switches to unauthenticated HTTP; legacy readers decode without a size limit |
| I-05 | Agent DaemonSet: probes, `updateStrategy`, `priorityClassName`, `imagePullSecrets`, `GOMEMLIMIT` | Medium | The agent is Burstable and evictable under the pressure it exists to diagnose |
| I-06 | Restrict the cgroup walk to kubelet subtrees; bounded, rooted reads | Medium | Removes exposure to unrelated systemd churn; `topologysource` already uses `os.Root` |
| I-07 | Default `-n` to the kubeconfig context namespace; add `--request-timeout` | Medium | Every command hard-codes `default`; the 5 s timeout has no flag |
| I-08 | Validate characters in agent-supplied names; expose a clock-skew gauge; return 410 for stale continue tokens | Medium | Server hardening H4–H6 |
| I-09 | Signal handling for capture; fall back when hard links are unavailable; refuse private exports to a terminal | Low | `internal/incident/writer.go:45-51,80` |
| I-10 | Document the Pod Security level the agent namespace needs; add a chart icon | Low | hostPath needs `privileged` PSA |
| I-11 | Extend Dependabot to the two prototype modules, seven other Dockerfiles and embedded digests | Medium | `.github/dependabot.yml` covers `/` only |
| I-12 | Decide the future of `prototype/trace` and act on it | High | 27.8k non-test Go lines (32%) for a server that is never published. Options in D-1 |
| I-13 | Reduce per-PR CI cost (see section 6.3) | Medium | "Go checks" is 16 min 25 s against a 20-minute timeout |
| I-14 | Align `make check` with CI | Low | `make check` omits two contract checks and uses a different formatter check from CI |

## 5. New features

### 5.1 Roadmap items and existing groundwork

| ID | Release | Groundwork present | Missing |
| --- | --- | --- | --- |
| N-01 | `v1.1.0` versioned JSON, schemas, MCP server | `schemaVersion` across incident, CLI and API types; `docs/explanation-schema.md` | Published JSON Schema files; `-o` on `status`, `history pod`, `compare`, `sample`, `session show`, `replay`; any MCP code |
| N-02 | `v1.2.0` OOMKill and eviction post-mortems | `oom_kill` parsing, `OOMKillEventsDelta`, last-termination state, capture and replay | Retention after the cgroup is gone (collector is memory-only); eviction handling |
| N-03 | `v1.3.0` CPU throttling | Cgroup walker and agent pipeline only | Parser for `cpu.stat`/`cpu.max`, model, API fields, diagnosis, UI |
| N-04 | `v1.4.0` PSI node pressure | `memory.pressure` and `io.pressure` parsing, `internal/nodeanalysis`, `explain node --rank` | `cpu.pressure` and `/proc/pressure` readers |
| N-05 | `v1.5.0` right-sizing with cost | `internal/recommend`, CI-gated evaluation corpus | Price and cost model; persisted history beyond the optional remote adapter |

### 5.2 Gaps the roadmap does not list

| ID | Feature | Value | Evidence |
| --- | --- | --- | --- |
| N-06 | Publish the Krew plugin; Homebrew and Scoop; Artifact Hub listing | High | `deploy/krew/memlens.yaml.tmpl` exists and is CI-tested but unpublished (`README.md:45`) |
| N-07 | Support `go install` | Medium | Module path differs from the repository name (`go.mod:1`) |
| N-08 | Worst-container limit in `top pods` and the TUI, for example `18% (app 99%)` | High | User-visible half of B-04 |
| N-09 | Effective Pod-level limit and Pod-scoped OOM events from the parent Pod cgroup | Medium | Containers under pod-level resources currently show unlimited |
| N-10 | Clear message on cgroup v1 nodes | Medium | No version detection in the standard agent; a v1 node probably appears as low mapping coverage (Plausible) |
| N-11 | Collector persistence or warm restart | Medium | Every restart loses snapshots, baselines and history; prerequisite for N-02 |
| N-12 | GitOps-safe TLS (bootstrap owns the Secret and `caBundle`) | Medium | Structural fix for B-17 |
| N-13 | Per-Pod HugeTLB accounting; surface parsed-but-dropped `zswap` and `oom_group_kill` | Low | HugeTLB exists only as optional node topology |
| N-14 | Alias names in redacted schema 1/2 captures | Medium | They keep real names while declaring `redacted: true` |
| N-15 | Feedback for unknown TUI filter values; filter by the same diagnosis the row shows | Low | `internal/tui/filter_spec.go:21-47`, `filter.go:92-100` |

## 6. Test suite assessment

### 6.1 Verdict

**Keep the suite.** It is proportionate for a multi-tenant tool that reads node
memory and enforces authorisation. Mass deletion would remove security
invariants and save under a minute of CI.

| Measure | Value |
| --- | --- |
| Go lines, main module (non-test / test) | 66,475 / 44,532 (ratio 0.67) |
| Go lines, `prototype/trace` (non-test / test) | 20,025 / 16,500 (ratio 0.82) |
| Test functions, main module | 1,308 tests, 12 benchmarks, 30 fuzz targets |
| Statement coverage, cross-package | 80.9% (73.9% per-package, as CI reports) |
| Exact-duplicate test bodies | 0 |
| Near-duplicate pairs in the same package | 8 pairs, 209 lines (0.5% of test lines) |
| Tests with no assertion | 0 (two "budget" tests only log elapsed time) |
| Python harness tests | 182 files, 17,904 lines, 953 cases, about 52 s in CI |
| Summed Go package time: plain / coverage / race | 45 s / 55 s / 196 s locally |

The 603 test files are a symptom of the small-file rule, not over-testing:
test files average 106 lines against 108 for source files, about 3.1 test
functions each, split by behaviour.

### 6.2 What to remove or merge (about 200–250 lines)

| Action | Files | Lines |
| --- | --- | --- |
| Fold the CronJob cohort test into the sequence test that already covers it; make the two "FitsBudgets" tests assert a bound | `internal/kube/change_marker_limits_test.go:15,97` | ~12 |
| Table-drive older tests | `internal/collector/server_test.go:166-378`, `internal/client/factory_test.go:10-78`, `internal/client/collector_test.go:38-120`, `internal/cgroup/parser_test.go:10-52`, `internal/collector/scoped_view_test.go:179-227` | ~145 |
| Delete trivial tests | `cmd/memlens-collector/main_test.go:64` (relation between two constants), `internal/incidentsessionapi/markers_test.go:13` (JSON round trip) | ~40 |
| Delete the test of an unreachable branch with the branch | `internal/kube/mapper_test.go:184`, `internal/kube/mapper.go:217-223` | ~20 |
| Move `aggregateWorkloads` tests to `internal/aggregate`, which has none | `internal/collector/workloads_test.go` | 0 |

Where the proportion is genuinely off is the trace area: about 1,370 test lines
and four benchmarks cover a confirmed-paths fast path that no shipped client
can request, and the qualification harnesses for a rejected candidate are
compiled and tested on every PR. That is settled by decision D-1, not by
trimming tests.

### 6.3 CI cost (the real waste)

| Action | Saving |
| --- | --- |
| Do not run the single-threaded 128-node capacity fill under `-race` (`internal/collector/volume_capacity_test.go:15`): 33.8 s locally, `internal/collector` is 111.6 s of the 322 s race step | 1–1.5 min |
| Run coverage and race in one pass (`-race -covermode=atomic`), replacing `ci.yml:142` and `:154` | ~50 s |
| Cache or path-filter the two prototype cross-builds in "Verify isolated trace preflight" (240 s of 317 s) | up to 4 min |
| Drop re-runs of Go tests already run: `hack/verify-scale-capacity.sh:5-10`, `hack/test-node-context-contract.sh:32`, `Makefile:159` | 10–20 s |
| Path-filter `trace-worker.yml` and the go/fuzz/helm jobs for docs-only PRs | whole jobs |
| Inject the watch interval so tests do not wait on a 500 ms ticker (`internal/cli/restricted_watch_test.go:32,56,92`) | ~2.5 s, less timing risk |
| Remove `test` from `make check` (coverage is a superset) | ~45 s locally |

### 6.4 What to add (higher value than any deletion)

1. **Fuzz the 23 targets CI never fuzzes** (CI fuzzes 7 of 30), in a nightly
   job so PR time stays flat. Add targets for `internal/promhistory/decode.go:33`,
   `internal/collector/pagination.go:164`,
   `internal/extension/metrics_format.go:51`, `internal/memorytopology/wire.go`
   and the cgroup path extractors.
2. **An in-process pipeline test**: agent publisher → extension ingest → store
   → read handler → client. Today the only end-to-end path is the kind job,
   which is skipped for changes classed as not runtime-sensitive.
3. **A concurrent `ReplaceNodeSnapshot` versus scoped read and pagination
   test** in `internal/collector`.
4. **CLI coverage** for container, workload and namespace `top` rows and the
   volume compare, capture and recommend commands (`internal/cli` is 66.8%).
5. **Startup wiring tests** for `cmd/memlens-agent` (5.6%),
   `cmd/memlens-collector` (36.1%) and `internal/certbootstrap` `Run` (38.2%).
6. **`internal/client/kube_proxy.go`**: 17 functions at 0%. Test it or retire
   it with the hidden pre-v1 flags (see I-04).
7. **A Linux job that runs the walker against the runner's real
   `/sys/fs/cgroup`**, to catch kernel-interface drift of the B-03 kind.

## 7. Implementation plan

Each work package is one reviewable PR on a branch named to the repository
convention. Every package follows the same sequence: write the failing
regression test first, make the smallest fix, run `make check`, then update
`CHANGELOG.md` and any contract document the change touches.

### Phase 0: housekeeping (one PR, under a day)

| Package | Branch | Contents | Verification |
| --- | --- | --- | --- |
| WP-00 | `patch/docs-and-build-hygiene` | B-19 roadmap and README text plus the release-contract assertion; B-28; B-29; B-38 (`.gitignore`, wire or delete the three scripts); document `completion` in `docs/installation.md` | `make check-release-contract` fails before and passes after; `go build ./...` on macOS; `make check-trace-preflight` on Go 1.27.4 |

### Phase 1: `v1.0.5` correctness and safety

| Package | Branch | Defects | Tests added | Extra verification |
| --- | --- | --- | --- | --- |
| WP-01 | `fix/ingest-prescan-bound` | B-01 | Handler and legacy-server allocation tests; pre-scan fuzz target | `e2e-kind`; `verify-authenticated-ingestion-kind` |
| WP-02 | `fix/metrics-response-budget` | B-02, B-30 | 1,500-Pod metrics test; counter naming test | Scrape with Prometheus in kind; `promtool check metrics` |
| WP-03 | `fix/cgroup-scan-resilience` | B-03, B-06, B-25 | `signals_test.go`, `walker_test.go`, `parser_test.go`, PSI fuzz seeds | Linux run against real cgroupfs; update `docs/reliability.md` wording on partial walks |
| WP-04 | `fix/agent-enrichment-budget` | B-07, B-26, B-27 | Owner-resolver cache tests; main-loop timeout test; telemetry test | `verify-scale-capacity`; `docs/metrics.md` for the new counter |
| WP-05 | `fix/sandbox-classification` | B-08, B-24 | Scanner and walker cases | kind run with an `ImagePullBackOff` Pod: cluster stays `ready` |
| WP-06 | `fix/pod-limit-diagnosis` | B-04, B-36 | Explain and aggregate tests | Sample output review; `docs/explanation-schema.md` if signals change |
| WP-07 | `fix/replay-capture-time` | B-05, B-10, B-35 | Replay, compare and incident validation tests; fuzz seed | Replay an old fixture by hand |
| WP-08 | `fix/doctor-missing-nodes` | B-09, B-12 | Doctor and kubeconfig-error CLI tests | Run `doctor --strict` in kind with one agent scaled away |

Order: WP-01 and WP-02 first (availability), then WP-03 to WP-05 (they share
the agent scan path, so land them in sequence), with WP-06 to WP-08 in
parallel. Then follow the release tag gate in `AGENTS.md` and
`docs/release-process.md`.

### Phase 2: operability and polish (next patch)

| Package | Branch | Items | Tests added |
| --- | --- | --- | --- |
| WP-09 | `fix/chart-value-guards` | B-16, B-18, B-37, I-10 | Negative `helm template` cases in CI |
| WP-10 | `patch/cert-rotation` | I-02, B-17 (documentation part) | `needsRotation` through the scheduled path; kind test with `validityDays=1` |
| WP-11 | `patch/read-path-efficiency` | B-13, B-14, B-32 | Allocation bound at 100,000 containers; blocking-writer gate test |
| WP-12 | `fix/ingest-rate-limit` | B-15, B-31, B-33 | Handler, reliability and coordinator tests |
| WP-13 | `patch/tui-view-cache` | I-01, B-11, B-20, N-15 | Counting-analyser test; Update-plus-View benchmark; mode Ctrl+C test |
| WP-14 | `fix/cli-output-consistency` | B-21, B-22, B-23, I-07 | Table-driven CLI tests |
| WP-15 | `patch/agent-daemonset-controls` | I-05, I-06 | `helm template` plus kubeconform; walker root test |
| WP-16 | `patch/secure-defaults` | I-03, I-04, I-08, I-09 | Default-mode test; legacy opt-in test; name validation tests |

### Phase 3: test suite and CI

| Package | Branch | Items |
| --- | --- | --- |
| WP-17 | `patch/ci-runtime` | Section 6.3 actions; I-11; I-14 |
| WP-18 | `patch/test-consolidation` | Section 6.2 actions. Behaviour-preserving only: coverage must not fall |
| WP-19 | `feature/pipeline-and-fuzz-tests` | Section 6.4 items 1–5 and 7 |

Acceptance for the phase: "Go checks" under 12 minutes, cross-package coverage
at or above 80.9%, and a nightly fuzz job covering all 30 targets.

### Phase 4: features

Each starts with a public design document, per the roadmap principles.

| Order | Item | Rationale |
| --- | --- | --- |
| 1 | N-06, N-07 distribution | Lowest effort, highest reach; the Krew manifest already exists |
| 2 | N-08 worst-container limit | Small, and completes B-04 for users |
| 3 | N-01 `v1.1.0` | Add `-o` to the remaining commands, publish schemas, then the MCP server |
| 4 | N-11 then N-02 `v1.2.0` | Post-mortems need retention after the cgroup is gone |
| 5 | N-03, N-04, N-05 | As on the roadmap |
| Any time | N-09, N-10, N-12, N-13, N-14 | Independent |

## 8. Decisions needed

| ID | Decision | Recommendation |
| --- | --- | --- |
| D-1 | Trace extension: publish it, keep it as an unpublished development extension, or park it | Keep it, but rename `prototype/trace` to reflect that it is the real server, label the `trace` commands "requires the separately built extension" in `--help`, move the qualification harnesses out of the per-PR path, and park the confirmed-paths pipeline until a client can use it. Then fix T-01 to T-05. |
| D-2 | Should a vanished cgroup still block publishing (B-06)? | No. `docs/reliability.md` protects against partial reads of live state; a cgroup that no longer exists is not one. |
| D-3 | Retire the legacy direct-HTTP and Service-proxy client paths (I-04)? | Yes, in `v1.1.0`, with a deprecation note in the next patch. It removes 17 untested functions. |
| D-4 | Is PSI `full avg10 > 0` alone enough for severity high? | Add a small floor or `avg60` confirmation; a 0.01% stall currently outranks a Pod at 95% of its limit. |
