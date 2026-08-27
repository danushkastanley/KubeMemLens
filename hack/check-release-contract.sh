#!/usr/bin/env bash
set -Eeuo pipefail

workflow=.github/workflows/release.yml
ci=.github/workflows/ci.yml
chart=charts/kube-memlens
consumer=hack/release/verify_consumer.sh
draft=hack/release/create_draft.sh

require_text() {
  local file=$1 text=$2
  grep -Fq -- "${text}" "${file}" || {
    echo "release contract is missing from ${file}: ${text}" >&2
    exit 1
  }
}

require_text "${workflow}" 'permissions: {}'
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
require_text "${consumer}" 'helm test'
require_text "${ci}" 'lint --strict charts/kube-memlens'
test -f "${chart}/values.schema.json"
test -f "${chart}/templates/tests/test-connection.yaml"
test "$(grep -c '^      contents: read$' "${workflow}")" -eq 3
test "$(grep -c '^      contents: write$' "${workflow}")" -eq 1
test "$(grep -c '^      packages: write$' "${workflow}")" -eq 1
test "$(grep -c '^      id-token: write$' "${workflow}")" -eq 1
test "$(grep -c '^          persist-credentials: false$' "${workflow}")" -eq 4

if grep -Eq -- '--clobber|gh release upload' "${workflow}" "${draft}"; then
  echo 'release workflow permits replacement or post-creation asset mutation' >&2
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
