#!/usr/bin/env bash
set -Eeuo pipefail

workflow=.github/workflows/release.yml
candidate_workflow=.github/workflows/candidate.yml
promotion_workflow=.github/workflows/promote.yml
candidate_publish_workflow=.github/workflows/publish-candidate.yml
ci=.github/workflows/ci.yml
chart=charts/kube-memlens
consumer=hack/release/verify_consumer.sh
draft=hack/release/create_draft.sh
candidate_manifest=hack/release/validate_candidate_manifest.sh
candidate_publisher=hack/release/publish_candidate.sh
promotion=hack/release/promote_candidate.sh
candidate_draft_publisher=hack/release/publish_candidate_draft.sh
release_process=docs/release-process.md

require_text() {
  local file=$1 text=$2
  grep -Fq -- "${text}" "${file}" || {
    echo "release contract is missing from ${file}: ${text}" >&2
    exit 1
  }
}

require_text "${workflow}" 'permissions: {}'
require_text "${workflow}" "- 'v*-alpha.*'"
require_text "${workflow}" "- 'v*-beta.*'"
require_text "${workflow}" 'contents: read'
require_text "${workflow}" 'name: release'
require_text "${workflow}" 'persist-credentials: false'
# shellcheck disable=SC2016 # Workflow variables are matched literally.
require_text "${workflow}" 'validate_tag.sh "$GITHUB_REF_NAME" "$GITHUB_SHA"'
require_text "${workflow}" 'type=oci,dest='
require_text "${workflow}" 'validate_oci_archive.py'
require_text "${workflow}" 'copy --all --preserve-digests'
require_text "${workflow}" 'release-subjects.txt'
require_text "${workflow}" 'checksums.txt.sigstore.json'
# shellcheck disable=SC2016 # Workflow expression is matched literally.
require_text "${workflow}" 'name: signed-release-bundle-${{ github.sha }}'
require_text "${workflow}" 'name: Verify as a clean read-only consumer'
require_text "${workflow}" 'name: Create complete draft release'
require_text "${workflow}" 'attestations: read'
require_text "${draft}" 'INCOMPLETE publication:'
require_text "${draft}" 'gh release create'
require_text "${draft}" 'gh release edit'
require_text "${workflow}" 'helm lint --strict'
require_text "${workflow}" 'package_chart.py'
require_text "${consumer}" 'helm test'
require_text "${consumer}" 'RELEASE_BUNDLE_CERTIFICATE_IDENTITY'
require_text "${ci}" 'lint --strict charts/kube-memlens'
require_text "${ci}" 'github.com/rhysd/actionlint/cmd/actionlint@v1.7.7'
require_text "${candidate_workflow}" 'workflow_dispatch:'
require_text "${candidate_workflow}" 'name: Build reproducible prospective GA artefacts'
# shellcheck disable=SC2016 # Workflow variables are matched literally.
require_text "${candidate_workflow}" 'GORELEASER_CURRENT_TAG=${ga_tag}'
# shellcheck disable=SC2016 # Workflow expressions are matched literally.
require_text "${candidate_workflow}" 'test "${{ steps.image-first.outputs.digest }}" = "${{ steps.image-second.outputs.digest }}"'
require_text "${candidate_workflow}" 'package_chart.py'
require_text "${candidate_workflow}" 'publish_candidate.sh'
require_text "${candidate_workflow}" 'verify_candidate.sh'
require_text "${candidate_workflow}" 'name: release'
require_text "${candidate_workflow}" 'candidates/${CANDIDATE_VERSION}/charts/kube-memlens'
require_text "${candidate_workflow}" 'candidates/${CANDIDATE_VERSION}/kube-memlens'
require_text "${promotion_workflow}" 'workflow_dispatch:'
require_text "${promotion_workflow}" 'name: Copy exact candidate subjects'
require_text "${promotion_workflow}" 'promote_candidate.sh'
require_text "${promotion_workflow}" 'verify_promotion.sh'
require_text "${promotion_workflow}" 'name: release'
require_text "${candidate_publish_workflow}" 'workflow_dispatch:'
require_text "${candidate_publish_workflow}" 'name: Validate and publish exact candidate draft'
require_text "${candidate_publish_workflow}" 'name: release'
require_text "${candidate_publish_workflow}" 'persist-credentials: false'
require_text "${candidate_publish_workflow}" 'publish_candidate_draft.sh'
require_text "${candidate_draft_publisher}" 'gh api --method PATCH'
require_text "${candidate_draft_publisher}" "'{draft: false, prerelease: true}'"
require_text "${candidate_manifest}" 'candidate and intended GA tags do not share a base version'
require_text "${candidate_publisher}" 'candidate-manifest.sigstore.json'
require_text "${promotion}" 'oras cp --recursive'
require_text "${promotion}" 'candidate and GA tags do not target the same commit'
require_text "${release_process}" 'release evidence issue #50'
test -f hack/release/candidate-manifest.schema.json
test -x hack/release/install_oras.sh
test -x hack/release/package_chart.py
test -f "${chart}/values.schema.json"
test -f "${chart}/templates/tests/test-connection.yaml"
test "$(grep -c '^      contents: read$' "${workflow}")" -eq 3
test "$(grep -c '^      contents: write$' "${workflow}")" -eq 1
test "$(grep -c '^      packages: write$' "${workflow}")" -eq 1
test "$(grep -c '^      id-token: write$' "${workflow}")" -eq 1
test "$(grep -c '^          persist-credentials: false$' "${workflow}")" -eq 4
attestation_ref='actions/attest-build-provenance@4d101475d8b20a2381f78447822ac1eab6504dd8 # v4.2.2'
attestation_count=$(awk -v ref="${attestation_ref}" \
  'index($0, ref) { count++ } END { print count + 0 }' \
  "${workflow}" "${candidate_workflow}" "${promotion_workflow}")
