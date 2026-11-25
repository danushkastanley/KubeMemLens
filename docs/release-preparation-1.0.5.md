# v1.0.5 release preparation

Prepared: 25 November 2026. Signed manifests and workflow results become the
artefact authority once a candidate tag freezes the source; this record does
not publish a release.

## Identity and scope

| Item | Status |
| --- | --- |
| Target stable version | `v1.0.5` |
| Proposed candidate | `v1.0.5-rc.1`; exact-tag approval remains required |
| Implementation baseline | `9109ff2` on `main` (82 merged pull requests since `v1.0.4`, #248 to #423), plus this preparation update |
| Candidate source | Must be the final reviewed preparation commit merged to `main` |
| Publication | No candidate or stable release published |
| Rollback reference | Published `v1.0.4` |
| Support | Existing standard EKS managed Linux scope; no new provider, runtime, terminal or scale qualification |

## Included changes

The [changelog](../CHANGELOG.md) lists every user-visible change. In groups:

- **Audit defects.** The November 2026 codebase audit's defects in the agent,
  collector, CLI, terminal interface and chart, #248 to #381. The largest are
  the bounded snapshot decode (#248), the metrics response budget (#249),
  unusable `memory.pressure` (#326) and the per-producer ingestion limit (#341).
- **Changed defaults.** The kubeconfig context namespace (#402), an explicit
  choice for the plaintext client path (#399), authenticated ingestion as the
  collector binary's default (#397), `410 Expired` for old continue tokens
  (#403), the kubelet-only cgroup walk (#401), `GOMEMLIMIT` on the agent (#400)
  and exit code 130 on Ctrl+C (#406).
- **Serving certificate.** Bootstrap-owned serving TLS as an install mode
  (#385, ADR 0028) and weekly scheduled rotation with an expiry gauge, alert
  and runbook (#398, ADR 0030).
- **Chart.** Agent priority class, update strategy, annotations, affinity,
  pull secrets and opt-in probes (#400); the Pod Security level label and a
  chart icon (#408); refused value combinations (#342, #344, #381).
- **Go 1.27.4** across all three modules and pinned builders for GO-2026-6859
  (#325), with the dependency updates in #357 and #369 to #373.
- **Trace extension.** Defect fixes (#387, #390 to #393, #395) and help text
  that says the extension is separately built (#423, ADR 0031). The extension
  is in no release artefact.
- **Release and checks.** The Krew index version bump is requested when a
  stable release is published (#389); `v1.0.5` would be the first release to
  use it. `make check` and CI run the same checks (#407), every fuzz target
  runs nightly (#415), and Dependabot covers more of the tree (#404).

## Compatibility and rollback

This patch introduces no capture-format or stored data migration. It does
change behaviour that scripts and installs can depend on:

- Commands without `-n` use the kubeconfig context's namespace.
- `MEMLENS_COLLECTOR_URL` alone no longer selects unauthenticated HTTP.
- A collector binary started by hand, without flags, needs its serving
  certificate files or `--ingestion-mode=legacy`. Chart installs are unaffected.
- A continue token held across the upgrade returns `410 Expired`; the list
  starts again.
- An upgrade adds the `kube-memlens-cert-rotation` CronJob with its
  ServiceAccount, Role, ClusterRole and their two bindings, including an
  upgrade that reuses stored values. Set
  `extensionTLS.rotation.enabled=false` to opt out. The default render grows
  from 27 to 33 resources.
- On nodes that also run containers outside Kubernetes, the unmapped count
  and mapping coverage change.

The [upstream Kubernetes support window](https://kubernetes.io/releases/) on
25 November 2026 is 1.35, 1.36 and 1.37, as for v1.0.4. The CI kind matrix
covers the three supported minors.

Helm upgrade and rollback restart the collector and reset its in-memory
history. Follow the recorded Helm revision and fresh-data checks in the
[reliability runbook](runbooks/reliability.md#roll-back). A rollback to v1.0.4
removes the rotation CronJob and its account and restores the earlier
defaults. A continue token issued by one version is refused by the other.

## Verification

The following preparation checks completed on 25 November 2026. Unless a
commit is named, they ran against this preparation branch.

- `make -k check` ran every target in a clean clone of `9109ff2`. Unit, race,
  coverage, vet, build, formatting, `govulncheck`, incident-session,
  memory-history, replica, trace release and chart, trace preflight,
  recommendation evaluation, support, scale, provider, terminal, release and
  community targets passed. Total statement coverage was 74.9%, against 73.9%
  for v1.0.4.
- `check-trace-worker` did not pass locally: the optional worker's SDK imports
  Linux-only packages, so its race tests cannot build on macOS, as for v1.0.2
  to v1.0.4. The Linux `Trace worker checks` workflow passed on `9109ff2`.
- `make check-release-contract check-support-contract check-community-contract`
  passed with the version changes in this update.
- Fresh `govulncheck` v1.7.0 scans of the standard, trace and Linux/amd64
  worker modules reported no reachable vulnerabilities and no affected
  imported packages. Each retains the module-only `GO-2026-5932` OpenPGP
  advisory in `x/crypto`, unchanged since v1.0.3. No finding is suppressed.
- All 14 steps of the CI `Helm checks` job passed when run locally, including
  Helm 3.18.4 strict lint. The default render uses the `1.0.5` image, and the
  pinned kubeconform validator accepted all 33 rendered resources with no
  errors or skips.
- The pinned Trivy 0.72.0 configuration and secret scans of the tracked files,
  and the high and critical vulnerability scan of a freshly built runtime
  image, each exited 0. The image runs as UID/GID 65532 with the expected
  source and licence labels. Untracked local directories, including a local
  copy of the optional worker SDK under `prototype/trace/worker/.sdk/`, were
  excluded, matching CI.
- All 635 local links in 189 Markdown files, including anchors, resolve.
- Live release-environment, tag-ruleset and immutability checks
  (`hack/release/check_repository_settings.sh`) passed. Dependabot, secret
  scanning and code scanning had no open alerts.
- The published Scorecard readback for `9109ff2` on 25 November 2026 was 9.1
  overall, against 8.8 for v1.0.4. Vulnerabilities scored 9, listing only
  `GO-2026-5932`. Branch-Protection scored 3, unchanged.
- On `main` at `9109ff2`, CodeQL, Trace worker checks and Scorecard passed. CI
  and Node-context verification were still running when this record was
  written; gate 1 below requires them on the final commit.
- `deploy/krew/memlens.yaml.tmpl` is unchanged since `v1.0.4`, so the Krew
  index pull request should change only the version, address and checksum
  lines.

Not verified in this preparation:

- The full kind end-to-end run on the merged tree as one unit. It ran for each
  behaviour change on its own branch before that pull request merged; the CI
  kind matrix runs it on `main`.
- An upgrade from a published `v1.0.4` install to this chart. Upgrade and
  rollback were exercised within one build only.
- The Krew index workflow on a real release. It has never run on one.
- `extensionTLS.source=bootstrap` under Argo CD or Flux.
- Thirteen open Dependabot pull requests, #409 to #414 and #416 to #422, are
  not part of this release. None changes a released artefact.
- The weekly image-digest report names two moved tags in a qualification
  script and the trace release assembler. Neither is in a release artefact.

## Remaining release gates

1. Review and merge this preparation update through the normal protected PR
   path. Complete required CI on the exact final `main` commit; that commit is
   the candidate source.
2. Record the v1.0.5 adopter decision on
   [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50).
   The v1.0.3-only exception does not carry over automatically.
3. With the maintainer's approval naming the exact tag, create the annotated
   `v1.0.5-rc.1` tag on the merged commit and dispatch the candidate workflow
   from it. The stable tag must not exist yet.
4. Check the generated candidate prerelease notes. Obtain independent
   protected-environment approvals and verify the candidate archives, image,
   chart, reproducibility, signatures, SBOMs, provenance, clean-consumer
   lifecycle and full draft inventory before publication. Confirm the shipped
   README files name no candidate.
5. Obtain exact stable-tag and publication approval and promote the same
   candidate bytes through the existing workflow. Verify the stable draft and
   immutable publication before describing v1.0.5 as released.
6. After publication, confirm that the
   [Krew index update](release-process.md#krew-index-update) workflow
   succeeded and that the pull request appeared on krew-index. Its manual
   fallback is unchanged.

The [release process](release-process.md) remains authoritative. Append
post-freeze evidence and decisions to release evidence issue #50 without
rewriting historical records.
