# v1 candidate evidence and blocker register

Status: **NO-GO for RC; NO-GO for stable v1**

Snapshot date: 28 August 2026

Preparation baseline: `1f88026cca474b93739d3d7144014e4861d9cbad`

This register maps the PROD-012 release gates to evidence that can be reviewed
from the repository. It is a preparation record, not approval to create a tag,
publish a package or promote a release. An item remains open until its result is
bound to the exact candidate commit and immutable artefact digests.

## Candidate identity

No release candidate has been authorised or frozen.

| Field | Current value |
| --- | --- |
| Candidate version | Not assigned |
| Annotated RC tag | Not created |
| Source commit | Not frozen |
| CLI archive checksums | Not recorded |
| Image digest | Not recorded |
| Helm chart digest | Not recorded |
| Release workflow run | Not run for this candidate |
| Clean-consumer result | Not run for this candidate |
| v1 promotion identity | Candidate and promotion workflows prepared; no candidate run recorded |

The [release process](release-process.md),
[candidate workflow](../.github/workflows/candidate.yml),
[candidate draft publisher](../.github/workflows/publish-candidate.yml) and
[promotion workflow](../.github/workflows/promote.yml) define the prepared
prospective-GA build and no-rebuild promotion path. They do not supply the
missing candidate values above; only an authorised, reviewed run can do that.

## Release sequence

The pre-candidate gate covers every non-adopter safety and release prerequisite,
including OpenSSF Passing under the current recorded decision. It also requires
fresh explicit approval naming the exact RC tag. Candidate-bound values such as
digests, reproducibility results and clean-consumer output are produced by that
authorised run; they cannot be prerequisites for starting the same run.

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
| No existing P0/P1 blocker remains | Pre-candidate | Blocked | Close the pre-candidate items below and repeat the live issue, advisory, code-scanning, secret and dependency queue readback. |
| Claims and limitations are ready | Pre-candidate | Complete at snapshot | Kubernetes 1.37 GA, the current supported minor window, provider rows, terminal rows, scale wording, the [support contract](compatibility.md), [installation guide](installation.md), [README](../README.md) and [security policy](../SECURITY.md) are reconciled. No provider qualification was rerun or widened. Repeat the claim audit against the frozen candidate. |
| RC approval names the exact tag | Pre-candidate | Not approved | Close every pre-candidate blocker, then obtain fresh explicit approval for the exact annotated RC tag and candidate publication. |
| Candidate commit and artefacts frozen and reproducible | Candidate run | Pending | Record the annotated tag, commit, archive checksums, image digest, chart digest and two-build equality results from the authorised workflow. |
| Install, upgrade, rollback and uninstall | Candidate run | Pending | Retain the sanitised clean-consumer lifecycle result for the frozen candidate. |
| Three independent adopters | Post-candidate, pre-GA | Blocked, 0/3 | Link three complete, privacy-reviewed reports for the published immutable candidate. |
| Release notes are complete | Post-candidate, pre-GA | Pending | Add the final security, compatibility, upgrade and rollback information to the [changelog](../CHANGELOG.md) and draft notes. |
| Named go/no-go review | Post-candidate, pre-GA | Not approved | Use the [go/no-go record](v1-go-no-go.md). Naming a role is not an approval. |
| v1 promotes the verified RC artefacts | Stable promotion | Blocked | Record the candidate manifest, copy the exact image and chart digests, reuse the candidate archives and complete the post-copy clean-consumer run. |
| Post-release operations active | Pre-GA | Pending readback | Confirm the named response, monitoring and rollback owners and the live repository controls immediately before release. |

## Blocker register

Open blockers have no waiver path. V1-B01, V1-B03, V1-B04 and V1-B07 are closed
at this pre-candidate snapshot. The merged-main Scorecard Vulnerabilities result
has returned to the repository threshold, and the new fuzz targets are visible
in the signed result.
V1-B02 starts after candidate publication; V1-B05 and V1-B06 require
candidate-bound execution evidence. Stable v1 remains blocked until every
remaining blocker closes. If a requirement changes, record the product
decision and update the release contract before asking for go/no-go review.

