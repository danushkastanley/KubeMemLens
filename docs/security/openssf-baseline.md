# OpenSSF Baseline

Status: community controls accepted; Best Practices submission deferred
Recorded: 27 August 2026
Owner: maintainers named in `MAINTAINERS.md`

Best Practices project: https://www.bestpractices.dev/projects/14259

Owner decision: submission is deferred until the public KubeMemLens domain is
live so the project record can use the long-lived public site. The GitHub
repository remains the current project URL. Project 14259 stays `in_progress`;
the deferred Passing result remains a v1 release blocker and must not be
presented as achieved.

## Scorecard

The repository had no published OpenSSF Scorecard result before PROD-011. A
local OpenSSF Scorecard v5.5.0 run against merged commit `9bdd0aa` on 27 August
2026 established an overall baseline of 6.1. The first accepted published run
used merged commit `11dda186`, scored 8.0, and passed the release thresholds.
The pinned, least-privilege workflow runs on `main` and weekly, publishes the
signed result, retains SARIF for five days, and uploads findings to GitHub code
scanning.

Accepted workflow: <https://github.com/danushkastanley/KubeMemLens/actions/runs/33079572322>

| Release check | Local baseline | Published result | Reason |
|---|---:|---:|---|
| Branch-Protection | 0 | 8 | `main` requires one CODEOWNER approval, stale-review dismissal, last-push separation, resolved conversations and strict checks; the remaining warning is that one approval is not the Scorecard maximum |
| Token-Permissions | 10 | 10 | Workflow tokens follow least privilege |
| Dangerous-Workflow | 10 | 10 | No dangerous workflow pattern detected |
| Pinned-Dependencies | 10 | 10 | All workflow dependencies are pinned by full commit SHA |
| Vulnerabilities | 7 | 7 | Three dependency advisories remain unreachable; `govulncheck` found zero affected symbols |
| Signed-Releases | 8 | 8 | Both detected release artefacts have signed subjects |

Other closure work from the local baseline is now visible in the published
result: Dependency-Update-Tool is 10, SAST is 7, CII-Best-Practices is 2 while
project 14259 remains in progress, and Code-Review is 2 after one of the five
changesets sampled by Scorecard had an independent approval. The last two are
recorded gaps, not release-threshold waivers.

The three dependency results were `GO-2026-6094`, `GO-2026-6107`, and
`GO-2026-5932`. `govulncheck v1.1.4 -show verbose ./...` reported zero affected
symbols: two vulnerable imported packages were not called by KubeMemLens and
the unmaintained `x/crypto/openpgp` package was not used. The dependency policy
still requires review on every change in reachability or upstream guidance.

The accepted merged run meets the thresholds in
[`docs/repository-security.md`](../repository-security.md). Re-check the
published result before release; do not infer a pass from a workflow file alone.

## Best Practices Passing assessment

The official OpenSSF Best Practices questionnaire remains the final result of
record. The repository currently has direct evidence for the following required
areas:

| Area | Repository evidence |
|---|---|
| Project, licence and interaction | `README.md`, `LICENSE`, `NOTICE`, `SUPPORT.md`, `CONTRIBUTING.md`, public issues and pull requests |
| Change control and releases | Public Git history, unique SemVer tags, `CHANGELOG.md`, immutable release workflow and release notes |
| Bug and vulnerability reporting | Public issue forms, `SECURITY.md`, private vulnerability reporting and the maintainer runbook |
| Build and tests | `Makefile`, public CI, Go tests, race tests, Helm validation, kind lifecycle and clean-consumer verification |
| Secure development | Threat model, tenant-isolation review, `govulncheck`, Trivy, secret scanning, push protection and dependency policy |
| Delivery integrity | HTTPS/SSH distribution, checksums, signatures, attestations, SBOMs and immutable image/chart digests |

Before marking the Passing target met, review every required questionnaire item,
provide its exact URL or allowed justification, and confirm that report-response
criteria are supported by the repository's actual issue history. Any unmet
required criterion remains a v1 blocker; it must not be marked `N/A` merely to
obtain a badge.

## Closure record

| Evidence | Result | Remaining action |
|---|---|---|
| Scorecard | Published 8.0 for merged commit `11dda186`; all six release checks meet their thresholds | Re-check before each release and close regressions under the documented policy |
| Private reporting | Primary receipt and `@legolas296` backup handover passed through two closed synthetic advisories without publication, CVE or private fork | Repeat after a material reporting-policy or maintainer-access change |
| Best Practices | Public project 14259 created; 67 answers staged locally but not submitted; owner deferred submission until the public domain is live | Add the public domain, obtain explicit submission confirmation, verify the public Passing result, and record the achieved level before the v1 release gate passes |
