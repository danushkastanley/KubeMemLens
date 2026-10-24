# v1.0.2 release preparation

Prepared: 24 October 2026. This is a pre-tag record; signed manifests and
workflow results become the artefact authority after the candidate is frozen.

## Identity and scope

| Item | Status |
| --- | --- |
| Target stable version | `v1.0.2` |
| Proposed candidate | `v1.0.2-rc.1`; exact-tag approval remains required |
| Implementation baseline | `3e50db9` on `main`, plus the reviewed Pod dashboard and this preparation change |
| Candidate source | Must be the final reviewed preparation commit merged to `main` |
| Publication | Not started; no `v1.0.2` or `v1.0.2-rc.1` tag exists at preparation readback |
| Rollback reference | Published `v1.0.1`; restores the older Go toolchain |
| Support | Existing standard EKS managed Linux scope; no new provider, runtime, terminal or scale qualification |

## Included changes

The [Pod dashboard](pod-dashboard.md) replaces the default line-oriented Pod
detail with composition, limit/headroom, Kubernetes context, retained history,
read-only next checks, container contributions and recent pressure evidence.
Enter opens the selected Pod; Esc restores the parent selection, filter, sort
and viewport. The main dashboard layout is unchanged. Compact layouts retain
all panels through Tab navigation; focus has a text marker for monochrome use.
Raw evidence remains available with `e` inside the dashboard.

The patch also includes the already merged maintenance changes since v1.0.1:

- [#219](https://github.com/danushkastanley/KubeMemLens/pull/219): Go 1.27.3
  across the three modules, pinned builders and trace executable validation;
  x/sys 0.49.0 aligned across the nested module graphs.
- [#220](https://github.com/danushkastanley/KubeMemLens/pull/220),
  [#221](https://github.com/danushkastanley/KubeMemLens/pull/221),
  [#222](https://github.com/danushkastanley/KubeMemLens/pull/222) and
  [#223](https://github.com/danushkastanley/KubeMemLens/pull/223): immutable
  Scorecard 2.5.0, CodeQL 4.39.0, Download Artifact 8.0.2 and GoReleaser action
  7.3.0 pins.

Go 1.27.3 addresses [GO-2026-6809 / CVE-2026-111943](https://pkg.go.dev/vuln/GO-2026-6809)
in certificate verification. The official advisory was checked on 24 October
2026. These source changes do not patch installed v1.0.1 binaries or previously
signed optional engine bundles.

## Compatibility and rollback

This patch introduces no API, capture-format, permission, host-mount or stored
data migration. Suggested commands remain read-only previews and do not run
automatically. Unknown limits, incomplete counter windows, stale snapshots and
connection failures remain explicit. History is bound to the Pod instance and
does not interpolate missing observations. The optional eBPF extension remains
a separate development profile and retains its matched-engine requirements.

The [upstream Kubernetes support window](https://kubernetes.io/releases/) was
checked on 24 October 2026: 1.35, 1.36 and 1.37. The existing CI matrix covers
those minors. Historical EKS, scale and terminal evidence remains bound to its
original artefacts; it is not qualification of prospective v1.0.2 bytes.

Use the [installation guide](installation.md) only after matching signed
artefacts are published. Helm upgrade and rollback restart the collector and
reset its in-memory history. Follow the recorded Helm revision and fresh-data
checks in the [reliability runbook](runbooks/reliability.md#roll-back). A rollback
to v1.0.1 restores the older Go certificate-verification behaviour.

## Verification

The following preparation checks completed on 24 October 2026:

- Root unit, race, coverage, vet and build checks passed; total statement
  coverage was 73.7%. Formatting, support, terminal and release contracts passed.
- Fresh root, trace and Linux/amd64 worker `govulncheck` scans reported zero
  reachable vulnerabilities and zero affected imported packages. Each retains
  the known module-only `GO-2026-5932` OpenPGP advisory, with no fixed version.
  The scanned targets do not import the affected package. No finding is
  suppressed; retain the existing worker dependency boundary and review any
  future OpenPGP import.
- The local `make check` passed trace preflight, then stopped at the optional
  worker's Linux-only SDK imports on macOS. This is not a passing full release
  check; the PR's Linux trace/worker and required CI jobs remain necessary.
- A newly built CLI passed 18 live terminal checks against the existing local
  kind demo cluster at 180×50 and 80×24. Checks covered Enter/Esc, retained
  filter and selection, read-only recommendations, raw evidence, nested
  container detail, resize and clean exit. The namespace held 13 synthetic
  Pods. This does not extend managed-provider or terminal qualification claims.
- Helm strict lint/render and test-hook security checks passed. Schema checks
  rejected unsupported replicas and unknown values; the pinned kubeconform
  validator accepted all 27 rendered resources with no errors or skips.
- Local file links passed in all 176 tracked/prepared Markdown files. Candidate
  documentation assertions passed for 1.0.2 / 1.0.2-rc.1. Historical version
  references retain their original scope.
- Live release-environment, tag-ruleset, immutability and community-control
  checks passed. Dependabot and secret-scanning readbacks had no open alerts.
  Three open Scorecard findings remain: Code-Review (0), Branch-Protection (4)
  and Vulnerabilities (9, the retained OpenPGP advisory). These are not three
  new CodeQL code findings. No alert was dismissed.
- The published Scorecard readback was 8.4 overall, with Vulnerabilities 9,
  Branch-Protection 4, Signed-Releases 8, and Token-Permissions,
  Dangerous-Workflow and Pinned-Dependencies 10. It passes the existing
  checks-only policy thresholds. It is baseline evidence, not a result for the
  final merged v1.0.2 source; repeat the readback before the candidate decision.

The PR checks, pinned Trivy configuration/secret/image scans and final merged
source review remain release gates. No v1.0.2 image, chart or archive has been
published or verified as an immutable release artefact.

The first PR CI run passed all checks except the Kubernetes 1.37 terminal smoke
test, which still expected the old `Diagnosis:` view immediately after opening
a Pod. The smoke test now exercises Enter, context/history panels, raw evidence
and both Esc return paths, preserving its history, scrolling and read-only
recommendation assertions. The repaired smoke passed locally with 20 temporary
workload Pods across 80×24, 120×30 and 180×50 terminals. A fresh CI run on the
repair remains required before merge.

## Remaining release gates

1. Review and merge the TUI and release preparation changes through the normal
   protected PR path. Complete required CI and the release/security readback on
   the exact final `main` commit.
2. Obtain approval naming `v1.0.2-rc.1`, create only that annotated candidate tag
   and dispatch the existing candidate workflow from it. The stable tag must
   not exist yet.
3. Obtain independent protected-environment approvals and verify the candidate
   archives, image, chart, reproducibility, signatures, SBOMs, provenance,
   clean-consumer lifecycle and full draft inventory before publication.
4. Record the v1.0.2 promotion decision. Earlier release-specific adopter
   exceptions do not carry forward; retain the documented gate unless the
   maintainer records a new explicit decision.
5. Obtain exact stable-tag/publication approval and promote the same candidate
   bytes through the existing workflow. Verify the stable draft and immutable
   publication before describing v1.0.2 as released.

The [release process](release-process.md) remains authoritative. Append
post-freeze evidence and decisions to release evidence issue #50 without
rewriting historical records. Preparation does not publish a release.