| ID | Severity | Blocker | Evidence | Required closure |
| --- | --- | --- | --- | --- |
| V1-B01 | P0 | **Closed at the pre-candidate readback.** [KubeMemLens.com](https://kubememlens.com) is live, project 14259 records it as the homepage, and the public OpenSSF Best Practices record reached `passing` at `2026-08-28T13:50:42.757Z`. The 100% Passing result is a project self-assessment and retains three suggested `Unmet` criteria. | [OpenSSF baseline](security/openssf-baseline.md), [Passing assessment](security/openssf-passing-assessment.md), [public Passing record](https://www.bestpractices.dev/en/projects/14259/passing) | Re-read the public result against the frozen candidate and reopen this blocker if the level regresses or the answers no longer match repository evidence. |
| V1-B02 | P0 | No independent adopter has submitted candidate evidence. The live `adopter-feedback` issue readback returned 0 reports, against a requirement of 3. This is a post-candidate, pre-GA blocker. | [Feedback policy](community-feedback.md), [adopter form](../.github/ISSUE_TEMPLATE/adopter_feedback.yml) | Publish the authorised immutable candidate first, then obtain and privacy-review three reports that identify it and cover installation, useful diagnostic output, upgrade or rollback, and uninstall. |
| V1-B03 | P0 | **Closed at the pre-candidate readback.** Kubernetes 1.35.5, 1.36.1 and 1.37.0 passed on merged main commit `1f88026cca474b93739d3d7144014e4861d9cbad` in [CI run 33186603536](https://github.com/danushkastanley/KubeMemLens/actions/runs/33186603536). The support contract and checksum-pinned matrix cover the current upstream-supported window without widening or rerunning provider claims. | [CI matrix](../.github/workflows/ci.yml), [compatibility policy](compatibility.md), [release gate](release-process.md), [upstream release page](https://kubernetes.io/releases/) | Re-read the three lanes against the frozen candidate and reopen this blocker if a required lane, pin or claim changes. Provider evidence remains limited to its recorded versions. |
| V1-B04 | P1 | **Closed at the pre-candidate readback.** An authenticated repository `projectsV2(first: 20)` query returned `totalCount: 0` and an empty node list on 28 August 2026. There is no hidden ProjectV2 queue to reconcile; release blockers remain in this register and the public roadmap. | [Public roadmap](roadmap.md), [this blocker register](#blocker-register) | Repeat the authenticated query before the exact candidate decision. A newly created project or release-blocking item reopens this blocker. |
| V1-B05 | P0 | The exact RC-to-v1 promotion controls are implemented but unexercised. The candidate workflow creates prospective stable bytes in version-scoped candidate repositories, and the separate stable workflow is statically prohibited from rebuilding them. No signed candidate manifest or post-copy equality result exists yet. | [Candidate workflow](../.github/workflows/candidate.yml), [promotion workflow](../.github/workflows/promote.yml), [manifest validator](../hack/release/validate_candidate_manifest.sh), [release process](release-process.md) | Run the authorised candidate workflow, publish the reviewed prerelease, then prove that promotion reuses every archive byte and preserves the image and chart digests before changing this blocker. |
| V1-B06 | P0 | Deterministic build controls are implemented but have no candidate-bound run. GoReleaser uses commit-time metadata, chart packaging normalises order and metadata, and the candidate workflow compares two archive, image and chart builds. | [GoReleaser configuration](../.goreleaser.yml), [deterministic chart packager](../hack/release/package_chart.py), [candidate workflow](../.github/workflows/candidate.yml) | Record matching archive checksums, image digest and chart package checksum from the authorised candidate workflow. Treat any mismatch as a blocker; do not waive or rebuild around it. |
| V1-B07 | P1 | **Closed at the pre-candidate readback.** Zero dependency-update pull requests and zero Dependabot alerts remain. Signed [Scorecard run 33193569645](https://github.com/danushkastanley/KubeMemLens/actions/runs/33193569645) scored 8.9 on merged commit `050115351a31066e7bb64d14591e25d013e8f323`; Vulnerabilities is 7, Fuzzing is 10 and SAST is 10. `GO-2026-6303` is no longer reported. The accepted `govulncheck` v1.7.0 run found zero affected symbols among the three remaining advisories. | [Dependabot policy](../.github/dependabot.yml), [Scorecard workflow](../.github/workflows/scorecard.yml), [OpenSSF baseline](security/openssf-baseline.md), [queue policy](repository-security.md) | Repeat the dependency, advisory, code-scanning and signed Scorecard readback against the frozen candidate; reopen if the required threshold or advisory count regresses. |
| V1-B08 | P0 | No release tag or publication is authorised. This preparation branch does not grant that authority. RC and stable publication require separate fresh approvals. | [Release process](release-process.md), [tag validator](../hack/release/validate_tag.sh) | Close the pre-candidate blockers before seeking exact RC approval. After candidate and adopter evidence passes, complete the go/no-go record and obtain separate approval for the exact stable tag and publication. |

## Point-in-time live readback

These counts can change. Repeat them against the frozen candidate rather than
copying this snapshot into the final decision.

| Queue or control | 28 August 2026 readback | Release treatment |
| --- | --- | --- |
| Open public issues | 0 | Re-read labels and severity before go/no-go. An empty issue queue does not close the blockers in this register. |
| Adopter feedback issues | 0 | V1-B02 remains open for stable v1. Collection starts after candidate publication. |
| Dependabot pull requests | 0; all #26 through #36 were closed, and accepted changes were merged through replacement PRs #38 to #41 | The queue is empty and the signed Scorecard result restores the required Vulnerabilities score, so V1-B07 is closed at this snapshot. |
| Dependabot security alerts | 0 open | Re-read reachability before go/no-go. |
| Private security advisories | 0 open drafts | Re-read before go/no-go; closed synthetic drills are not findings. |
| Secret-scanning alerts | 0 open | Re-read before go/no-go. |
| Code-scanning alerts | 3 open Scorecard SARIF findings; 0 CodeQL-origin findings | Branch-Protection 8, Vulnerabilities 7 and Code-Review 5 are transparent posture signals, not source-code vulnerabilities. Fuzzing and SAST are now 10. Every explicit release threshold is met at this snapshot. |
| Repository ProjectV2 | Authenticated query returned `totalCount: 0` and no nodes | V1-B04 is closed at this snapshot; repeat before the exact candidate decision. |
| Accepted published Scorecard | 8.9 in [run 33193569645](https://github.com/danushkastanley/KubeMemLens/actions/runs/33193569645) on merged commit `050115351a31066e7bb64d14591e25d013e8f323` | Vulnerabilities is 7 and meets the [required threshold](repository-security.md); Fuzzing and SAST are 10; V1-B07 is closed at this snapshot. |
| Merged-main CI and CodeQL | [CI run 33186603536](https://github.com/danushkastanley/KubeMemLens/actions/runs/33186603536) and [CodeQL run 33186603528](https://github.com/danushkastanley/KubeMemLens/actions/runs/33186603528) passed on `1f88026cca474b93739d3d7144014e4861d9cbad` | Retain as pre-candidate evidence; repeat or confirm it for the frozen candidate. |
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
and the completed [go/no-go record](v1-go-no-go.md), including separate fresh
approval for the exact stable tag. The current pre-candidate decision remains
**NO-GO** because fresh approval naming the exact RC tag and candidate
publication is not recorded under V1-B08. V1-B01, V1-B03, V1-B04 and V1-B07
are closed only for the current pre-candidate snapshot and must be re-read after
the candidate is frozen. The 0/3 adopter count does not contribute to the RC
decision. Stable v1 remains **NO-GO** until every remaining item is linked and
reviewed.
