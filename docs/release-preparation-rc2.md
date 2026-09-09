# RC2 release preparation

Prepared 9 September 2026. Status: locally verified source preparation for review;
release approval pending. Current hosted verification is recorded on
[preparation PR #74](https://github.com/danushkastanley/KubeMemLens/pull/74).

## Identity

- Proposed candidate: `v1.0.0-rc.2`.
- Prospective stable product/chart identity: `1.0.0`, retained by design.
- Candidate repositories after approval: `ghcr.io/danushkastanley/candidates/1.0.0-rc.2/`.
- Existing public candidate: [`v1.0.0-rc.1`](https://github.com/danushkastanley/KubeMemLens/releases/tag/v1.0.0-rc.1).
- No RC2 tag, release, digest or reproducibility result is asserted here.
- Stable `v1.0.0` remains deferred. RC1's approval in [issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50) does not approve RC2 or stable publication.

## Review of all open PRs

The initial readback contained 12 open PRs, all from Dependabot. Included updates
are consolidated on the preparation branch. Original PRs remain open pending
merge of the reviewed replacement.

| PR | Update | Decision |
| --- | --- | --- |
| [#62](https://github.com/danushkastanley/KubeMemLens/pull/62) | component-base 0.37.0 | Defer. Mixed Kubernetes minor versions fail compilation. |
| [#63](https://github.com/danushkastanley/KubeMemLens/pull/63) | apiserver 0.37.0 | Defer. Its coordinated graph needs ConditionsAwareAuthorize and the new union.New interface. Requires a separate authentication migration and isolation review. |
| [#64](https://github.com/danushkastanley/KubeMemLens/pull/64) | x/time 0.15.0 | Include. Upstream changes only its minimum Go directive; this project already exceeds it. |
| [#65](https://github.com/danushkastanley/KubeMemLens/pull/65) | client-go 0.37.0 | Defer with the Kubernetes family. CI proves incompatible scheme validation signatures with apiserver 0.36.4. |
| [#66](https://github.com/danushkastanley/KubeMemLens/pull/66) | apimachinery 0.37.0 | Defer with the Kubernetes family. Do not merge a partial minor upgrade. |
| [#67](https://github.com/danushkastanley/KubeMemLens/pull/67) | CodeQL upload-sarif 4.37.9 | Include together with all CodeQL actions. |
| [#68](https://github.com/danushkastanley/KubeMemLens/pull/68) | Syft installer action 0.24.2 | Include the immutable action commit; retain explicit Syft 1.49.0. |
| [#69](https://github.com/danushkastanley/KubeMemLens/pull/69) | CodeQL analyse 4.37.9 | Include as a group. Individual PRs mix action configuration versions. |
| [#70](https://github.com/danushkastanley/KubeMemLens/pull/70) | CodeQL autobuild 4.37.9 | Include as a group. |
| [#71](https://github.com/danushkastanley/KubeMemLens/pull/71) | CodeQL init 4.37.9 | Include as a group. CI reports configuration 4.37.9 with runtime 4.37.8. |
| [#72](https://github.com/danushkastanley/KubeMemLens/pull/72) | gRPC 1.83.1 | Supersede with 1.83.2. Alert #2 requires at least 1.83.1, but the combined image scan found CVE-2026-84445 in that version. Upstream 1.83.2 fixes it. |
| [#73](https://github.com/danushkastanley/KubeMemLens/pull/73) | Go image 1.27.1 | Include the pinned image digest and align go.mod. Its Go-check failure came from dated provider-review fixtures, now given a fixed test clock. |

Retaining Kubernetes libraries at 0.36.4 does not change the supported cluster
minor matrix. Their 0.37.0 migration must test the new authoriser semantics and
tenant isolation before reconsideration.

## Upstream review

- [Go 1.27.1 release history](https://go.dev/doc/devel/release#go1.27): compiler, runtime and standard-library fixes. The container manifest matches Dependabot's pinned digest.
- [CodeQL action 4.37.9](https://github.com/github/codeql-action/releases/tag/v4.37.9): tag resolves to `cdf488f595d80d6e07e03d4674febd5ab45fa938`.
- [Syft installer action 0.24.2](https://github.com/anchore/sbom-action/releases/tag/v0.24.2): immutable release commit `3ad7283483fc7af8ff2b4ea19663c2d5ca935e26`.
- [gRPC 1.83.1 release](https://github.com/grpc/grpc-go/releases/tag/v1.83.1): transport buffering and xDS RBAC fixes.
- [gRPC 1.83.2 release](https://github.com/grpc/grpc-go/releases/tag/v1.83.2): rejects requests missing both authority and Host headers. This supersedes Dependabot's proposed patch; its module also requires x/net 0.58.0.
- [x/time comparison](https://github.com/golang/time/compare/v0.14.0...v0.15.0).
- [etcd TLS-handshake advisory](https://pkg.go.dev/vuln/GO-2026-6107): update the three API/client modules to 3.6.14 together. This does not deploy or upgrade a cluster's etcd server.
- [x/crypto SSH advisory](https://pkg.go.dev/vuln/GO-2026-6354) and [related channel advisory](https://pkg.go.dev/vuln/GO-2026-6355): update to 0.56.0.
- [Kubernetes maintained releases](https://kubernetes.io/releases/): recheck before freezing the candidate.

## Verification

The combined Go 1.27.1, gRPC 1.83.2, etcd client 3.6.14 and x/crypto 0.56.0 preparation passed `make check`, including
all Go tests, the race suite, 64.3% statement coverage, vet, vulnerability
reachability and builds. Module
verification, actionlint 1.7.7, strict Helm lint, kubeconform validation of all
24 rendered resources and rejection of collector.replicas=2 also passed.
Hosted CI must verify the combined image and all three Kubernetes lanes before
merge; use PR #74's checks for the latest result.
The final dependency build passed all 11 Linux terminal rows and four macOS
pseudo-terminal exit modes. These are bounded checks, not a new soak or macOS
emulator qualification. All six Linux/macOS/Windows amd64/arm64 CLI builds
passed. Changed Go modules retain Apache-2.0 or Go's BSD-style licence terms.
Live release-environment, tag-protection and
immutable-release settings passed readback. Initial alert readback found one open Dependabot
alert, zero secret-scanning alerts and three Scorecard posture entries, with
no open CodeQL-origin findings.

The first combined [CI run](https://github.com/danushkastanley/KubeMemLens/actions/runs/34308891490)
failed the image gate on CVE-2026-84445. No suppression was added: gRPC was
advanced to 1.83.2. The subsequent whole-dependency review also patched
etcd/client/pkg/v3 (GO-2026-6107) and SSH module findings (GO-2026-6354,
GO-2026-6355). The final local verbose scan reports no reachable vulnerable
calls, one imported-package finding for cel-go (GO-2026-6094), and the unused
OpenPGP module finding (GO-2026-5932), for which no upstream fix is available.
This is reachability triage, not an advisory-free dependency graph.

CEL 0.30.0 passed an isolated compilation/extension-test trial, but its release
also changes expression validation, cost accounting and evaluation behaviour.
Its broader compatibility review is deferred alongside the Kubernetes library
migration; the vulnerable NativeTypes/ParseStructTag calls are not reachable
according to the final scan. OpenPGP is not imported by this application.

Live community settings passed the maintainer, review, token, tag, environment
and secret-protection checks, then stopped at the published Scorecard
vulnerability threshold. The 5 September main-branch report scores 8.3 overall
but 4/10 for six dependency advisories, below the required 7/10. Best Practices
still reports Passing at 100%. Merge the verified dependency fixes and obtain
a fresh published Scorecard before candidate approval; do not lower the gate.

Candidate documentation checks now recognise RC2's explicitly unpublished
chart/image destinations while preserving working RC1 commands. Both release
SBOM validators require the installed Syft 1.49.0; a contract assertion prevents
the stale archive-validator version from returning.

## Intermittent Helm hook finding

The [first final-dependency CI run](https://github.com/danushkastanley/KubeMemLens/actions/runs/34309698394)
passed the supply-chain, Go, Helm, CodeQL and Kubernetes 1.36 checks, but the
Kubernetes 1.35 connection-test Pod failed immediately after rollback.
Install and upgrade tests in that lane passed. Issue #50 records the same
post-rollback symptom during RC1 verification, so this predates RC2.

A fresh local Kubernetes 1.35.5 lifecycle and 30 isolated connection-hook
repetitions passed using Helm 3.18.4 and the unchanged chart. Those arm64 runs
do not establish the cause of the hosted amd64 failure. CI now reports the
failed hook's phase and container exit reason without exporting credentials.
No resource limit, assertion or timeout was relaxed. A passing rerun is
verification evidence, not a claim that this intermittent failure is fixed.

## Publication gates

1. Merge the reviewed preparation with green Go, supply-chain, CodeQL, Helm
   and Kubernetes 1.35/1.36/1.37 lifecycle checks.
2. Resolve or triage remaining queues. The gRPC update must reach main before
   alert #2 can be considered remediated.
3. Recheck settings, public documents, tag absence and source cleanliness on
   the merged commit. Preserve all prior immutable release evidence.
4. Obtain approval naming `v1.0.0-rc.2`, then create its annotated tag and run
   the existing candidate workflow from that tag.
5. Review reproducibility, signatures, attestations, SBOMs, exact digests and
   clean-consumer install/test/upgrade/rollback/uninstall evidence.
6. Publish the verified draft through publish-candidate and append its signed
   identities to issue #50.

No managed-provider or live-density claim is widened. Evidence remains bound
to recorded versions; no cloud provisioning is part of this preparation.

## Rollback

Before publication, revert the preparation commits. After publication, use an
explicit prior authenticated chart/image revision or fix forward with a new
candidate. Never overwrite published artefacts. The gRPC advisory still applies
when returning to RC1. There is no persisted-data migration; collector restarts
lose in-memory evidence and rebuild it from fresh agent samples.
