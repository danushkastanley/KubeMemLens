# v1.0.6 release preparation

Prepared: 28 November 2026. Signed manifests and workflow results become the
artefact authority once a candidate tag freezes the source; this record does
not publish a release.

## Identity and scope

| Item | Status |
| --- | --- |
| Target stable version | `v1.0.6` |
| Proposed candidate | `v1.0.6-rc.1`; exact-tag approval remains required |
| Implementation baseline | `52f48aa` on `main`: 21 pull requests merged since `v1.0.5` (#427 to #448), plus this update |
| Candidate source | Must be the final reviewed preparation commit merged to `main` |
| Publication | No candidate or stable release published |
| Rollback reference | Published `v1.0.5` |
| Support | Existing standard EKS managed Linux scope; no new provider, runtime, terminal or scale qualification |

## Why this is a patch release

The release adds features, which strict semantic versioning would number
`1.1.0`. The maintainer chose `1.0.6` on 28 November 2026 for two reasons.
Nothing in it breaks an existing command, schema field or chart value: snapshot
schema 8 is negotiated, and a `v1.0.5` agent, collector or CLI keeps working
beside a `v1.0.6` one. And the [roadmap](roadmap.md) already names `v1.1.0` for
versioned JSON and the local MCP server. `v1.0.5` also carried additions under
a patch number. The roadmap's sentence about patch releases is corrected in
this update to say what they carry.

## Included changes

The [changelog](../CHANGELOG.md) lists every user-visible change. In groups:

- **The Pod's own cgroup.** Snapshot schema 8 (#436, ADR 0032): the Pod limit
  comes from the Pod cgroup, an OOM at the Pod-level limit is seen, and
  `zswap`, `zswapped` and `oom_group_kill` are kept. Explanation schema 6
  carries the new evidence.
- **Redacted captures.** Default incident captures of schema 1 and 2 alias
  every name (#440).
- **Pod tables.** The container closest to its own limit (#435), and one
  diagnosis for the TUI row and its filters, with a notice for an unknown
  filter value (#434).
- **Operator messages.** A cgroup v1 node is named in `doctor` and `status`
  and no longer fails every scan (#433). `doctor` names a recent collector
  start (#447), which is stage 1 of ADR 0033 (#437, #445, #448).
- **Trace extension.** `prototype/trace` is renamed to `extensions/trace`
  (#439), the local image builds again (#442), and ADR 0031 records the
  dropped test-trimming step (#443). The extension is in no release artefact.
- **Distribution.** The Artifact Hub listing, the Homebrew tap and the Scoop
  bucket are documented (#446). `v1.0.6` would be the first release on which
  the tap and the bucket update themselves.
- **Dependencies and fixtures.** Thirteen Dependabot updates in one change
  (#432) and refreshed package pins in the terminal qualification image
  (#444). None changes a released artefact.
- **Checks.** Faster and narrower CI and tests (#427, #428, #429, #431, #438),
  and volume qualification samples that say why they failed (#430).

## Compatibility and rollback

There is no stored data to migrate and no chart change: the chart differs from
`v1.0.5` only in its version and image tag, and the default render is still 33
resources. The standard module's `go.mod` and `go.sum`, the runtime
Dockerfile, the release workflows and `deploy/krew/memlens.yaml.tmpl` are
unchanged since `v1.0.5`.

Behaviour that scripts and people can depend on does change:

- A Pod's limit and percentage come from the Pod cgroup when its `memory.max`
  is finite. For most Pods the limit is the same and the percentage can be
  slightly higher, because it includes the sandbox. For a Pod that uses
  pod-level resources the limit itself is corrected.
- `explain -o json` writes `schemaVersion` 6 for nearly every Pod once the
  agents are at `v1.0.6`. A script that requires 3 must accept 6.
- A redacted capture holds aliases. Replay and compare it as
  `--pod namespace-1/pod-1`. Captures written by `v1.0.5` and earlier still
  hold real names and still replay.
- The LIMIT cell of a Pod table can contain a space, and the TUI Pod table's
  DIAGNOSIS cell can read differently for a Pod with a recent OOM kill, node
  pressure or a container at its own limit.
- A cgroup v1 node reports with no containers, where it showed only as
  missing. `doctor` fails its `cgroup mode` check for it and names the cause.

Mixed versions during an upgrade or rollback, from ADR 0032:

- A `v1.0.6` agent posts at the schema a `v1.0.5` collector offers.
- A `v1.0.6` collector accepts schema 7 from a `v1.0.5` agent; those Pods keep
  the container-sum limit.
- A `v1.0.5` CLI gets responses projected to its own schema.
- After a collector rollback, a schema 8 post is refused once and the agent
  renegotiates.

The [upstream Kubernetes support window](https://kubernetes.io/releases/) on
28 November 2026 is 1.35, 1.36 and 1.37, as for v1.0.5. The CI kind matrix
covers the three supported minors.

Helm upgrade and rollback restart the collector and reset its in-memory
history. `doctor` now says so in its `collector history` line for the first 15
minutes. Follow the recorded Helm revision and fresh-data checks in the
[reliability runbook](runbooks/reliability.md#roll-back).

## Verification

The following preparation checks completed on 28 November 2026. Unless a
commit is named, they ran against this preparation branch.

- `make -k check` ran every target. Unit, race, coverage, vet, build,
  formatting, `govulncheck`, incident-session, memory-history, replica, trace
  release and chart, trace preflight and qualification, recommendation
  evaluation, support, scale, terminal, release and community targets passed.
  Total statement coverage was 76.0%, against 75.0% for v1.0.5. Two targets
  did not pass in that run, `check-trace-worker` and `check-provider-contract`;
  both are described below.
- `check-trace-worker` did not pass locally: the optional worker's SDK imports
  Linux-only packages, so its race tests cannot build on macOS, as for v1.0.2
  to v1.0.5. The Linux `Trace worker checks` workflow passed on `52f48aa`.
- Fresh `govulncheck` v1.7.0 scans of the standard, trace and Linux/amd64
  worker modules reported no reachable vulnerabilities and no affected
  imported packages. The standard module retains the module-only
  `GO-2026-5932` OpenPGP advisory in `x/crypto`, unchanged since v1.0.3. The
  trace and worker modules no longer list it. No finding is suppressed.
- All 14 steps of the CI `Helm checks` job passed when run locally from the
  workflow file, including Helm 3.18.4 strict lint. The default render uses
  the `1.0.6` image, and the pinned kubeconform validator accepted all 33
  rendered resources with no errors or skips.
- The pinned Trivy 0.72.0 configuration and secret scans of the tracked files,
  and the high and critical vulnerability scan of a freshly built runtime
  image, each exited 0. The image runs as UID/GID 65532 with the expected
  source and licence labels. The scans ran on a copy holding only tracked
  files, matching CI; a local copy of the optional worker SDK under
  `extensions/trace/worker/.sdk/` is ignored by Git and fails the
  configuration scan when included.
- Live release-environment, tag-ruleset and immutability checks
  (`hack/release/check_repository_settings.sh`) passed. Dependabot, secret
  scanning and code scanning had no open alerts, and no Dependabot pull
  request was open.
- The published Scorecard readback for `52f48aa` was 9.1 overall, as for
  v1.0.5. Vulnerabilities scored 9, listing only `GO-2026-5932`.
  Branch-Protection scored 3, unchanged.
- `main` at `52f48aa` passed CI, CodeQL, Trace worker checks and Scorecard,
  and the nightly fuzzing run of 27 November. That commit changed one ADR, so
  CI skipped its kind lanes there. `5f202ab`, the last commit that changed
  code, passed the Kubernetes 1.35.5, 1.36.1, density and CSI volume health
  lanes on `main`; its two other 1.37.0 lanes were cancelled by the next merge
  and had passed on its pull request (#447).
- No `v1.0.6` tag, candidate tag or release exists.
- `deploy/krew/memlens.yaml.tmpl` is unchanged since `v1.0.5`, so the Krew
  index pull request should change only the version, address and checksum
  lines.
- `make check-provider-contract` passed on the first commit of this
  preparation, `c52af4d`. It compares the chart with the checked-out commit,
  so it fails on a tree with an uncommitted chart change, as it did in the
  `make -k check` run above.
- All 677 local links in 192 Markdown files, including anchors, resolve.
- The full kind end-to-end run (`hack/e2e-kind.sh`, Kubernetes 1.37.0) passed
  on `c52af4d`: install, then the agentless, authenticated ingestion,
  tenant-scoped read, tenant isolation, resource metrics, MemoryQoS, Pod
  resource, TUI and reliability smokes, then upgrade, rollback and uninstall.
  The Pod resource smoke reported the Pod cgroup limit, the older live and
  capture readers, mixed agent and collector versions and a collector rollback
  as passing. Later commits in this preparation change this record only.
- An upgrade from the published `v1.0.5` passed on a disposable kind cluster
  at Kubernetes 1.37.0. The published chart and image were installed from
  GHCR, upgraded to the chart and an image built from `c52af4d`, tested with
  `helm test`, rolled back to revision 1 and uninstalled. At each of the three
  stages `doctor --strict` passed with both the published `v1.0.5` CLI, taken
  from the release archive after a checksum match, and the CLI built from
  `c52af4d`. After the upgrade the new CLI read `limitSource: pod-cgroup` and
  wrote explanation schema 6; the `v1.0.5` CLI read the same Pods with no new
  field and wrote schema 3. After the rollback the agent logged no error.

Not verified in this preparation:

- The density benchmarks, the live density smoke and the CSI volume health
  lane locally. CI ran them on `5f202ab`, and no code has changed since.
- Kubernetes 1.35 and 1.36 locally. CI ran both kind lanes on `5f202ab`.
- A `v1.0.6` agent that keeps posting to a `v1.0.5` collector for longer than
  a rollout. The upgrade check used one node, and Helm replaced the agent and
  the collector together in each direction. The Pod resource smoke covers the
  mixed pairs with schema limits on one build, not with the published bytes.
- `zswap` with a non-zero pool and a real `oom_group_kill`. A kind node reports
  both as zero; tests cover non-zero values.
- The Homebrew tap and the Scoop bucket updating themselves. Neither has yet
  seen a new stable release.
- The candidate, publication and promotion workflows for this version. They
  run only on a tag.

## Remaining release gates

1. Review and merge this preparation update through the normal protected PR
   path. Complete required CI on the exact final `main` commit; that commit is
   the candidate source.
2. Record the v1.0.6 adopter decision on
   [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50).
   The v1.0.5-only exception does not carry over automatically.
3. With the maintainer's approval naming the exact tag, create the annotated
   `v1.0.6-rc.1` tag on the merged commit and dispatch the candidate workflow
   from it. The stable tag must not exist yet.
4. Check the generated candidate prerelease notes. Obtain independent
   protected-environment approvals and verify the candidate archives, image,
   chart, reproducibility, signatures, SBOMs, provenance, clean-consumer
   lifecycle and full draft inventory before publication. Confirm the shipped
   README files name no candidate.
5. Obtain exact stable-tag and publication approval and promote the same
   candidate bytes through the existing workflow. Verify the stable draft and
   immutable publication before describing v1.0.6 as released.
6. After publication, confirm each channel:
   - the [Krew index update](release-process.md#krew-index-update) workflow
     succeeded and its pull request appeared on krew-index;
   - the [Homebrew tap](release-process.md#homebrew-tap) and the
     [Scoop bucket](release-process.md#scoop-bucket) committed the new
     version within a few hours, each after verifying the release;
   - [Artifact Hub](release-process.md#artifact-hub) lists chart `1.0.6`.

The [release process](release-process.md) remains authoritative. Append
post-freeze evidence and decisions to release evidence issue #50 without
rewriting historical records.
