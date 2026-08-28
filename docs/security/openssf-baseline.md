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
`a897b962b2fc5772804eac93d7b7c44359d4c51c`, scored 8.3, and also passed every
release threshold. The pinned, least-privilege workflow runs on `main` and
weekly, publishes the signed result, retains SARIF for five days, and uploads
findings to GitHub code scanning.

Accepted workflow: <https://github.com/danushkastanley/KubeMemLens/actions/runs/33079572322>

Latest accepted workflow: <https://github.com/danushkastanley/KubeMemLens/actions/runs/33111267079>

| Release check | Local baseline | First published | Latest main | Reason |
|---|---:|---:|---:|---|
| Branch-Protection | 0 | 8 | 8 | The current ruleset has no bypass actors and requires one CODEOWNER approval, stale-review dismissal, last-push approval, resolved conversations and seven strict checks. Scorecard still records the one-reviewer limitation. |
| Token-Permissions | 10 | 10 | 10 | Workflow tokens follow least privilege |
| Dangerous-Workflow | 10 | 10 | 10 | No dangerous workflow pattern detected |
| Pinned-Dependencies | 10 | 10 | 10 | All workflow dependencies are pinned by full commit SHA |
| Vulnerabilities | 7 | 7 | 7 | Three dependency advisories remain unreachable; `govulncheck` found zero affected symbols |
| Signed-Releases | 8 | 8 | 8 | Both detected release artefacts have signed subjects |

Other closure work from the local baseline is now visible in the published
result: Dependency-Update-Tool is 10, SAST is 9, and Code-Review is 5 after five
of the ten changesets sampled by Scorecard had independent approval. The
Scorecard run predates the Passing submission and still records
CII-Best-Practices as 2. These are transparent signals, not release-threshold
waivers.

The three dependency results were `GO-2026-6094`, `GO-2026-6107`, and
`GO-2026-5932`. `govulncheck v1.1.4 -show verbose ./...` reported zero affected
symbols. The current Go 1.27 gate uses `govulncheck` v1.7.0 and reports the same
zero-reachable result: two vulnerable imported packages are not called by
KubeMemLens and the unmaintained `x/crypto/openpgp` package is not used. The
dependency policy still requires review on every change in reachability or
upstream guidance.

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

The submitted record keeps `version_tags`, `test_most` and `dynamic_analysis`
as `Unmet`, and uses `N/A` only where the criterion permits it. Passing does not
upgrade those suggested criteria or widen the release support contract.

## Closure record

| Evidence | Result | Remaining action |
|---|---|---|
| Scorecard | Latest signed result is 8.3 for merged commit `a897b962b2fc5772804eac93d7b7c44359d4c51c`; all six release checks meet their thresholds | Re-check before each release and close regressions under the documented policy |
| Private reporting | Primary receipt and `@legolas296` backup handover passed through two closed synthetic advisories without publication, CVE or private fork | Repeat after a material reporting-policy or maintainer-access change |
| Best Practices | Public project 14259 uses `https://kubememlens.com`, reports `passing`, and reached 100% of required Passing criteria on 28 August 2026 | Keep the public answers aligned with repository and live project evidence; do not describe the self-assessment as certification |
