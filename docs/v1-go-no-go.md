# v1 go/no-go and adopter record

Status: **`v1.0.0-rc.1` owner approval recorded; stable `v1.0.0` NO-GO**

Prepared: 29 August 2026

Evidence register: [v1 candidate evidence and blocker register](v1-candidate-evidence.md)

This file is the pre-freeze stable-promotion template. After the candidate tag
freezes the source commit, append candidate results, adopter links and dated
decisions to [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50).
Do not delete failed or partial results. A named maintainer role does not mean
that person has reviewed or approved the stable release.

## Candidate identity

| Field | Result |
| --- | --- |
| Candidate version | `v1.0.0-rc.1` approved; not yet built |
| Annotated RC tag | `v1.0.0-rc.1` approved; not yet created |
| Source commit | Not recorded |
| Release workflow run | Not recorded |
| Archive checksum inventory | Not recorded |
| Image digest | Not recorded |
| Chart digest | Not recorded |
| Reproducibility comparison | Not recorded |
| Clean-consumer result | Not recorded |
| Proposed v1 identity | `v1.0.0`; stable tag and publication not approved |
| Exact-artefact promotion proof | Not recorded |

## Review roles

The role assignments come from [the maintainer record](../MAINTAINERS.md).
They record responsibility only.

| Responsibility | Named person | Review state | Dated evidence |
| --- | --- | --- | --- |
| Primary release decision | Danushka Stanley, `@danushkastanley` | `v1.0.0-rc.1` candidate approved; stable not reviewed | [Release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50), 29 August 2026 |
| Backup release review | `@legolas296` | Not reviewed | Not recorded |
| Security blocker readback | Danushka Stanley, primary; `@legolas296`, backup | Not reviewed | Not recorded |
| Rollback owner | Danushka Stanley, primary; `@legolas296`, backup | Not activated for a candidate | Not recorded |

The backup reviewer must review the exact head and artefact inventory. Approval
of an earlier pull request or policy change does not approve a release tag.
The temporary pull-request bypass used for the implementation queue was removed
after PR #49 merged. It did not satisfy or remove this independent release
review. The protected release environment prevents self-review and remains the
write boundary for candidate and stable workflows.

## Adopter evidence

Current count: **0/3**. No issue with the `adopter-feedback` label existed at
the 28 August 2026 readback.

Use the [privacy-safe adopter form](../.github/ISSUE_TEMPLATE/adopter_feedback.yml)
and the [feedback policy](community-feedback.md). An adopter report must identify
the candidate by tag, commit or immutable digest. It must not contain account,
project, subscription, cluster, namespace, workload, host, image or resource
identifiers.

| Report | Independent adopter | Candidate identity | Redacted environment | Install and useful diagnostic result | Upgrade or rollback | Uninstall | Privacy review | Maintainer outcome |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded |
| 2 | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded |
| 3 | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded | Not recorded |

Count a report only after the maintainer confirms that the adopter is
independent, the tested identity matches the frozen candidate, required fields
are complete, and the public text passes the privacy review. Feedback does not
widen a provider-support claim by itself.

## Gate review

