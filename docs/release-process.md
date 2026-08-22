# Release Process

KubeMemLens releases are tag-driven but created as GitHub drafts for maintainer review.

## Pre-release gate

1. Start from a clean, reviewed commit on `main`.
2. Read `README.md`, then confirm the target version and maturity label across `CHANGELOG.md`, chart metadata, installation, compatibility, security, roadmap, and release notes.
3. Run `make check`; inspect the reported statement coverage rather than treating a percentage as a substitute for behaviour-focused tests.
4. Run Helm lint, rendering, strict schema validation, and the unsafe-replica rejection check.
5. Complete the declared live-cluster compatibility matrix, including upgrade and uninstall.
6. Review `govulncheck`, the pinned Trivy configuration/secret/runtime-image scans, licences, RBAC, NetworkPolicy, host mounts, image user, and sensitive metric labels.
7. Check the [current upstream Kubernetes support window](https://kubernetes.io/releases/), move the kind matrix to all three supported minors, and keep each checksum-pinned kubectl on the same minor as its node image.
8. Confirm the rollback path and known limitations.

Configure a GitHub environment named `release` before the first tag. Restrict it to protected release tags and require maintainer approval. The workflow references this environment so its publication job cannot start until the configured protection rules pass.

## Build

Push an annotated semantic-version tag such as `v0.5.0-alpha.1`. The release workflow has two jobs:

- The read-only build job creates and validates the six CLI archives, SBOMs, Krew manifest and Helm package. It scans a representative image and transfers the bundle through a one-day GitHub Actions artefact.
- The protected publish job downloads and revalidates that bundle. It signs the complete checksum file, creates provenance, and refuses to continue when a release already exists for the tag.
- The publish job creates one draft release without replacement flags, then builds and signs the multi-architecture image and pushes and signs the version-aligned OCI Helm chart.
- The final release asset records the exact image digest and chart reference for the draft audit.

GoReleaser cannot publish directly because `.goreleaser.yml` disables its release publisher and the build job also passes `--skip=publish`. Only the environment-gated publish job receives `contents`, `packages`, `id-token` and `attestations` write permissions. No workflow run should be described as successful until the Actions logs and resulting artefacts have been inspected.

Trivy runs from the official `0.72.0` image pinned by immutable digest. KubeMemLens does not use mutable Trivy action tags: Aqua Security's [March 2026 advisory](https://github.com/aquasecurity/trivy/security/advisories/GHSA-69fq-xp46-6x23) documented compromised action tags and explicitly identifies digest-pinned images as unaffected. Version and digest upgrades require reviewing the official immutable release and rerunning all three scans locally or in CI.

The Syft installer action is commit-pinned and requests Syft `v1.44.0` explicitly. Upgrade it only after checking the official immutable release and verifying that every archive produces a non-empty SPDX 2.3 SBOM without local build paths.

The Dockerfile frontend and multi-architecture Go builder image are also pinned by manifest-list digest. Toolchain upgrades must update both the human-readable tag and digest, build both release architectures, and repeat the runtime-image scan.

## Draft audit

Before promotion:

- download and verify every checksum;
- verify the Cosign bundle and certificate identity against this repository workflow;
- inspect SBOMs and provenance;
- run each CLI archive's `version` command on a representative platform;
- pull the image by digest and confirm it runs as UID/GID 65532;
- install the exact OCI chart into a disposable cluster and repeat smoke, upgrade, rollback, and uninstall;
- confirm the image, chart, archive, changelog, and tag use one version;
- confirm the release contains no credentials, local paths, production identifiers, or build debris.

## Promotion and rollback

Publish the GitHub draft only after the audit passes. GitHub release immutability applies after publication, not while the release remains a draft. Submit Krew and Artifact Hub metadata after the immutable release artefacts exist.

For Artifact Hub, register one Helm OCI repository at `oci://ghcr.io/danushkastanley/charts/kube-memlens`. Artifact Hub then supplies the repository ID. Render `deploy/artifacthub/artifacthub-repo.yml.tmpl` with that ID and the maintainer's public Artifact Hub account email, review the public contact, and push it as the registry's special `artifacthub.io` metadata tag using the media types in the official Artifact Hub OCI instructions. The chart package includes a chart-local README and version-aligned `artifacthub.io/images` metadata so the indexed package can resolve its installation documentation and release image.

Do not replace artefacts for an existing version. The workflow has no `--clobber` path and stops when the tag already has a release. If a run leaves a partial draft or publishes any package before failing, inspect the external state and fix forward with a new version. If a release is defective, document the issue, mark it appropriately, and provide an explicit Helm rollback revision or prior exact version. Revoke or remove a compromised artefact only as part of a documented security response.
