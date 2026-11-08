# v1.0.3 release preparation

Prepared: 8 November 2026. Signed manifests and workflow results become the
artefact authority once a candidate tag freezes the source; this record does
not publish a release.

## Identity and scope

| Item | Status |
| --- | --- |
| Target stable version | `v1.0.3` |
| Proposed candidate | `v1.0.3-rc.1`; exact-tag approval remains required |
| Implementation baseline | `d94ebc5` on `main` (merged PRs #231, #232 and #233) |
| Candidate source | Must be the final reviewed preparation commit merged to `main` |
| Publication | No candidate or stable release published |
| Rollback reference | Published `v1.0.2` |
| Support | Existing standard EKS managed Linux scope; no new provider, runtime, terminal or scale qualification |

## Included changes

- [#232](https://github.com/danushkastanley/KubeMemLens/pull/232): first-run
  and diagnosis changes.
  - The sample cgroup data is built into the CLI, so `kubectl memlens sample`
    works from a release binary. Sample names are validated before lookup.
  - When the aggregated API is absent, the CLI says that KubeMemLens is not
    installed and links to the installation guide. Restricted mode notes a
    missing chart or metrics-server on stderr for table and text output.
  - The chart prints next steps after installation.
  - `rss-heavy` now also requires material anonymous memory: at least half of
    a finite limit, or 512 MiB without one. On a local kind cluster, v1.0.2
    labelled 12 of 13 Pods `rss-heavy`, including idle system Pods; the new
    build labels only the deliberate workload holding 203 MiB of a 256 MiB
    limit. The diagnosis name, metric label
    and JSON value are unchanged.
  - The CLI and TUI label the bucket "Anonymous (anon)" and `ANON` instead of
    "RSS / anon" and `RSS`. Structured output field names are unchanged.
  - Usage text shows `kubectl memlens`; the pre-v1 connection flags are hidden
    from help but still accepted.
  - The Krew manifest template extracts `LICENSE` and states the chart and
    metrics-server requirements.
- [#233](https://github.com/danushkastanley/KubeMemLens/pull/233):
  `golang.org/x/net` 0.62.0 across the standard, trace and worker modules for
  CVE-2026-78661, with the matching `x/crypto`, `x/sync`, `x/term` and
  `x/text` releases.
- [#231](https://github.com/danushkastanley/KubeMemLens/pull/231): launch
  documentation. The README, security policy, code of conduct and support
  contract were rewritten. GKE and self-managed `v0.0.1-alpha.3` results are
  now marked historical, and the support scope is unchanged.

The recommendation evaluation corpus and reviewed copy hashes were updated in
#232 for the new threshold and wording; the evaluation gate passes with them.

## Compatibility and rollback

This patch introduces no API, capture-format, permission, host-mount or stored
data migration. JSON, YAML and CSV field names and the `rss-heavy` value are
unchanged. Scripts that parse the human-readable table header for `RSS` must
read `ANON` instead. Hidden flags keep their behaviour.

Fewer Pods are labelled `rss-heavy`. Dashboards or alerts keyed on the
diagnosis label will see a lower count after upgrading; that is the intended
correction, not lost data.

The [upstream Kubernetes support window](https://kubernetes.io/releases/) was
checked on 8 November 2026: 1.35, 1.36 and 1.37. Kubernetes 1.34 reached end of
life on 27 October 2026. The existing CI kind matrix covers the three supported
minors.

Helm upgrade and rollback restart the collector and reset its in-memory
history. Follow the recorded Helm revision and fresh-data checks in the
[reliability runbook](runbooks/reliability.md#roll-back). A rollback to v1.0.2
restores the earlier labels, threshold and `x/net` version.

## Verification

The following preparation checks completed on 8 November 2026 against this
preparation branch:

- `make check` ran every target. Unit, race, coverage, vet, build, formatting,
  incident-session, trace release and chart, recommendation evaluation,
  support, scale, terminal, release and community targets passed. Total
  statement coverage was 73.9%.
- Two targets did not pass locally. `check-trace-worker` stopped at the
  optional worker's Linux-only SDK imports on macOS, as for v1.0.2; the Linux
  trace and worker CI jobs remain necessary. `check-provider-contract` binds the
  chart to the committed source, so it failed while the chart edits were
  uncommitted; it passed when repeated after the preparation commit, together
  with the release, support, community and terminal contracts.
- Fresh `govulncheck` v1.7.0 scans of the standard, trace and Linux/amd64
  worker modules reported no reachable vulnerabilities and no affected
  imported packages. Each retains the module-only `GO-2026-5932` OpenPGP
  advisory in `x/crypto`; its fix exists only as an untagged pseudo-version and
  is not adopted. No finding is suppressed.
- Helm strict lint passed. The default render uses the `1.0.3` image, passes the
  test-hook security check, and the pinned kubeconform validator accepted all
  27 rendered resources with no errors or skips. The schema rejected two
  collector replicas.
- The pinned Trivy 0.72.0 configuration and secret scans of the tracked files,
  and the high and critical vulnerability scan of a freshly built runtime image,
  reported no findings. The image runs as UID/GID 65532.
- All 581 local links in 177 Markdown files, including anchors, resolve.
- Live release-environment, tag-ruleset, immutability and community-control
  checks passed. Dependabot and secret-scanning readbacks had no open alerts.
- The published Scorecard readback on 8 November 2026 was 8.5 overall.
  Vulnerabilities scored 8, listing `GO-2026-5932` and
  [`GO-2026-6865`](https://osv.dev/GO-2026-6865), a panic in
  `github.com/klauspost/compress` before 1.20.2 that reaches the standard,
  trace and worker module graphs indirectly. govulncheck does not report it as
  called. Code-Review 4 and Branch-Protection 4 are unchanged in kind from
  v1.0.2.

The open dependency queue at preparation time:

- [#229](https://github.com/danushkastanley/KubeMemLens/pull/229),
  bubbletea 2.1.1, and
  [#230](https://github.com/danushkastanley/KubeMemLens/pull/230), the Go
  builder image digest, pass CI.
- [#228](https://github.com/danushkastanley/KubeMemLens/pull/228), Kubernetes
  libraries 0.37.2, fails the trace and worker checks because it updates only
  the root module.

Merge, align or explicitly defer each of these, and decide on the
`klauspost/compress` update, before the candidate tag freezes the source.

## Remaining release gates

1. Resolve the dependency queue and the `klauspost/compress` decision above.
   Review and merge this preparation through the normal protected PR path,
   updating this record if the source changes. Complete required CI on the
   exact final `main` commit.
2. Record the v1.0.3 adopter decision on
   [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50).
   The v1.0.2-only exception does not carry over automatically.
3. Obtain approval naming `v1.0.3-rc.1`, create only that annotated candidate
   tag and dispatch the existing candidate workflow from it. The stable tag
   must not exist yet.
4. Obtain independent protected-environment approvals and verify the candidate
   archives, image, chart, reproducibility, signatures, SBOMs, provenance,
   clean-consumer lifecycle and full draft inventory before publication.
5. Obtain exact stable-tag and publication approval and promote the same
   candidate bytes through the existing workflow. Verify the stable draft and
   immutable publication before describing v1.0.3 as released.
6. After publication, render `deploy/krew/memlens.yaml.tmpl` with the signed
   archive checksums and submit it to Krew.

The [release process](release-process.md) remains authoritative. Append
post-freeze evidence and decisions to release evidence issue #50 without
rewriting historical records.
