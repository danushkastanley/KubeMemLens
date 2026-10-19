# v1.0.1 release preparation

Prepared: 19 October 2026. This is the pre-tag record; signed manifests and
workflow results become the artefact authority after the candidate is frozen.

## Identity and status

| Item | Status |
| --- | --- |
| Target stable version | `v1.0.1` |
| Proposed candidate | `v1.0.1-rc.1`; exact-tag approval is pending |
| Reviewed implementation baseline | `681a05095c1562f15c1862b71c471cdf0b2e38cc` on `main` |
| Release metadata | Prepared for review; the final candidate must use the merged preparation commit |
| Candidate and stable publication | Not started; neither target tag exists at the preparation readback |
| Release decision | Blocked by current Scorecard thresholds; see the dated readback below |
| Rollback reference | Published `v1.0.0`; a rollback restores its older security dependencies |
| Support scope | Existing standard EKS managed Linux profile; no new provider, runtime, terminal or scale claim |

## Included changes

- [#214](https://github.com/danushkastanley/KubeMemLens/pull/214) preserves source
  time, freshness and completeness in deep-mode `top` output.
- [#215](https://github.com/danushkastanley/KubeMemLens/pull/215) uses source
  timestamps and Pod continuity for offline rates, withholding unproven rates.
- [#216](https://github.com/danushkastanley/KubeMemLens/pull/216) updates Go to
  1.27.2 and x/net to 0.60.0 across the three modules and their builders.
- [#213](https://github.com/danushkastanley/KubeMemLens/pull/213) patches Moby
  user-database parsing in the optional worker.
- [#200](https://github.com/danushkastanley/KubeMemLens/pull/200),
  [#211](https://github.com/danushkastanley/KubeMemLens/pull/211) and
  [#112](https://github.com/danushkastanley/KubeMemLens/pull/112) align Kubernetes
  0.37.1, Bubble Tea 2.1.0 and x/time 0.16.0, including the nested modules.
- [#217](https://github.com/danushkastanley/KubeMemLens/pull/217) and the rollback
  repair in #211 preserve sub-second recovery measurements, retain redacted
  probe errors and wait for aggregation readiness before the rollback assertion.
- The reviewed workflow updates retain immutable action pins for Docker builds,
  Buildx, upload-artifact, Syft installation and CodeQL.

## Compatibility, upgrade and rollback

This is a patch to the standard 1.0 release line. It adds no service endpoint,
permission, host mount, stored-data migration or default-enabled profile. The
separate eBPF extension remains a development feature.

Deep-mode `top` CSV output appends `captured_at`, `freshness` and `completeness`;
JSON and YAML also carry that metadata. Read columns by name. Offline comparison
output omits the rate column when identity or a forward source interval is
unproven, or either observation is stale. Redacted captures do not contain the
Pod UID needed to establish continuity. Existing captures remain readable.

Use the versioned commands in the [installation guide](installation.md) only
after the matching release is published. Upgrade with reviewed values and the
signed image digest. Helm upgrade and rollback restart the collector and reset
its in-memory history. If rollback is required, use the previously recorded
Helm revision and verify fresh collection as described in the
[reliability runbook](runbooks/reliability.md#roll-back). The v1.0.0 rollback
reference contains the older dependency versions and is not a security fix.

The [upstream Kubernetes release window](https://kubernetes.io/releases/) was
read on 19 October 2026 and still comprises 1.35, 1.36 and 1.37. The pinned kind
matrix covers those minors. Historical EKS, scale and terminal evidence keeps
its original artefact and environment boundaries; it is not new qualification
of the prospective v1.0.1 bytes.

## Verification and remaining gates

The implementation PRs passed their required CI before merge. Two isolated
fixture failures passed unchanged reruns: a terminal-pane timeout in #200 and
a kind Node-readiness failure during #112 setup. Their PR records preserve the
initial failures; neither is described as a confirmed application-code fix.

The live release-environment, tag-ruleset and release-immutability readback
passed on 19 October 2026. Self-review is disabled for the release environment.
The authenticated readback found no open Dependabot alerts, unpublished private
advisories, secret-scanning alerts, CodeQL findings or repository ProjectV2
projects. The public OpenSSF Best Practices API still reports `passing`; this
is a self-assessment, not certification. These results do not clear the
Scorecard release thresholds.

### Current release blockers

The [Scorecard run on implementation baseline 681a050](https://github.com/danushkastanley/KubeMemLens/actions/runs/39172421856)
completed successfully, but its findings fail the existing
[release thresholds](repository-security.md#openssf-thresholds):

| Check | Observed | Required | Decision |
| --- | --- | --- | --- |
| Overall | 8.0 | At least 7.0 | Pass |
| Branch-Protection | 4 | At least 8 until the proposed amendment is merged; then at least 4 plus live controls | Amendment prepared for review at the maintainer's request; no live settings changed |
| Vulnerabilities | 4, six advisories | At least 7 and no unreviewed advisory-count regression | Blocked; requires a reviewed remediation or closure plan |
| Token-Permissions, Dangerous-Workflow, Pinned-Dependencies | 10 each | At least 8 each | Pass |
| Signed-Releases | 8 | At least 8 | Pass |

Code-Review is also 0; it is an advisory signal rather than a separate numerical
release threshold. On 19 October the maintainer selected retaining checks-only
PRs and requested an explicit amendment for review. The
[proposed policy](repository-security.md#proposed-amendment-19-october-2026) and
its executable predicate lower only the Branch-Protection minimum to 4 while
retaining live-control verification. This proposal takes effect after review
and merge. Live repository settings, the vulnerability threshold, independent
release approval and the adopter decision remain unchanged.

The six reported advisory IDs are `GO-2026-6094` (CEL), `GO-2026-6107` (etcd),
`GO-2026-5932` (OpenPGP), and `GO-2026-5064`, `GO-2026-5338`, `GO-2026-5622`
(containerd). The root module's fresh `govulncheck` reports zero reachable
vulnerabilities, two affected imported packages and one affected module. That
result does not clear the repository-wide module findings or the release gate.
The trace preflight also found zero reachable vulnerabilities. A separate
Linux/amd64 worker scan found zero reachable or imported-package findings and
the OpenPGP module advisory only; this is static analysis, not a worker runtime
qualification.
The [Go vulnerability database](https://vuln.go.dev/) lists fixes for CEL at
0.30.0 and etcd client/pkg at 3.7.1; it lists no fixed containerd v1 or OpenPGP
version. Containerd v1 appears in the separate worker module's dependency graph.
A version-only update cannot be assumed to resolve every finding.

### Preparation checks

- Helm strict lint, default rendering, unsupported-replica and unknown-value
  rejection passed. The pinned kubeconform validator accepted all 27 rendered
  resources with zero errors, invalid resources or skips.
- All 172 tracked/prepared Markdown files passed the local-file-link check.
  Historical version references retain their original evidence boundaries.
- The candidate workflow's version and documentation assertions, support
  contract and release integrity contract passed for 1.0.1 / 1.0.1-rc.1.
- Local `make check` passed the trace preflight, then stopped because the
  eBPF worker imports Linux-only SDK code on this macOS host. The Linux worker
  check remains required; this attempt is not a passing full release check.

Release-preparation checks and the final merged commit must be reviewed
separately before tagging.

Remaining gates:

1. Resolve the current Scorecard blockers, then review and merge the version,
   changelog and documentation preparation into
   `main`; complete the release checks on that exact commit.
2. Obtain approval naming `v1.0.1-rc.1`, then create only that annotated tag and
   dispatch the existing candidate workflow from it.
3. Obtain the independent protected-environment approvals. Inspect the candidate
   archives, image, chart, reproducibility, signatures, SBOMs, provenance,
   clean-consumer result and complete draft inventory before publication.
4. Record the v1.0.1 promotion decision. The v1.0.0-only adopter exception does
   not automatically apply: retain the documented adopter gate unless the
   maintainer explicitly records a patch-specific decision. Neither outcome is
   assumed by this preparation.
5. Obtain the exact stable-tag/publication approval, promote the same candidate
   bytes through the existing workflow, inspect the stable draft and verify its
   immutable published state.

The [release process](release-process.md) remains authoritative. Append reviewed
post-freeze evidence to release evidence issue #50 without rewriting historical
records. No release, provider qualification or successful publication is implied
by this preparation document.
