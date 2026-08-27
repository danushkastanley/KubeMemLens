# OpenSSF Baseline

Status: pending first merged Scorecard run and Best Practices assessment
Recorded: 27 August 2026
Owner: maintainers named in `MAINTAINERS.md`

Best Practices project: https://www.bestpractices.dev/projects/14259

## Scorecard

The repository had no published OpenSSF Scorecard result before PROD-011. A
local OpenSSF Scorecard v5.5.0 run against merged commit `9bdd0aa` on 27 August
2026 established an overall baseline of 6.1. The ticket adds a pinned,
least-privilege workflow that runs on `main` and weekly, publishes the signed
result, retains SARIF for five days, and uploads findings to GitHub code scanning.

| Release check | Baseline | Reason |
|---|---:|---|
| Branch-Protection | 0 | The existing ruleset did not target `main` |
| Token-Permissions | 10 | Workflow tokens follow least privilege |
| Dangerous-Workflow | 10 | No dangerous workflow pattern detected |
| Pinned-Dependencies | 10 | All workflow dependencies were pinned |
| Vulnerabilities | 7 | Three dependency advisories were present; `govulncheck` found zero reachable vulnerabilities |
| Signed-Releases | 8 | All three detected release artefacts were signed |

Other closure work from the baseline is explicit: Dependency-Update-Tool was 0
before `.github/dependabot.yml`; SAST was 0 before the CodeQL workflow; and
CII-Best-Practices was 0 because no official assessment was registered. Code
review was 0 because the repository had one maintainer and no approved
changesets. These are not waived by the aggregate score.

The three dependency results were `GO-2026-6094`, `GO-2026-6107`, and
`GO-2026-5932`. `govulncheck v1.1.4 -show verbose ./...` reported zero affected
symbols: two vulnerable imported packages were not called by KubeMemLens and
the unmaintained `x/crypto/openpgp` package was not used. The dependency policy
still requires review on every change in reachability or upstream guidance.

Accept the first merged run only when it meets the thresholds in
[`docs/repository-security.md`](../repository-security.md). Record the workflow
URL, commit, date, overall score, six release-blocking check scores, and closure
owner for every gap here. Do not infer a pass from a workflow file alone.

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
| Scorecard | Local baseline 6.1; release checks recorded above | Merge PROD-011, inspect the signed `main` result, and confirm the ruleset, Dependabot and CodeQL improvements meet the threshold |
| Best Practices | Public project 14259 created; questionnaire pending at 0% | Complete the 67-criterion evidence-backed Passing questionnaire after PROD-011 reaches `main` |
