# v1 candidate evidence and blocker register

Status: **`v1.0.0-rc.1` approved as the first public candidate; publication pending; NO-GO for stable v1**

Snapshot date: 29 August 2026

Pre-alignment baseline: `c7eb76c0d79a3537ef24f2ea47b80ea8d0663c86`

This register maps the PROD-012 release gates to evidence that can be reviewed
from the repository. Two earlier candidate workflows stopped before publication
and created no public release or candidate package. On 29 August 2026, Danushka
Stanley superseded the earlier tag-retention decision and approved removing those
unpublished tags so the first successful public candidate can use the exact
`v1.0.0-rc.1` identity. This approval does not cover stable `v1.0.0`. The complete
failure history and later workflow evidence remain append-only in [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50).

## Candidate identity

The public candidate identity is approved but not yet frozen, built or published.

| Field | Current value |
| --- | --- |
| Candidate version | Approved `v1.0.0-rc.1` |
| Annotated RC tag | Pending recreation on the reviewed `main` commit |
| Source commit | Not frozen |
| CLI archive checksums | Not recorded |
| Image digest | Not recorded |
| Helm chart digest | Not recorded |
| Release workflow run | Earlier attempts [33230688425](https://github.com/danushkastanley/KubeMemLens/actions/runs/33230688425) and [33234835703](https://github.com/danushkastanley/KubeMemLens/actions/runs/33234835703) failed before publication; corrected RC1 not yet run |
| Clean-consumer result | Not run for this candidate |
| v1 promotion identity | Prospective stable `v1.0.0`; promotion remains unapproved |

The [release process](release-process.md),
[candidate workflow](../.github/workflows/candidate.yml),
[candidate draft publisher](../.github/workflows/publish-candidate.yml) and
[promotion workflow](../.github/workflows/promote.yml) define the prepared
prospective-GA build and no-rebuild promotion path. They do not supply the
missing candidate values above; only an authorised, reviewed run can do that.

## Release sequence

The pre-candidate gate covers every non-adopter safety and release prerequisite,
including OpenSSF Passing under the current recorded decision. Fresh approval
for the reset `v1.0.0-rc.1` identity and candidate publication is recorded.
Candidate-bound values such as digests, reproducibility results and clean-consumer
output are produced by the authorised run; they cannot be prerequisites for
starting the same run.

The published immutable candidate then starts the adopter path. Three accepted
reports are required before stable v1 promotion. The current 0/3 count is a
stable-release blocker, not a reason to test an unfrozen branch or prevent an
otherwise authorised candidate from being published.

## Entry evidence

Accepted earlier work is useful input, but it does not replace candidate-bound
verification.

| Area | Existing evidence | Candidate action |
| --- | --- | --- |
| Tenant isolation | The [tenant-isolation validation](security/tenant-isolation-validation.md) maps the adversarial suite and retained evidence. | Confirm the frozen candidate contains the accepted boundary and has no open P0 or P1 security finding. |
| Reliability | The [reliability contract](reliability.md) and [runbook](runbooks/reliability.md) define partial, stale, recovery and rollback behaviour. | Exercise the exact candidate through the required lifecycle and incident paths. |
| Scale | The [local 5,000-container result](qualification-results/rc-5000-local-kind-2026-08-26.md) records the qualified local profile. | Check whether the frozen candidate changes a scale-sensitive boundary. Re-run only if the claim changes or the existing evidence no longer applies. |
| Providers and runtimes | The [provider/runtime evidence index](qualification-results/provider-runtime-0.0.1-alpha.3-b878c14/README.md) and [support contract](compatibility.md) record qualified and unsupported rows. | Compare the candidate with every provider-sensitive boundary. Keep the claim narrow, or obtain separate approval for an opt-in requalification where the boundary changed. |
| Terminals | The [terminal evidence index](qualification-results/terminal-runtime-e631f20/README.md) records the Linux terminal rows and explicit macOS gaps. | Check whether the candidate changes terminal behaviour. Preserve the unqualified macOS rows unless new evidence exists. |
| Release integrity | The [release process](release-process.md) describes deterministic candidate builds, version-scoped candidate destinations, signed manifests, no-rebuild stable promotion and clean-consumer verification. | Complete the real RC workflow only after every pre-candidate blocker closes and fresh tag approval is recorded. |
| Repository operations | The [repository security record](repository-security.md), [OpenSSF baseline](security/openssf-baseline.md) and [private-reporting drill](security/reviews/private-reporting-drill-2026-08-27.md) record the accepted controls. | Re-read live settings, finding queues and OpenSSF results for the candidate. |
| Adopter feedback | The [feedback policy](community-feedback.md) and [issue form](../.github/ISSUE_TEMPLATE/adopter_feedback.yml) define a privacy-safe route. | After the immutable candidate is published, obtain three independent reports. The current count is zero and blocks stable v1 only. |

## PROD-012 gate readout

| Gate | Phase | State | Evidence needed to close |
| --- | --- | --- | --- |
| Dependency implementation and earlier evidence reviewed | Pre-candidate | Complete at snapshot | The PROD-010 candidate machinery and dependency queue were reviewed and merged through PRs #38 to #41. OpenSSF project 14259 now has a verified Passing result and V1-B01 is closed. The real candidate run remains candidate-stage evidence, not a prerequisite for itself. |
| No existing P0/P1 blocker remains | Pre-candidate | Complete at snapshot | Required CI, CodeQL, OpenSSF, dependency, advisory, secret, ProjectV2 and support-contract readbacks passed. Issue #50 is the intentional release evidence record, not an untriaged product finding. Repeat the queues on the frozen candidate. |
| Claims and limitations are ready | Pre-candidate | Complete at snapshot | Kubernetes 1.37 GA, the current supported minor window, provider rows, terminal rows, scale wording, the [support contract](compatibility.md), [installation guide](installation.md), [README](../README.md) and [security policy](../SECURITY.md) are reconciled. No provider qualification was rerun or widened. Repeat the claim audit against the frozen candidate. |
| RC approval names the exact tag | Pre-candidate | Complete | Danushka Stanley approved resetting the unpublished attempts and publishing the corrected exact `v1.0.0-rc.1` candidate on 29 August 2026. The protected release environment still requires its independent reviewer before write-capable jobs run. |
| Candidate commit and artefacts frozen and reproducible | Candidate run | Pending | Record the annotated tag, commit, archive checksums, image digest, chart digest and two-build equality results from the authorised workflow. |
| Install, upgrade, rollback and uninstall | Candidate run | Pending | Retain the sanitised clean-consumer lifecycle result for the frozen candidate. |
| Three independent adopters | Post-candidate, pre-GA | Blocked, 0/3 | Link three complete, privacy-reviewed reports for the published immutable candidate. |
| Release notes are complete | Pre-candidate | Complete at snapshot | The [changelog](../CHANGELOG.md), installation guide, compatibility contract, security policy and candidate/stable commands name the exact identities, upgrade boundary, rollback behaviour and unsupported profiles. Re-check the published draft against this frozen snapshot. |
| Named go/no-go review | Post-candidate, pre-GA | Not approved | Use the [go/no-go record](v1-go-no-go.md). Naming a role is not an approval. |
| v1 promotes the verified RC artefacts | Stable promotion | Blocked | Record the candidate manifest, copy the exact image and chart digests, reuse the candidate archives and complete the post-copy clean-consumer run. |
| Post-release operations active | Pre-GA | Pending readback | Confirm the named response, monitoring and rollback owners and the live repository controls immediately before release. |

## Blocker register

Open blockers have no waiver path. V1-B01, V1-B03, V1-B04, V1-B07 and the
candidate-approval half of V1-B08 are closed at this pre-candidate snapshot.
The merged-main Scorecard Vulnerabilities result has returned to the repository
threshold, and the new fuzz targets are visible in the signed result.
V1-B02 starts after candidate publication; V1-B05 and V1-B06 require
candidate-bound execution evidence. Stable v1 remains blocked until every
remaining blocker closes. If a requirement changes, record the product
decision and update the release contract before asking for go/no-go review.

| ID | Severity | Blocker | Evidence | Required closure |
| --- | --- | --- | --- | --- |
| V1-B01 | P0 | **Closed at the pre-candidate readback.** [KubeMemLens.com](https://kubememlens.com) is live, project 14259 records it as the homepage, and the public OpenSSF Best Practices record reached `passing` at `2026-08-28T13:50:42.757Z`. The 100% Passing result is a project self-assessment and retains three suggested `Unmet` criteria. | [OpenSSF baseline](security/openssf-baseline.md), [Passing assessment](security/openssf-passing-assessment.md), [public Passing record](https://www.bestpractices.dev/en/projects/14259/passing) | Re-read the public result against the frozen candidate and reopen this blocker if the level regresses or the answers no longer match repository evidence. |
| V1-B02 | P0 | No independent adopter has submitted candidate evidence. The live `adopter-feedback` issue readback returned 0 reports, against a requirement of 3. This is a post-candidate, pre-GA blocker. | [Feedback policy](community-feedback.md), [adopter form](../.github/ISSUE_TEMPLATE/adopter_feedback.yml) | Publish the authorised immutable candidate first, then obtain and privacy-review three reports that identify it and cover installation, useful diagnostic output, upgrade or rollback, and uninstall. |
| V1-B03 | P0 | **Closed at the pre-candidate readback.** Kubernetes 1.35.5, 1.36.1 and 1.37.0 passed on merged main commit `c7eb76c0d79a3537ef24f2ea47b80ea8d0663c86` in [CI run 33194300176](https://github.com/danushkastanley/KubeMemLens/actions/runs/33194300176), including the successful retry of the transient 1.35 lane. The support contract and checksum-pinned matrix cover the current upstream-supported window without widening or rerunning provider claims. | [CI matrix](../.github/workflows/ci.yml), [compatibility policy](compatibility.md), [release gate](release-process.md), [upstream release page](https://kubernetes.io/releases/) | Re-read the three lanes against the frozen candidate and reopen this blocker if a required lane, pin or claim changes. Provider evidence remains limited to its recorded versions. |
| V1-B04 | P1 | **Closed at the pre-candidate readback.** An authenticated repository `projectsV2(first: 20)` query returned `totalCount: 0` and an empty node list on 28 August 2026. There is no hidden ProjectV2 queue to reconcile; release blockers remain in this register and the public roadmap. | [Public roadmap](roadmap.md), [this blocker register](#blocker-register) | Repeat the authenticated query before the exact candidate decision. A newly created project or release-blocking item reopens this blocker. |
| V1-B05 | P0 | The exact RC-to-v1 promotion controls are implemented but unexercised. The candidate workflow creates prospective stable bytes in version-scoped candidate repositories, and the separate stable workflow is statically prohibited from rebuilding them. No signed candidate manifest or post-copy equality result exists yet. | [Candidate workflow](../.github/workflows/candidate.yml), [promotion workflow](../.github/workflows/promote.yml), [manifest validator](../hack/release/validate_candidate_manifest.sh), [release process](release-process.md) | Run the authorised candidate workflow, publish the reviewed prerelease, then prove that promotion reuses every archive byte and preserves the image and chart digests before changing this blocker. |
| V1-B06 | P0 | Deterministic build controls are implemented but have no candidate-bound run. GoReleaser uses commit-time metadata, chart packaging normalises order and metadata, and the candidate workflow compares two archive, image and chart builds. | [GoReleaser configuration](../.goreleaser.yml), [deterministic chart packager](../hack/release/package_chart.py), [candidate workflow](../.github/workflows/candidate.yml) | Record matching archive checksums, image digest and chart package checksum from the authorised candidate workflow. Treat any mismatch as a blocker; do not waive or rebuild around it. |
| V1-B07 | P1 | **Closed at the pre-candidate readback.** Zero dependency-update pull requests and zero Dependabot alerts remain. Signed [Scorecard run 33194300199](https://github.com/danushkastanley/KubeMemLens/actions/runs/33194300199) scored 8.8 on merged commit `c7eb76c0d79a3537ef24f2ea47b80ea8d0663c86`; Vulnerabilities is 7, Fuzzing is 10 and SAST is 10. `GO-2026-6303` is no longer reported. The accepted `govulncheck` v1.7.0 run found zero affected symbols among the three remaining advisories. | [Dependabot policy](../.github/dependabot.yml), [Scorecard workflow](../.github/workflows/scorecard.yml), [OpenSSF baseline](security/openssf-baseline.md), [queue policy](repository-security.md) | Repeat the dependency, advisory, code-scanning and signed Scorecard readback against the frozen candidate; reopen if the required threshold or advisory count regresses. |
| V1-B08 | P0 | **Candidate approval is closed; stable approval remains open.** Danushka Stanley approved removal of the unpublished failed tags, recreation of the exact `v1.0.0-rc.1` tag on corrected `main`, and candidate publication on 29 August 2026. Stable `v1.0.0` remains separately unapproved. | [Release process](release-process.md), [tag validator](../hack/release/validate_tag.sh), [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50) | Build and publish the approved `v1.0.0-rc.1` through the protected workflows. Stop before stable promotion until candidate and adopter evidence passes and separate stable approval is recorded. |

## Point-in-time live readback

These counts can change. Repeat them against the frozen candidate rather than
copying this snapshot into the final decision.

| Queue or control | 29 August 2026 readback | Release treatment |
| --- | --- | --- |
| Open public issues | 1 intentional `release-evidence` tracker, [issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50); 0 untriaged product issues | Append candidate and stable evidence to issue #50. Re-read every other issue label and severity before go/no-go. |
| Adopter feedback issues | 0 | V1-B02 remains open for stable v1. Collection starts after candidate publication. |
| Dependabot pull requests | 0; all #26 through #36 were closed, and accepted changes were merged through replacement PRs #38 to #41 | The queue is empty and the signed Scorecard result restores the required Vulnerabilities score, so V1-B07 is closed at this snapshot. |
| Dependabot security alerts | 0 open | Re-read reachability before go/no-go. |
| Private security advisories | 0 open drafts | Re-read before go/no-go; closed synthetic drills are not findings. |
| Secret-scanning alerts | 0 open | Re-read before go/no-go. |
| Code-scanning alerts | 3 open Scorecard SARIF findings; 0 CodeQL-origin findings | Branch-Protection 8, Vulnerabilities 7 and Code-Review 5 are transparent posture signals, not source-code vulnerabilities. Fuzzing and SAST are now 10. Every explicit release threshold is met at this snapshot. |
| Repository ProjectV2 | Authenticated query returned `totalCount: 0` and no nodes | V1-B04 is closed at this snapshot; repeat before the exact candidate decision. |
| Accepted published Scorecard | 8.8 in [run 33194300199](https://github.com/danushkastanley/KubeMemLens/actions/runs/33194300199) on merged commit `c7eb76c0d79a3537ef24f2ea47b80ea8d0663c86` | Vulnerabilities is 7 and meets the [required threshold](repository-security.md); Fuzzing and SAST are 10; V1-B07 is closed at this snapshot. |
| Merged-main CI and CodeQL | [CI run 33194300176](https://github.com/danushkastanley/KubeMemLens/actions/runs/33194300176) and [CodeQL run 33194300180](https://github.com/danushkastanley/KubeMemLens/actions/runs/33194300180) passed on `c7eb76c0d79a3537ef24f2ea47b80ea8d0663c86` | Retain as pre-candidate evidence; repeat or confirm it for the frozen candidate. |
| OpenSSF Best Practices | Project 14259 is `passing`; the required Passing criteria report 100% | V1-B01 is closed at this snapshot. The self-assessment retains three suggested `Unmet` criteria. |

## Candidate and stable evidence bundle

The authorised candidate run must first record:

- annotated tag, source commit and release workflow run;
- CLI archive names and SHA-256 checksums;
- image repository and immutable digest;
- chart repository, version and immutable digest;
- `release-subjects.txt` covered by the signed checksum and provenance set,
  with the associated SBOMs, signatures and attestations;
- isolated reproducibility comparison;
- clean-consumer verification and install, upgrade, rollback and uninstall result;
- current Kubernetes 1.35 through 1.37 lifecycle results;
- claim audit for provider, runtime, scale, terminal and unsupported rows;
- P0/P1, dependency, advisory, code-scanning, secret and OpenSSF queue readback.

After candidate publication, append three privacy-reviewed adopter summaries
and dated go/no-go reviews to [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50),
including separate fresh approval for the exact stable tag. The reset
`v1.0.0-rc.1` candidate is approved for publication through the protected
workflow. V1-B01, V1-B03, V1-B04 and V1-B07 are closed only for this snapshot
and must be re-read after the candidate is frozen. The 0/3 adopter count does
not contribute to the RC decision. Stable v1 remains **NO-GO** until every
remaining item is linked and reviewed.
