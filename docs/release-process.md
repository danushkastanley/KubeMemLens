# Release Process

KubeMemLens releases are tag-driven but created as GitHub drafts for maintainer review.

## Pre-release gate

1. Start from a clean, reviewed commit on `main`.
2. Read `README.md` and the canonical support contract, then confirm the target version, maturity label, support rows and evidence owners across `CHANGELOG.md`, chart metadata, installation, security and release notes.
3. Run `make check`; inspect the reported statement coverage rather than treating a percentage as a substitute for behaviour-focused tests.
4. Run Helm lint, rendering, strict schema validation, and the unsafe-replica rejection check.
5. Confirm every provider/runtime claim links to reviewed, version-bound evidence. Do not provision cloud resources merely because its advisory freshness date has passed. If the candidate changes a provider-sensitive boundary, narrow the affected claim or obtain separate approval for a new qualification run. A live-scale claim still requires the reviewed [scale qualification](scale-qualification.md) summary and evaluation for the exact declared profile.
6. Review `govulncheck`, the pinned Trivy configuration/secret/runtime-image scans, licences, RBAC, NetworkPolicy, host mounts, image user, and sensitive metric labels.
7. Check the [current upstream Kubernetes support window](https://kubernetes.io/releases/), move the kind matrix to all three supported minors, and keep each checksum-pinned kubectl on the same minor as its node image.
8. Confirm the rollback path and known limitations.

A support-contract change is a release decision. A new or widened claim must name the exact profile, link reviewed evidence, update the changelog and pass the release documentation gate before a tag is created. Historical provider evidence is never scheduled, run in CI or made a per-release requirement; stale rows are advisory and malformed or tampered rows still fail validation.

The repository has three release-side protections: the `release` environment requires maintainer review and accepts only `v*` tags, the active release-tag ruleset prevents ordinary tag creation, movement or deletion, and repository release immutability protects published assets and their tags. Verify the live configuration before tagging:

```sh
hack/release/check_repository_settings.sh
```

## Build

Push an annotated semantic-version tag such as `v0.5.0-alpha.1`. The release workflow has two jobs:

- The read-only build job validates the annotated semantic tag, creates the six CLI archives and their SBOMs, and builds the multi-architecture image once as an OCI archive with embedded SPDX SBOM and SLSA provenance attestations.
- The same job scans the exact amd64 image manifest, strictly lints and schema-validates the chart, packages the chart, creates its SPDX SBOM, and transfers the complete bundle through a one-day GitHub Actions artefact.
- The reviewed publish job revalidates the bundle, refuses every occupied release/image/chart destination, then imports the OCI archive into GHCR with digest preservation. It does not rebuild the image.
- Image and chart packages are signed and receive GitHub provenance. Their exact digests, tag and commit are written to `release-subjects.txt` before that file enters the signed checksum and provenance set.
- A clean consumer verifies checksums, Cosign identities, GitHub attestations, archive and runtime identity, OCI digests and the packaged-chart install/test/upgrade/rollback/uninstall lifecycle in disposable kind.
- Only after consumer verification succeeds does the workflow create one complete GitHub draft without replacement flags or later asset uploads.

GoReleaser cannot publish directly because `.goreleaser.yml` disables its release publisher and the build job also passes `--skip=publish`. The build job has only `contents: read`. The reviewed registry job has `packages`, `id-token` and `attestations` write permissions but only read access to repository contents. Clean-consumer verification runs in a fresh read-only job, so the candidate CLI never inherits a repository-write token. A final reviewed job receives only the content write needed to create the verified draft and does not execute candidate code. No checkout persists a write credential. No workflow run should be described as successful until the Actions logs and resulting artefacts have been inspected.

Trivy runs from the official `0.72.0` image pinned by immutable digest. KubeMemLens does not use mutable Trivy action tags: Aqua Security's [March 2026 advisory](https://github.com/aquasecurity/trivy/security/advisories/GHSA-69fq-xp46-6x23) documented compromised action tags and explicitly identifies digest-pinned images as unaffected. Version and digest upgrades require reviewing the official immutable release and rerunning all three scans locally or in CI.

The Syft installer action is commit-pinned and requests Syft `v1.44.0` explicitly. Upgrade it only after checking the official immutable release and verifying that every archive produces a non-empty SPDX 2.3 SBOM without local build paths.

The Dockerfile frontend and multi-architecture Go builder image are also pinned by manifest-list digest. Toolchain upgrades must update both the human-readable tag and digest, build both release architectures, and repeat the runtime-image scan.

## Draft audit

Before promotion:

- download and verify every checksum;
- verify the Cosign bundle and certificate identity against this repository workflow;
- verify GitHub's immutable-release attestation, file/image/chart provenance and every SPDX SBOM;
- run each CLI archive's `version` command on a representative platform;
- pull the image by digest and confirm it runs as UID/GID 65532;
- install the exact OCI chart into a disposable cluster and repeat smoke, upgrade, rollback, and uninstall;
- verify that any live-scale statement matches the reviewed profile, candidate artefacts and sanitised scale evaluation;
- confirm provider claims remain limited to their reviewed combinations and inspect, but do not fail solely on, advisory freshness warnings;
- confirm the image, chart, archive, changelog, and tag use one version;
- confirm the release contains no credentials, local paths, production identifiers, or build debris.

## Promotion and rollback

Publish the GitHub draft only after the audit passes. Repository release immutability locks the assets and associated tag at publication; the draft remains editable so the audit can finish first. Submit Krew and Artifact Hub metadata after the immutable release artefacts exist.

For Artifact Hub, register one Helm OCI repository at `oci://ghcr.io/danushkastanley/charts/kube-memlens`. Artifact Hub then supplies the repository ID. Render `deploy/artifacthub/artifacthub-repo.yml.tmpl` with that ID and the maintainer's public Artifact Hub account email, review the public contact, and push it as the registry's special `artifacthub.io` metadata tag using the media types in the official Artifact Hub OCI instructions. The chart package includes a chart-local README and version-aligned `artifacthub.io/images` metadata so the indexed package can resolve its installation documentation and release image.

Do not replace artefacts for an existing version. The workflow has no `--clobber` or post-creation upload path and stops when any release, image tag or chart version already exists. A draft can be created only after signed packages pass clean-consumer verification. GitHub draft creation starts with the title `INCOMPLETE publication`; the workflow changes it to the final version only after every asset upload succeeds. Cancellation or a failed API call therefore cannot leave a normal-looking partial draft. Any package published before an earlier failure reserves that version; inspect the external state and fix forward with a new version. If a release is defective, document the issue, mark it appropriately, and provide an explicit Helm rollback revision or prior exact version. Revoke or remove a compromised artefact only as part of a documented security response.
