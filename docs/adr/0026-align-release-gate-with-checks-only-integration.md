# ADR 0026: Align the release gate with checks-only integration

Date: 19 October 2026

Status: proposed for review; effective only after merge

## Context

The September integration policy requires PRs and strict CI but disables
mandatory approving reviews, CODEOWNER approval and last-push approval. It
retained a Branch-Protection release threshold of 8. The v1.0.1 audit observed
a score of 4, so the selected integration policy and release gate disagree.
The maintainer requested retaining checks-only PRs and preparing an explicit
release-policy amendment for review.

## Decision

Set the Branch-Protection minimum to 4 while requiring the existing live-control
readback: PR-only integration, strict required checks, resolved conversations,
deletion and force-push protection, and the exact checks-only review settings.
Retain protected independent release approval, no environment administrator
bypass, release-tag restrictions, immutable artefacts and exact-tag approval.
The score does not substitute for any live-control check.

Keep the overall minimum at 7.0, Vulnerabilities at 7 with zero reachable
vulnerabilities and a reviewed advisory closure plan, and Token-Permissions,
Dangerous-Workflow, Pinned-Dependencies and Signed-Releases at 8. No other
release gate or current vulnerability finding is waived.

## Alternatives and consequences

Restoring mandatory PR approvals would support the previous score threshold
but conflicts with the maintainer's chosen integration policy. Ignoring the
reported score without changing the documented gate would conceal a failure.

This amendment accepts that code can merge without an independent human
review. CI cannot replace that review. Independent publication approval remains
required but does not establish that every merged change was independently
reviewed. The current Vulnerabilities score of 4 still blocks release.

## Verification, migration and rollback

The live checker uses a shared Scorecard predicate. Regression cases cover the
accepted boundary, each lower score, missing checks and vulnerability failure
despite a passing overall score. Existing PR-policy checks remain in place.

No GitHub setting or data migration is needed. Merge only after review, then
repeat the live readback before the candidate decision. Rollback restores the
Branch-Protection minimum of 8 in the policy and predicate; releases then block
until that requirement is met or another explicit decision is reviewed.