test "${attestation_count}" -eq 9

if grep -Fq -- "- 'v*'" "${workflow}"; then
  echo 'legacy release workflow accepts RC or stable tags' >&2
  exit 1
fi

if grep -Eq -- '--clobber|gh release upload' \
  "${workflow}" "${candidate_workflow}" "${candidate_publish_workflow}" "${promotion_workflow}" \
  "${draft}" hack/release/create_candidate_draft.sh hack/release/create_promoted_draft.sh; then
  echo 'release workflow permits replacement or post-creation asset mutation' >&2
  exit 1
fi

if grep -Eq 'goreleaser|docker/build-push-action|docker build( |$)|docker buildx build|helm package|gh release upload' \
  "${candidate_publish_workflow}"; then
  echo 'candidate draft publication rebuilds or mutates candidate assets' >&2
  exit 1
fi

if grep -Eq 'goreleaser|docker/build-push-action|docker build( |$)|docker buildx build|helm package' \
  "${promotion_workflow}"; then
  echo 'promotion workflow rebuilds or repackages candidate artefacts' >&2
  exit 1
fi

publish_job=$(sed -n '/^  publish:/,$p' "${workflow}")
if grep -Eq 'docker build( |$)|docker buildx build|docker/build-push-action' <<< "${publish_job}"; then
  echo 'publish job rebuilds the qualified image' >&2
  exit 1
fi

subjects_line=$(grep -n 'release-subjects.txt' "${workflow}" | head -1 | cut -d: -f1)
release_line=$(grep -n 'hack/release/create_draft.sh' "${workflow}" | head -1 | cut -d: -f1)
if [ -z "${subjects_line}" ] || [ -z "${release_line}" ] || [ "${subjects_line}" -ge "${release_line}" ]; then
  echo 'release subject inventory must exist before draft creation' >&2
  exit 1
fi

echo 'release integrity contract passed'