| Gate | Decision | Reviewer evidence |
| --- | --- | --- |
| Candidate machinery and dependency-update queue have accepted evidence | GO at pre-candidate baseline | The queue closed through PRs #38 to #41; repeat the queue and reachability readback on the frozen candidate. |
| Candidate commit and all artefact identities are frozen | NO-GO | Not recorded |
| Candidate rebuild is reproducible | NO-GO | Not recorded |
| No P0/P1 security, correctness, data-leakage or release-integrity blocker remains | NO-GO | Not recorded |
| OpenSSF project 14259 has a verified Passing result | GO at pre-candidate baseline | The public [Passing record](https://www.bestpractices.dev/en/projects/14259/passing) reached 100% of required Passing criteria on 28 August 2026 and uses `https://kubememlens.com` as the homepage. This is a self-assessment, not certification. |
| Current Kubernetes 1.35 through 1.37 lifecycle matrix passes | GO at pre-candidate baseline | All three lanes passed on merged main commit `c7eb76c0d79a3537ef24f2ea47b80ea8d0663c86` in [run 33194300176](https://github.com/danushkastanley/KubeMemLens/actions/runs/33194300176), including the successful retry of the transient 1.35 lane. Confirm the same tree on the frozen candidate. |
| ProjectV2 and release-blocking work queues are reconciled | GO at pre-candidate baseline | An authenticated repository `projectsV2(first: 20)` query returned `totalCount: 0` and no nodes on 28 August 2026. Release blockers remain recorded in the evidence register. |
| Dependency, advisory, CodeQL, secret and Scorecard queues are reviewed | GO at pre-candidate baseline | The merged-main readback found zero open dependency-update pull requests, Dependabot alerts, private advisories, secret alerts or CodeQL-origin alerts. Three Scorecard SARIF findings remain as posture signals. Signed [run 33194300199](https://github.com/danushkastanley/KubeMemLens/actions/runs/33194300199) scored 8.8; Vulnerabilities is 7, Fuzzing is 10 and SAST is 10, so every explicit release threshold is met. |
| Fresh install, supported upgrade, rollback and uninstall pass | NO-GO | Not recorded |
| Three independent adopter reports are accepted | NO-GO | 0/3 |
| Provider, runtime, scale and terminal claims match evidence | NO-GO | Not recorded |
| Limitations and unsupported environments are prominent | NO-GO | Not recorded |
| Security, compatibility, upgrade and rollback notes are complete | NO-GO | Not recorded |
| v1 uses the frozen RC bytes without a rebuild | NO-GO | Promotion controls are prepared but no candidate-bound run or equality proof exists |
| Monitoring, vulnerability response and rollback ownership are active | NO-GO | Not recorded |

## Final decision

| Field | Value |
| --- | --- |
| Decision | **NO-GO** |
| Decision timestamp | Not recorded |
| Primary decision and evidence link | Not recorded |
| Backup review and evidence link | Not recorded |
| Remaining blockers | V1-B02, V1-B05, V1-B06 and V1-B08; V1-B01, V1-B03, V1-B04 and V1-B07 are closed at the pre-candidate readback |
| Exact annotated tag approved | Candidate `v1.0.0-rc.1`: yes, 29 August 2026. Stable `v1.0.0`: no |
| Publication approved | Candidate prerelease: yes, 29 August 2026. Stable release: no |
| Rollback target | Not recorded |
| Post-release observation window | Not recorded |

Stable `GO` requires every gate above to read `GO`, with dated evidence for the
exact candidate in issue #50. Candidate owner approval is recorded, but the
protected release environment still requires its independent reviewer before
write-capable jobs run. Stable promotion needs fresh approval for the exact
`v1.0.0` tag and publication.

## Post-release ownership

Complete this section before changing the final decision.

| Operation | Primary | Backup | Trigger and response evidence |
| --- | --- | --- | --- |
| Release observation | Danushka Stanley | `@legolas296` | Not recorded |
| Vulnerability response | Danushka Stanley | `@legolas296` | [Security policy](../SECURITY.md) and [maintainer operations](security/maintainer-operations.md); live readiness not recorded |
| Dependency and Scorecard review | Danushka Stanley | `@legolas296` | [Repository security policy](repository-security.md); merged-main Scorecard is 8.8 overall, Vulnerabilities is 7, Fuzzing is 10 and SAST is 10, closing V1-B07 at the pre-candidate readback |
| Rollback decision | Danushka Stanley | `@legolas296` | [Release rollback process](release-process.md); candidate target not recorded |
| Adopter follow-up | Danushka Stanley | `@legolas296` | [Community feedback policy](community-feedback.md); reports not recorded |
