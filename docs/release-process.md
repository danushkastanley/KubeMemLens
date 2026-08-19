# Release Process

KubeMemLens releases are tag-driven but created as GitHub drafts for maintainer review.

## Pre-release gate

1. Start from a clean, reviewed commit on `main`.
2. Confirm the target version and update `CHANGELOG.md`, chart metadata, support matrix, and release notes.
3. Run `make check`; inspect the reported statement coverage rather than treating a percentage as a substitute for behaviour-focused tests.
4. Run Helm lint, rendering, strict schema validation, and the unsafe-replica rejection check.
5. Complete the declared live-cluster compatibility matrix, including upgrade and uninstall.
6. Review `govulncheck`, the pinned Trivy configuration/secret/runtime-image scans, licences, RBAC, NetworkPolicy, host mounts, image user, and sensitive metric labels.
7. Check the [current upstream Kubernetes support window](https://kubernetes.io/releases/), move the kind matrix to all three supported minors, and keep each checksum-pinned kubectl on the same minor as its node image.
7. Confirm the rollback path and known limitations.

## Build

Push an annotated semantic-version tag such as `v0.5.0-rc.1`. The release workflow:

- builds CLI archives for Linux, macOS, and Windows on amd64 and arm64;
- injects version, commit, and build date;
- creates checksums and archive SBOMs;
- builds and vulnerability-scans a representative release image before publishing multi-architecture image manifests;
- signs the checksum file with keyless Cosign and emits its certificate;
- generates GitHub build provenance;
- builds a linux/amd64 and linux/arm64 image with SBOM and provenance;
- pushes and signs the digest in GHCR;
- packages and pushes the version-aligned OCI Helm chart;
- creates a draft GitHub release for manual review.

No workflow run should be described as successful until the actual Actions logs and resulting artefacts have been inspected.

Trivy runs from the official `0.72.0` image pinned by immutable digest. KubeMemLens does not use mutable Trivy action tags: Aqua Security's [March 2026 advisory](https://github.com/aquasecurity/trivy/security/advisories/GHSA-69fq-xp46-6x23) documented compromised action tags and explicitly identifies digest-pinned images as unaffected. Version and digest upgrades require reviewing the official immutable release and rerunning all three scans locally or in CI.

The Syft installer action is commit-pinned and requests Syft `v1.44.0` explicitly. Upgrade it only after checking the official immutable release and verifying that every archive produces a non-empty SPDX 2.3 SBOM without local build paths.

The Dockerfile frontend and multi-architecture Go builder image are also pinned by manifest-list digest. Toolchain upgrades must update both the human-readable tag and digest, build both release architectures, and repeat the runtime-image scan.

## Draft audit

Before promotion:

- download and verify every checksum;
- verify the Cosign signature and certificate identity against this repository workflow;
- inspect SBOMs and provenance;
- run each CLI archive's `version` command on a representative platform;
- pull the image by digest and confirm it runs as UID/GID 65532;
- install the exact OCI chart into a disposable cluster and repeat smoke, upgrade, rollback, and uninstall;
- confirm the image, chart, archive, changelog, and tag use one version;
- confirm the release contains no credentials, local paths, production identifiers, or build debris.

## Promotion and rollback

Publish the GitHub draft only after the audit passes. Submit Krew and Artifact Hub metadata after the immutable release artefacts exist.

For Artifact Hub, register one Helm OCI repository at `oci://ghcr.io/danushkastanley/charts/kube-memlens`. Artifact Hub then supplies the repository ID. Render `deploy/artifacthub/artifacthub-repo.yml.tmpl` with that ID and the maintainer's public Artifact Hub account email, review the public contact, and push it as the registry's special `artifacthub.io` metadata tag using the media types in the official Artifact Hub OCI instructions. The chart package includes a chart-local README and version-aligned `artifacthub.io/images` metadata so the indexed package can resolve its installation documentation and release image.

Do not replace artefacts for an existing version. If a release is defective, document the issue, mark it appropriately, fix forward with a new version, and provide an explicit Helm rollback revision or prior exact version. Revoke or remove a compromised artefact only as part of a documented security response.
