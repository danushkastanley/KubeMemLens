# v1.0.4 release preparation

Prepared: 10 November 2026. Signed manifests and workflow results become the
artefact authority once a candidate tag freezes the source; this record does
not publish a release.

## Identity and scope

| Item | Status |
| --- | --- |
| Target stable version | `v1.0.4` |
| Proposed candidate | `v1.0.4-rc.1`; exact-tag approval remains required |
| Implementation baseline | `1e00096` on `main` (merged PRs #237 to #243, #245 and #246), plus this preparation update |
| Candidate source | Must be the final reviewed preparation commit merged to `main` |
| Publication | No candidate or stable release published |
| Rollback reference | Published `v1.0.3` |
| Support | Existing standard EKS managed Linux scope; no new provider, runtime, terminal or scale qualification |

## Included changes

- [#245](https://github.com/danushkastanley/KubeMemLens/pull/245): scrapeable
  metrics and alert runbook links.
  - `metrics/current` negotiates its representation from `Accept`. OpenMetrics
    or Prometheus text is served only when a supported text type is listed with
    a quality at least as high as JSON; a missing header, `*/*`,
    `application/json` and anything unrecognised keep the JSON object used by
    `kubectl get --raw` and the CLI. Authorisation, the
    `kube-memlens-metrics-reader` binding, size budget and cardinality
    guardrails are unchanged. The gap dated from the authenticated-reads change
    in `7375b42`.
  - The default `metrics.prometheusRule.runbookBaseURL` points at
    `danushkastanley/KubeMemLens`; the old repository URL returned 404.
  - `docs/metrics.md` documents a least-privilege Prometheus scrape through the
    API server, verified on 9 November 2026 with Prometheus 3.15.0 on a
    disposable kind Kubernetes 1.36.1 cluster.
- [#241](https://github.com/danushkastanley/KubeMemLens/pull/241): candidate
  install paths stay out of the README, installation guide and chart README,
  which the archives and chart package ship. `create_candidate_draft.sh` writes
  the candidate install command into the prerelease notes from the signed
  manifest, and the release contract rejects candidate paths or RC versions in
  those guides. `v1.0.4-rc.1` is the first candidate built this way.
- [#237](https://github.com/danushkastanley/KubeMemLens/pull/237): the Krew
  manifest caveats state only setup prerequisites.
- [#238](https://github.com/danushkastanley/KubeMemLens/pull/238),
  [#239](https://github.com/danushkastanley/KubeMemLens/pull/239) and
  [#240](https://github.com/danushkastanley/KubeMemLens/pull/240): pinned
  GitHub Actions updates (`github/codeql-action` 4.39.3,
  `sigstore/cosign-installer` 4.1.3, `docker/login-action` 4.8.0).
- [#246](https://github.com/danushkastanley/KubeMemLens/pull/246): CI skips the
  kind jobs when only the chart README changes. No product effect.
- [#242](https://github.com/danushkastanley/KubeMemLens/pull/242) and
  [#243](https://github.com/danushkastanley/KubeMemLens/pull/243): public
  roadmap and README demo GIF.

No Go module, builder image or runtime dependency changed since `v1.0.3`.

## Compatibility and rollback

This patch introduces no API, capture-format, permission, host-mount or stored
data migration. The `metrics/current` JSON object is unchanged for every
request that does not prefer exposition text. Scrapers configured as in
[Scrape with Prometheus](metrics.md#scrape-with-prometheus) start receiving
series; the optional `PrometheusRule` and Grafana dashboard, if enabled, start
receiving data. Responses now carry `Vary: Accept`.

The [upstream Kubernetes support window](https://kubernetes.io/releases/) on
10 November 2026 is 1.35, 1.36 and 1.37, as for v1.0.3. The CI kind matrix
already covers the three supported minors.

Helm upgrade and rollback restart the collector and reset its in-memory
history. Follow the recorded Helm revision and fresh-data checks in the
[reliability runbook](runbooks/reliability.md#roll-back). A rollback to v1.0.3
restores JSON-only metrics responses and the old runbook URL.

## Verification

The following preparation checks completed on 10 November 2026 against this
preparation branch:

- `make -k check` ran every target. Unit, race, coverage, vet, build,
  formatting, `govulncheck`, incident-session, trace release and chart,
  trace preflight, recommendation evaluation, support, scale, provider,
  terminal, release and community targets passed. Total statement coverage was
  73.9%, as for v1.0.3.
- `check-trace-worker` did not pass locally: the optional worker's SDK imports
  Linux-only packages, so its race tests cannot build on macOS, as for v1.0.2
  and v1.0.3. Its Linux dependency-boundary step passed; the Linux trace and
  worker CI jobs remain necessary. The provider contract passed on the
  committed chart edits.
- Fresh `govulncheck` v1.7.0 scans of the standard, trace and Linux/amd64
  worker modules reported no reachable vulnerabilities and no affected
  imported packages. Each retains the module-only `GO-2026-5932` OpenPGP
  advisory in `x/crypto`, unchanged since v1.0.3. No finding is suppressed.
- Helm 3.18.4 strict lint passed. The default render uses the `1.0.4` image and
  passes the test-hook security check, and the pinned kubeconform v0.7.0
  validator accepted all 27 rendered resources with no errors or skips. The
  schema rejected two collector replicas.
- The pinned Trivy 0.72.0 configuration and secret scans of the tracked files,
  and the high and critical vulnerability scan of a freshly built runtime image,
  reported no findings. The image runs as UID/GID 65532 with the expected
  source and licence labels. A local, git-ignored copy of the optional worker
  SDK under `prototype/trace/worker/.sdk/` contains upstream example Dockerfiles
  and manifests with findings; it is not tracked and was excluded, matching CI.
- All 595 local links in 178 Markdown files, including anchors, resolve.
- Live release-environment, tag-ruleset and immutability checks
  (`hack/release/check_repository_settings.sh`) passed. Dependabot, secret
  scanning and code scanning had no open alerts.
- The published Scorecard readback on 10 November 2026 was 8.8 overall.
  Vulnerabilities scored 9, listing only `GO-2026-5932`. Branch-Protection
  scored 3, down from 4, because the `main` ruleset stopped requiring branches
  to be up to date before merging on 9 November 2026. The seven required status
  checks, pull-request-only merges and force-push and deletion protection are
  unchanged.
- `main` at `1e00096` passed CI, CodeQL, Node-context verification, Trace
  worker checks and Scorecard after those PRs merged.

## Remaining release gates

1. Review and merge this preparation update through the normal protected PR
   path. Complete required CI on the exact final `main` commit; that commit is
   the candidate source.
2. Record the v1.0.4 adopter decision on
   [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50).
   The v1.0.3-only exception does not carry over automatically.
3. With the maintainer's approval naming the exact tag, create the annotated
   `v1.0.4-rc.1` tag on the merged commit and dispatch the candidate workflow
   from it. The stable tag must not exist yet.
4. Check the generated candidate prerelease notes, the first produced by
   `create_candidate_draft.sh`. Obtain independent protected-environment
   approvals and verify the candidate archives, image, chart, reproducibility,
   signatures, SBOMs, provenance, clean-consumer lifecycle and full draft
   inventory before publication. Confirm the shipped README files name no
   candidate.
5. Obtain exact stable-tag and publication approval and promote the same
   candidate bytes through the existing workflow. Verify the stable draft and
   immutable publication before describing v1.0.4 as released.
6. After publication, render `deploy/krew/memlens.yaml.tmpl` with the signed
   archive checksums if the Krew submission is moved to v1.0.4.

The [release process](release-process.md) remains authoritative. Append
post-freeze evidence and decisions to release evidence issue #50 without
rewriting historical records.
