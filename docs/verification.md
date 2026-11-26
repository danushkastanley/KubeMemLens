# Verification

How to decide what to run for a change, and which tests are worth writing. [CONTRIBUTING.md](../CONTRIBUTING.md#verification-scope) states the policy; this page is the working reference.

The rule: read the diff, run the checks whose trigger matches, then run the change itself. CI runs everything else.

## Order

1. **Cheap, every change** (seconds): focused package tests, `make fmt-check`, `go vet ./...`, `go build ./cmd/...`.
2. **Triggered checks** (seconds to a few minutes): the rows below that match the diff.
3. **Run the change** (under a second for samples, several minutes for kind): see [Run the change](#run-the-change).
4. **Before opening a PR**: `make test-race` if the change touches goroutines, locks or shared state.

`make check` is the full local gate: everything CI runs on a pull request apart from the kind lanes, including the trace extension, release, provider and terminal contracts. It also runs the trace qualification harnesses, which CI runs only when their paths change and once a week. It runs the Go tests once, with the race detector and coverage together. Run it when a change spans several areas or touches the Makefile; otherwise the rows below are enough and CI covers the rest.

## Trigger table

Paths are prefixes. Run a row only when the diff touches its trigger.

| Diff touches | Run | Why |
| --- | --- | --- |
| `internal/cgroup/` parsers | `go test ./internal/cgroup -run='^$' -fuzz='^<FuzzTarget>$' -fuzztime=10s` for the parser changed | Kernel files vary by version and runtime; hand-picked cases miss them |
| Any decoder or wire type with a `Fuzz*` target in its package | The matching fuzz target for 10s | Decoders take untrusted bytes |
| `internal/collector/` pagination, snapshots, ingestion | `go test -race ./internal/collector/...` and the ingestion smoke | Read gate, rate limit and record copying have regressed here |
| `internal/recommend/`, `internal/explain/` | `make check-recommendation-evaluation`, then `make run-sample-explain` | Diagnosis quality is scored against fixtures, not asserted line by line |
| `internal/tui/`, `internal/*view/` | `E2E_RUN_TUI_SMOKE=true` kind run, or `make run-sample-top` for layout-only changes | Key handling and quit paths only fail in a real terminal |
| `internal/kubeauth/`, `internal/kubeprincipal/`, `internal/extension/` | Ingestion, tenant-scoped read and tenant isolation smokes | Authorisation is only proven against a real API server |
| `internal/resourcemetrics/` | `E2E_RUN_RESOURCE_METRICS_SMOKE=true` | Depends on the metrics API being served |
| `internal/qosview/` | `E2E_RUN_MEMORY_QOS_SMOKE=true` | Depends on the kubelet feature gate |
| `internal/volumehealth/`, `internal/volumecontext/` | `make check-provider-contract`; `hack/verify-volume-health-kind.sh` for health changes | CSI behaviour differs with the alpha gate on and off |
| `internal/memoryhistory/`, `internal/promhistory/` | `make check-memory-history-contract` | History wire format is a published contract |
| `internal/incidentsession*/`, `internal/incident/` | `make check-incident-session-contract` | Bundles must stay bounded and redacted |
| `internal/replica*/` or chart replica values | `make check-replica-contract` | Chart guards and replica logic must agree |
| `charts/kube-memlens/` | `helm template` with the changed values, `hack/test-chart-value-guards.sh`, then a base kind run | The kind run covers install, upgrade, rollback and uninstall |
| A new resource kind, Pod or projected token in `charts/kube-memlens/templates/` | The `Helm checks` steps in `.github/workflows/ci.yml`, run in order, and `NODE_CONTEXT_EXECUTION_MODE=api-reads hack/verify-node-context-execution-kind.sh` | Both list exactly what the chart may contain: the Helm checks count projected tokens, and `hack/node-qualification/owned_resources.py` lists the allowed kinds. Neither is part of the base kind run |
| `charts/kube-memlens-trace/`, `extensions/trace/` | `make check-trace-chart-contract`, `make check-trace-preflight` | Separate module with its own gate |
| `extensions/trace/qualification/`, `hack/ebpf-qualification/`, `hack/ebpf-active-qualification/`, `hack/trace_packages.py` | `make check-trace-qualification` | Non-loading harness tests that left the per-PR gate ([ADR 0031](adr/0031-keep-the-trace-extension-as-a-separately-built-extension.md)). Their workflow is not a required check |
| `extensions/trace/worker/` | `make check-trace-worker` | Separate module and SDK preparation |
| RBAC, NetworkPolicy, Service ports, security context | Tenant isolation smoke, and re-read [security-model.md](security-model.md) | A privilege change needs a stated reason |
| Metric names or labels | Scrape `metrics/current` on a kind run; update [metrics.md](metrics.md) | Dashboards and alerts depend on the names |
| Go files with an OS build tag (`_linux.go`, `//go:build`) | `go build ./...` and `GOOS=linux go build ./...` | A `main` package declared for one OS breaks the build on the others |
| `hack/*.sh`, `hack/lib/` | `bash -n` on the file, its `hack/test-*.sh` partner if one exists | Shell errors surface late in CI |
| `hack/ci_scope.py` | `python3 hack/test_ci_scope.py` | Decides whether kind lanes run at all |
| `.github/workflows/` | `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7` | |
| `go.mod`, `go.sum`, `Dockerfile` | `go mod verify`, `make vuln`, `make build`, base kind run | Supply chain and image contents |
| `hack/release/`, `.goreleaser.yml`, release workflows | `make check-release-contract` | |
| `hack/scale-profiles/`, density code paths | `make check-scale-contract` | Live soak stays opt-in; see [scale-qualification.md](scale-qualification.md) |
| `README.md`, `SUPPORT.md`, community files | `make check-support-contract`, `make check-community-contract` | These files are checked for required content |
| An image pinned as `name:tag@sha256:…` in a workflow `run:` step, a `hack/` script or a chart template | `python3 hack/check_embedded_digests.py` (needs Docker and network) | Dependabot does not read these. The `Embedded image digests` workflow runs the same check weekly and fails when a tag has moved |
| Other `docs/` only | Check local links resolve | No Go or kind run needed |

Never run without an explicit request: `make soak-live-density`, `make qualify-cluster`, anything under `hack/ebpf-*qualification/`, or any script against a cluster that is not a disposable kind cluster created for the run.

## Reliability checklist

Each item came from a shipped fix. Ask the question only when the trigger matches.

| Trigger in the diff | Question | Origin |
| --- | --- | --- |
| A numeric field gains a new source or default | Can "unknown", "unreported" and "unlimited" still be told apart from zero in every output? | unreported kernel total shown as zero; unlimited versus unknown limit in `top` |
| A cache or TTL around a Kubernetes lookup | Does a failed lookup retry soon, or wait out the full TTL? | Node lookup retry |
| The agent drops, filters or withholds data | Is the withheld amount counted in a metric? | withheld snapshots |
| A walk over cgroups or Pods | What happens when an entry disappears mid-walk? | cgroups that stop during the walk |
| A timeout wrapped around a scan | Does the budget include API calls it should not? | Kubernetes lookups inside the scan timeout |
| A lock or gate around a read path | Is it released before writing the response? | read gate held during response write |
| A rate limit or quota | Is it charged after admission and per producer? | ingestion rate limit |
| A decoder for agent or tenant input | Is count and size bounded before decoding? | snapshot container count |
| A `--watch` or long-running loop | Does it survive a collector outage and recover? | deep `top --watch` |
| A new TUI screen or key binding | Do Ctrl+C and `q` quit from it? | quit from every screen |
| An empty result set | Is "empty" distinguishable from "rebuilding" or "never reported"? | empty namespace; `doctor` and silent nodes |
| A new chart value | Is an invalid combination refused at render time? | chart value guards |
| A test assigns a process-wide variable (`time.Local`, an environment variable, a package-level default) | Can another test in the package read it while this one runs? Run `go test -race` on the package | history test set `time.Local` under parallel tests |
| Container-to-Pod mapping | Does it hold on nested runtimes and for idle Pod sandboxes? | nested cgroup layout; sandbox as infrastructure |
| A limit in the collector's snapshot validation | Can a Pod the API server accepts exceed it, and does the agent stay inside it before posting? | one Pod with 65 labels made the collector reject its node's snapshots |

## Which tests to write

A test earns its place when it would catch a defect a user would notice. In order of value for this project:

1. **Kind end-to-end.** The agent reads a real cgroup tree and the CLI shows the result. Hard to fool. Extend the kind harness or a `hack/verify-*-kind.sh` script when a new runtime behaviour has no smoke.
2. **Property and fuzz tests.** State something that cannot break and let the fuzzer attack it: decode never panics and respects its bounds; encode then decode returns the input; pagination visits every key exactly once; an optimised path equals the plain one. Add the target next to the existing `Fuzz*` functions.
3. **Differential tests.** For a rewrite or migration that must keep behaviour, such as a new reader, decoder or history backend: the old path is the oracle. Run old and new on the same inputs, generated or taken from fixtures. Normalise differences that do not matter, such as ordering or whitespace. Keep a written list of approved differences next to the test. Any other difference is a bug or an unapproved breaking change.
4. **Fixture tests.** A synthetic cgroup fixture or sample workload with an expected explanation. Use these for parser, aggregation and presentation changes.
5. **Regression tests.** One per bug fix, failing before the fix. Name it after the behaviour, not the function.

Do not write:

- tests that repeat the implementation's arithmetic or assert on unexported fields;
- mocks of the Kubernetes API or collector standing in for a runtime path the kind harness can run;
- tests for getters, constructors, constants or flag wiring;
- extra table rows that take the same branch;
- tests whose only purpose is a coverage number.

If a test is slow or flaky it will be re-run instead of read. Keep unit tests fast and deterministic, and move anything that needs a cluster into the kind harness.

## Run the change

The `verify` skill at [.claude/skills/verify/SKILL.md](../.claude/skills/verify/SKILL.md) holds the commands. In short:

- **Presentation, diagnosis, CLI flags:** `go run ./cmd/kubectl-memlens sample list|top|explain <sample>` and read the output.
- **Agent, collector, chart, RBAC, TUI:** `hack/e2e-kind.sh` with its own cluster name and only the smoke flags the diff needs. The script creates the cluster, uses a temporary kubeconfig and deletes the cluster on exit.

## Verification method

Start the PR's Verification section with one method, so unverified changes can be found later:

| Method | Meaning |
| --- | --- |
| `kind` | The change ran on a disposable kind cluster and the output showed the behaviour |
| `sample` | The change ran through the sample commands and the output showed the behaviour |
| `checks` | Only the cheap and triggered checks ran; nothing exercised the behaviour |
| `none` | Nothing ran. Say why |

`checks` and `none` are allowed. They must be stated, not implied. The evidence must come from the commit being merged; see [Stale evidence](#stale-evidence).

## Stale evidence

Verification and review results belong to the commit they ran on. After a new push, re-run the steps whose trigger the new commits match, and re-review if the code changed. A pass on an earlier commit is not evidence for the current one.

## Writing a check

A check is a test, smoke, contract script, trigger row or review rule.

- The person who owns the area writes its checks, and refines them until they flag what that person would flag in review.
- A failing check says why: the defect, the input or state that caused it, and the step that failed. A reviewer should not have to rebuild the reasoning from the diff.
- A runtime check leaves evidence when it fails: the step, the command output and the artefacts under `E2E_ARTIFACT_DIR`, not only a timeout.
- Keep checks fast and deterministic. A slow check gets run less often. A flaky one gets re-run instead of read.
- A check is never weakened in the commit that it judges. A wrong check is fixed in its own commit, with the reason.

## Adding a gate

A gate blocks a merge or a release: a required CI job, a new contract script in `make check`, or a hook.

1. **Observe.** Run it without blocking, for example with `continue-on-error: true` in CI, and record what it would have blocked.
2. **Review.** Read every would-be block. Fix false positives before the gate affects anyone.
3. **Enforce.** Make it required only when the false positives are understood. Document how to record a justified exception, so a correct change is never stuck behind the gate.

There is no local commit gate today. Add one through these three steps if needed.

## Turning misses into rules

A miss is a defect that reached review, `main` or a release when a check should have caught it.

- **From a fix or revert.** Write the trigger as something visible in a diff, plus the question or command that catches it. Add it to the trigger table, the reliability checklist or [REVIEW.md](../REVIEW.md) in the fixing PR, in its own commit.
- **From review comments.** A pattern becomes a candidate rule only when it has appeared in at least two PRs and can be recognised from a diff. Try to disprove it first: find merged, clean changes that it would wrongly flag. A maintainer approves it in a PR.
- **Replay before adding.** Run `/code-review` on the original diff with the current rules. If the defect is already found, no new rule is needed. If a small diff still misses it, add the rule to REVIEW.md.

## Reviewing the checks

At each release, when the changelog is written from merged PRs:

- List the PRs since the last tag that were reverted or needed a follow-up fix. For each, record whether a row, rule or test caught it, and at which stage: local checks, `/code-review`, CI or human review.
- List rows and rules that fired but caught nothing a maintainer cared about.
- Add rules for the misses as described above. Remove or narrow the noisy ones.

## Keeping this page honest

- A defect that reached review or `main` and matches no row adds one row, in the PR that fixes it.
- A row that has not caught anything and costs real time is removed.
- A change must not edit a check, fixture or row that judges it in the same commit as the code it judges.

Background: this page follows the practices in [How the Claude Code team thinks about verification](https://claude.dev/blog/how-the-claude-code-team-thinks-about-verification/).
