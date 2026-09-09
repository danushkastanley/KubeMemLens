# RC2 release preparation

Prepared 9 September 2026. Status: locally verified source preparation; hosted CI and release approval pending.

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
| [#72](https://github.com/danushkastanley/KubeMemLens/pull/72) | gRPC 1.83.1 | Include. Alert #2 reports high-severity HTTP/2 fragmented-frame memory exhaustion through 1.83.0. The prior 1.36 kind failure was a connection-hook failure; combined CI must pass. |
| [#73](https://github.com/danushkastanley/KubeMemLens/pull/73) | Go image 1.27.1 | Include the pinned image digest and align go.mod. Its Go-check failure came from dated provider-review fixtures, now given a fixed test clock. |

Retaining Kubernetes libraries at 0.36.4 does not change the supported cluster
minor matrix. Their 0.37.0 migration must test the new authoriser semantics and
tenant isolation before reconsideration.

## Upstream review

- [Go 1.27.1 release history](https://go.dev/doc/devel/release#go1.27): compiler, runtime and standard-library fixes. The container manifest matches Dependabot's pinned digest.
- [CodeQL action 4.37.9](https://github.com/github/codeql-action/releases/tag/v4.37.9): tag resolves to `cdf488f595d80d6e07e03d4674febd5ab45fa938`.
- [Syft installer action 0.24.2](https://github.com/anchore/sbom-action/releases/tag/v0.24.2): immutable release commit `3ad7283483fc7af8ff2b4ea19663c2d5ca935e26`.
- [gRPC 1.83.1 release](https://github.com/grpc/grpc-go/releases/tag/v1.83.1): transport buffering and xDS RBAC fixes.
- [x/time comparison](https://github.com/golang/time/compare/v0.14.0...v0.15.0).
- [Kubernetes maintained releases](https://kubernetes.io/releases/): recheck before freezing the candidate.

## Verification

The combined Go 1.27.1 preparation passed `make check`, including all Go tests,
the race suite, coverage, vet, vulnerability reachability and builds. Module
verification, actionlint 1.7.7, strict Helm lint, kubeconform validation of all
24 rendered resources and rejection of collector.replicas=2 also passed.
Hosted CI must still verify the combined image and all three Kubernetes lanes.
Earlier TUI-only terminal runs are historical; they do not verify the changed
Go and dependency build. Live release-environment, tag-protection and
immutable-release settings passed readback. Initial alert readback found one open Dependabot
alert, zero secret-scanning alerts and three Scorecard posture entries, with
no open CodeQL-origin findings.

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
