# Repository Security Baseline

This document records the public repository controls required for a KubeMemLens
release. Repository files describe intent; the GitHub API readback is the source
of truth for live settings.

## Required live controls

| Surface | Required state |
|---|---|
| `main` | Ruleset targets `refs/heads/main`; pull request required; required CI checks pass; force push and deletion blocked; review conversations resolved |
| Release tags | Active ruleset targets `refs/tags/v*`; creation, update, deletion, and non-fast-forward changes restricted to the release operator path |
| Release environment | Tag policy `v*`; primary and backup review ownership; self-review and ordinary admin bypass disabled where GitHub supports it |
| Actions | Default workflow token permission is read-only; workflows grant scoped job permissions; actions are pinned by full commit SHA |
| Vulnerability reporting | Private vulnerability reporting enabled; synthetic drill closed privately without publication or CVE request |
| Dependencies | Dependabot security updates enabled; bounded weekly Go, Actions, and Docker update configuration; no automatic merge |
| Secrets | Secret scanning and push protection enabled; open alerts have a named owner and closure record |
| Releases | Immutable releases enabled; build, publish, clean-consumer verification, and draft creation use separate least-privilege jobs |

Run `hack/release/check_repository_settings.sh` and the commands below before a
release candidate. Store a sanitised result, not the authentication token or raw
account data.

```sh
gh api repos/danushkastanley/KubeMemLens/rules/branches/main
gh api repos/danushkastanley/KubeMemLens/rulesets
gh api repos/danushkastanley/KubeMemLens/environments
gh api repos/danushkastanley/KubeMemLens/private-vulnerability-reporting
gh api repos/danushkastanley/KubeMemLens/immutable-releases
gh api repos/danushkastanley/KubeMemLens/actions/permissions/workflow
```

## OpenSSF thresholds

The scheduled Scorecard workflow publishes a signed result and uploads SARIF to
GitHub code scanning. For v1 and every later supported release:

- the overall Scorecard must be at least 7.0;
- Branch-Protection, Token-Permissions, Dangerous-Workflow,
  Pinned-Dependencies, and Signed-Releases must each be at least 8;
- Vulnerabilities must be at least 7, `govulncheck` must report zero reachable
  vulnerabilities, and the known advisory count must not regress without a
  reviewed closure plan; and
- a regression below either threshold is a release blocker, even when the
  overall score remains above 7.0.

The OpenSSF Best Practices target is the Passing badge. Record evidence for
every required criterion and use `N/A` only with the justification the criterion
requests. The badge is a documented project self-assessment, not certification
or a security guarantee.

The first accepted Scorecard result, Best Practices result, and closure plan for
any gap belong in [the OpenSSF baseline](security/openssf-baseline.md). Do not
place an unverified badge in the README.

## Review cadence

- Review open dependency, secret, code-scanning, and private advisory queues at
  least weekly while a supported line exists.
- Review rulesets, environment reviewers, Actions permissions, release
  immutability, owners, and OpenSSF thresholds before every release candidate.
- Review the security and support contacts whenever a maintainer changes role.
- Re-run the private-reporting drill after a material reporting-policy change.

These are project operating checks. They do not promise a response time to users.

## 28 August 2026 control readback

| Control | Readback |
|---|---|
| `main` ruleset | Targets `refs/heads/main`; one independent CODEOWNER approval is required, with stale-review dismissal, last-push separation, resolved conversations, strict CI and CodeQL checks, deletion protection, and force-push protection active. The temporary pull-request-only repository-administrator bypass used for the explicitly authorised maintenance queue was removed after the final readback PR merged. |
| Release tags and environment | `refs/tags/v*` restrictions and the tag-only environment policy are active; `@danushkastanley` and `@legolas296` are reviewers; self-review and admin bypass are disabled |
| Workflow token | Default `read`; workflows cannot approve pull requests |
| Vulnerability reporting | Enabled; primary drill `GHSA-9r58-62g3-2v7p` and backup-handover drill `GHSA-wfhm-45q5-8jf6` closed privately without a CVE, private fork, or publication |
| Dependencies and secrets | Dependabot security updates, secret scanning, and push protection enabled; zero open Dependabot or secret-scanning alerts at readback |
| Immutable releases | Enabled |
| Routing labels | `compatibility`, `diagnosis-feedback`, and `adopter-feedback` created alongside the existing bug, enhancement, question, and dependency labels |

This is a point-in-time record. CODEOWNER enforcement is active and the `main`
ruleset has no bypass actor. Release tags and the protected release environment
remain separately protected. Signed [Scorecard run 33193569645](https://github.com/danushkastanley/KubeMemLens/actions/runs/33193569645)
scored 8.9 on merged commit `050115351a31066e7bb64d14591e25d013e8f323`,
and OpenSSF Best Practices project 14259 has a verified Passing self-assessment.
The Vulnerabilities check is 7, Fuzzing is 10 and SAST is 10. `GO-2026-6303`
is no longer reported, and the accepted `govulncheck` run found zero affected
symbols among the three remaining advisories. V1-B07 is closed at this
pre-candidate readback.
`make check-community-settings` remains the final live gate for an exact candidate decision.
