# KubeMemLens — Claude Code instructions

@AGENTS.md

AGENTS.md owns branch naming, commit metadata and the release tag gate. This file owns how a change is verified and reviewed. The working reference is [docs/verification.md](docs/verification.md); the reviewer's rules are in [REVIEW.md](REVIEW.md).

## Verification

Verification means running the change, not adding tests. Before saying a change works:

1. Run the cheap checks for the packages you touched: `go test ./internal/<pkg>/...`, `make fmt-check`, `go vet ./...`, `go build ./cmd/...`.
2. Read your diff against the trigger table and the reliability checklist in [docs/verification.md](docs/verification.md) and run only the rows that match. Most changes match two to five rows. Do not run `make check` or every kind lane by reflex; CI runs the full set.
3. Exercise the changed behaviour on a real path with the `verify` skill ([.claude/skills/verify/SKILL.md](.claude/skills/verify/SKILL.md)): the sample commands for presentation and diagnosis changes, a disposable kind cluster for anything that runs in the agent, collector or chart.
4. Report what you ran, what you saw, and what you could not run, with the verification method from [docs/verification.md](docs/verification.md#verification-method). "Tests pass" is not evidence that the feature works.

Run steps 1 to 3 before you push or open a PR. Untested pushes spend shared CI and review time.

Evidence belongs to one commit. If you change code after verifying or reviewing it, run the affected steps again. A pass on an older commit proves nothing about the current one.

## Tests

Write a test only when it can fail for a reason a user would care about. The full rules are in [docs/verification.md](docs/verification.md#which-tests-to-write); the short form:

- A bug fix gets one regression test that fails before the fix. Confirm that it fails first.
- New parsing, decoding, pagination or wire code gets a property or fuzz test beside the existing `Fuzz*` targets, not a table of hand-picked cases.
- A rewrite or migration that must keep behaviour gets a differential test: old and new paths on the same generated inputs, with an explicit list of approved differences.
- Runtime behaviour is proven by the kind harness, not by a unit test built on mocks.
- Do not add tests that restate the implementation, assert on private structure, cover getters or constructors, or exist to raise coverage. Coverage is reported, never targeted.

## Review

- Use one reviewer, `/code-review`, after the change has been verified and before the PR is opened. If reviews miss things, tune [REVIEW.md](REVIEW.md); do not add a second reviewer.
- Each later round builds on the earlier ones: pass the previous findings, what changed since, and any check that failed, rather than reviewing from scratch.
- Findings are tagged **Important**, **Nit** or **Pre-existing**. Fix Important findings before the PR. Report Pre-existing ones and do not fix them in the same change.

## When a check fails

- Fix the code, not the check. Never skip, loosen, retry or add a sleep to make a check pass.
- A change must not edit the check that judges it. If a check itself is wrong, say so and change it in a separate commit with the reason.
- After three failed attempts at the same check, stop and report the failure with its output.
- A flaky test is a defect: find the cause. If it cannot be found, propose deleting the test and record why; do not leave it retrying.

## Keeping the checks honest

- If a defect reaches review or `main` that a check should have caught, add one row to the trigger table or reliability checklist, or one rule to [REVIEW.md](REVIEW.md), in the same PR as the fix but a separate commit. A rule must be recognisable from a diff; see [docs/verification.md](docs/verification.md#turning-misses-into-rules).
- A check you write names the defect and the input when it fails. See [docs/verification.md](docs/verification.md#writing-a-check).
- A new blocking gate starts in observe mode. See [docs/verification.md](docs/verification.md#adding-a-gate).
- Remove rows, rules and tests that no longer earn their run time.
