# v1 go/no-go and adopter record

Status: **NO-GO template, no approval recorded**

Prepared: 27 August 2026

Evidence register: [v1 candidate evidence and blocker register](v1-candidate-evidence.md)

Use this record only after the candidate identity is frozen. Replace every
`Not recorded` value with a linkable result. Do not delete failed or partial
results. A named maintainer role does not mean that person has reviewed or
approved the candidate.

## Candidate identity

| Field | Result |
| --- | --- |
| Candidate version | Not recorded |
| Annotated RC tag | Not recorded |
| Source commit | Not recorded |
| Release workflow run | Not recorded |
| Archive checksum inventory | Not recorded |
| Image digest | Not recorded |
| Chart digest | Not recorded |
| Reproducibility comparison | Not recorded |
| Clean-consumer result | Not recorded |
| Proposed v1 identity | Not recorded |
| Exact-artefact promotion proof | Not recorded |

## Review roles

The role assignments come from [the maintainer record](../MAINTAINERS.md).
They record responsibility only.

| Responsibility | Named person | Review state | Dated evidence |
| --- | --- | --- | --- |
| Primary release decision | Danushka Stanley, `@danushkastanley` | Not reviewed | Not recorded |
| Backup release review | `@legolas296` | Not reviewed | Not recorded |
| Security blocker readback | Danushka Stanley, primary; `@legolas296`, backup | Not reviewed | Not recorded |
| Rollback owner | Danushka Stanley, primary; `@legolas296`, backup | Not activated for a candidate | Not recorded |

The backup reviewer must review the exact head and artefact inventory. Approval
of an earlier pull request or policy change does not approve a release tag.

## Adopter evidence

Current count: **0/3**. No issue with the `adopter-feedback` label existed at
the 27 August 2026 readback.

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
| Every dependency ticket has accepted evidence | NO-GO | Not recorded |
| Candidate commit and all artefact identities are frozen | NO-GO | Not recorded |
| Candidate rebuild is reproducible | NO-GO | Not recorded |
| No P0/P1 security, correctness, data-leakage or release-integrity blocker remains | NO-GO | Not recorded |
| OpenSSF project 14259 has a verified Passing result | NO-GO | Deferred; no Passing result recorded |
| Current Kubernetes 1.35 through 1.37 lifecycle matrix passes | NO-GO | Not recorded |
| ProjectV2 and release-blocking work queues are reconciled | NO-GO | Current token lacks `read:project` |
| Dependency, advisory, CodeQL, secret and Scorecard queues are reviewed | NO-GO | Not recorded |
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
| Remaining blockers | V1-B01 through V1-B08 |
| Exact annotated tag approved | No |
| Publication approved | No |
| Rollback target | Not recorded |
| Post-release observation window | Not recorded |

A `GO` decision requires every gate above to read `GO`, with dated evidence for
the exact candidate. After both named reviews are recorded, obtain fresh
explicit approval for the exact annotated tag and publication. Until then, do
not create a tag, push an image or chart, publish a release, or update package
indexes.

## Post-release ownership

Complete this section before changing the final decision.

| Operation | Primary | Backup | Trigger and response evidence |
| --- | --- | --- | --- |
| Release observation | Danushka Stanley | `@legolas296` | Not recorded |
| Vulnerability response | Danushka Stanley | `@legolas296` | [Security policy](../SECURITY.md) and [maintainer operations](security/maintainer-operations.md); live readiness not recorded |
| Dependency and Scorecard review | Danushka Stanley | `@legolas296` | [Repository security policy](repository-security.md); candidate readback not recorded |
| Rollback decision | Danushka Stanley | `@legolas296` | [Release rollback process](release-process.md); candidate target not recorded |
| Adopter follow-up | Danushka Stanley | `@legolas296` | [Community feedback policy](community-feedback.md); reports not recorded |
