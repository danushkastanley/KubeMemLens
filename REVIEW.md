# Review instructions

KubeMemLens reads cgroup and Kubernetes memory evidence on each node and explains it to an operator. A wrong number or an overconfident diagnosis is the worst defect this project can ship. Review for that first.

Tag each finding **Important**, **Nit** or **Pre-existing**. Report a pre-existing defect once and do not hold the change for it. Say why a finding matters in one sentence, with the input or state that triggers it.

## Always check

- **Evidence honesty.** Unknown, unreported and unlimited values stay distinct from zero all the way to the output. Copy describes evidence and uncertainty; it never claims a leak or root cause.
- **Bounds.** Anything decoded from an agent, a tenant, the API server or a file has an explicit size, count and time limit, checked before allocation.
- **Tenant and privilege boundary.** No new RBAC verb, resource, host path, capability, port or NetworkPolicy opening without a stated reason. Namespace-scoped readers never see another namespace's Pods, history or node data.
- **Sensitive output.** No Pod names, namespaces, container IDs, tokens or paths in logs, metrics labels or incident bundles beyond what the documented contracts allow.
- **Failure states.** A collector outage, missing node, stopped cgroup or unusable pressure file produces a named, degraded state, not a zero, an empty table or a silent success.
- **Metrics and wire contracts.** A changed metric name, label, JSON field or CLI flag is either backwards compatible or documented in `docs/` and the chart in the same change.
- **Concurrency.** Locks are not held across network writes or Kubernetes calls; goroutines stop on context cancellation.

## Tests in the change

Flag as **Important**:

- a bug fix with no test that would have failed before it;
- a test edited or deleted so that the change passes;
- a skip, retry, sleep or widened timeout added to quiet a failure;
- a change to a check, contract script, fixture, CI scope rule, trigger row, the `verify` skill or this file that also benefits from that edit;
- a rewrite or migration that claims unchanged behaviour with no differential test or list of approved differences.

Flag as **Nit**, and ask for removal rather than improvement:

- tests that mirror the implementation line for line, assert on private fields or mock the thing under test;
- tests for getters, constructors, constants or generated code;
- several table cases that exercise the same branch;
- a hand-written case table where an existing `Fuzz*` target or a round-trip property already covers the input space.

Do not ask for more tests to raise coverage. Ask for a test only when you can name the defect it would catch.

## Verification evidence

The PR description must give a [verification method](docs/verification.md#verification-method) and state what was run and what was observed. Check it against [docs/verification.md](docs/verification.md): if the diff matches a trigger row and the description shows no evidence for it, say which row. A runtime change verified only by unit tests is an **Important** finding.

Evidence that predates the latest commit to the code it covers is stale. Name the commits it does not cover.

If this is a later review round, start from the earlier findings. Say which are fixed, which remain, and what is new. Do not re-report findings that were resolved.

## Do not report

- Formatting, import order or naming that `make fmt-check` and `go vet` already enforce.
- Missing `CHANGELOG.md` entries; the changelog is written at release time.
- Style preferences with no behavioural consequence.
- Requests for abstraction, configuration or defensive branches the change does not need.
- Anything under `local-docs/`, `dist/`, `qualification-evidence/` or `docs/qualification-results/` unless the change edits it.
