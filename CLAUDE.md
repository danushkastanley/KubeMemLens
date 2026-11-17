# KubeMemLens — Claude Code instructions

@AGENTS.md

AGENTS.md owns branch naming, commit metadata and the release tag gate. This file owns how a change is verified.

## Verification

Verification means running the change, not adding tests. Before saying a change works:

1. Run the cheap checks for the packages you touched: `go test ./internal/<pkg>/...`, `make fmt-check`, `go vet ./...`, `go build ./cmd/...`.
2. Read your diff against the trigger table in [docs/verification.md](docs/verification.md) and run only the rows that match. Most changes match two to five rows. Do not run `make check` or every kind lane by reflex; CI runs the full set.
3. Exercise the changed behaviour on a real path with the `verify` skill ([.claude/skills/verify/SKILL.md](.claude/skills/verify/SKILL.md)): the sample commands for presentation and diagnosis changes, a disposable kind cluster for anything that runs in the agent, collector or chart.
4. Report what you ran, what you saw, and what you could not run. "Tests pass" is not evidence that the feature works.

## Tests

Write a test only when it can fail for a reason a user would care about. The full rules are in [docs/verification.md](docs/verification.md#which-tests-to-write); the short form:

- A bug fix gets one regression test that fails before the fix. Confirm that it fails first.
- New parsing, decoding, pagination or wire code gets a property or fuzz test beside the existing `Fuzz*` targets, not a table of hand-picked cases.
- Runtime behaviour is proven by the kind harness, not by a unit test built on mocks.
- Do not add tests that restate the implementation, assert on private structure, cover getters or constructors, or exist to raise coverage. Coverage is reported, never targeted.

## When a check fails

- Fix the code, not the check. Never skip, loosen, retry or add a sleep to make a check pass.
- A change must not edit the check that judges it. If a check itself is wrong, say so and change it in a separate commit with the reason.
- After three failed attempts at the same check, stop and report the failure with its output.
- A flaky test is a defect: find the cause. If it cannot be found, propose deleting the test and record why; do not leave it retrying.

## When something slips through

If a defect reaches review or `main` that a check should have caught, add one row to the trigger table or one rule to [REVIEW.md](REVIEW.md) in the same PR as the fix. Remove rows and rules that no longer earn their run time.
