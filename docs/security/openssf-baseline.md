# OpenSSF Baseline

Status: OpenSSF Best Practices Passing self-assessment achieved
Recorded: 28 August 2026
Owner: maintainers named in `MAINTAINERS.md`

Best Practices project: https://www.bestpractices.dev/en/projects/14259/passing

The owner approved submission after <https://kubememlens.com> became the
long-lived public homepage. Project 14259 reached `passing` at
`2026-08-28T13:50:42.757Z`; the public record reports 100% of required Passing
criteria. This is a project self-assessment badge, not certification or a
guarantee that vulnerabilities are absent.

## Scorecard

The repository had no published OpenSSF Scorecard result before PROD-011. A
local OpenSSF Scorecard v5.5.0 run against merged commit `9bdd0aa` on 27 August
2026 established an overall baseline of 6.1. The first accepted published run
used merged commit `11dda186`, scored 8.0, and passed the release thresholds.
The latest accepted signed run used merged commit
`d40ffe07dcc99eba8ee58c721d0f0370d8960c7a` and scored 8.9. Its Vulnerabilities
check returned to the required score of 7 after `GO-2026-6303` was removed, and
the native Go fuzz targets raised Fuzzing from 0 to 10. The pinned,
least-privilege workflow runs on `main` and weekly, publishes the signed result,
retains SARIF for five days, and uploads findings to GitHub code scanning.

Accepted workflow: <https://github.com/danushkastanley/KubeMemLens/actions/runs/33079572322>

Latest accepted workflow: <https://github.com/danushkastanley/KubeMemLens/actions/runs/33192629650>

| Release check | Local baseline | First published | Latest main | Reason |
|---|---:|---:|---:|---|
| Branch-Protection | 0 | 8 | 8 | The accepted run observed the temporary pull-request-only repository-administrator bypass used for the authorised maintenance queue. That bypass was removed after the final readback PR merged; the ruleset otherwise requires one CODEOWNER approval, stale-review dismissal, last-push approval, resolved conversations and seven strict checks. Scorecard records the one-reviewer limitation. |
| Token-Permissions | 10 | 10 | 10 | Workflow tokens follow least privilege |
| Dangerous-Workflow | 10 | 10 | 10 | No dangerous workflow pattern detected |
| Pinned-Dependencies | 10 | 10 | 10 | All workflow dependencies are pinned by full commit SHA |
| Vulnerabilities | 7 | 7 | 7 | Scorecard records three dependency advisories. The accepted merged-main `govulncheck` run detected the same three and found zero affected symbols; `GO-2026-6303` is no longer reported |
| Signed-Releases | 8 | 8 | 8 | Both detected release artefacts have signed subjects |

Other closure work from the local baseline is now visible in the published
result: Dependency-Update-Tool is 10, SAST is 10, Fuzzing is 10,
CII-Best-Practices is 5, and Code-Review is 5 after seven of the twelve
changesets sampled by Scorecard had independent approval. These are transparent
signals, not release-threshold waivers.

The three Scorecard results are `GO-2026-6094`, `GO-2026-6107`, and
`GO-2026-5932`. The accepted merged-main Go 1.27 gate used `govulncheck` v1.7.0
and found zero affected symbols: two imported-package vulnerabilities and one
required-module vulnerability. The zero-affected result is not a general
security guarantee, but the signed Scorecard result now meets the explicit
Vulnerabilities threshold in
[`docs/repository-security.md`](../repository-security.md). V1-B07 is closed at
this pre-candidate readback.

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

The submitted record keeps `version_tags`, `test_most` and `dynamic_analysis`
as `Unmet`, and uses `N/A` only where the criterion permits it. Passing does not
upgrade those suggested criteria or widen the release support contract.

## Closure record

| Evidence | Result | Remaining action |
|---|---|---|
| Scorecard | Latest signed result is 8.9 for merged commit `d40ffe07dcc99eba8ee58c721d0f0370d8960c7a`; Vulnerabilities is 7, Fuzzing is 10 and SAST is 10 | Retain this signed pre-candidate result and repeat the queue readback against the frozen candidate |
| Private reporting | Primary receipt and `@legolas296` backup handover passed through two closed synthetic advisories without publication, CVE or private fork | Repeat after a material reporting-policy or maintainer-access change |
| Best Practices | Public project 14259 uses `https://kubememlens.com`, reports `passing`, and reached 100% of required Passing criteria on 28 August 2026 | Keep the public answers aligned with repository and live project evidence; do not describe the self-assessment as certification |
