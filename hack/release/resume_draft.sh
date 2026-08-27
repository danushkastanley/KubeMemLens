#!/usr/bin/env bash
set -Eeuo pipefail

tag=${1:?usage: resume_draft.sh TAG EXPECTED_ASSETS PRERELEASE}
expected=${2:?usage: resume_draft.sh TAG EXPECTED_ASSETS PRERELEASE}
prerelease=${3:?usage: resume_draft.sh TAG EXPECTED_ASSETS PRERELEASE}
repository=${RELEASE_REPOSITORY:-danushkastanley/KubeMemLens}

case "${prerelease}" in true|false) ;; *) exit 2 ;; esac
releases=$(gh api --paginate --slurp "repos/${repository}/releases?per_page=100")
matches=$(jq -c --arg tag "${tag}" '[.[][] | select(.tag_name == $tag)]' <<< "${releases}")
count=$(jq 'length' <<< "${matches}")
if [ "${count}" -eq 0 ]; then
  exit 3
fi
test "${count}" -eq 1 || {
  echo "release lookup returned multiple entries for ${tag}" >&2
  exit 1
}
release=$(jq -c '.[0]' <<< "${matches}")
jq -e --arg tag "${tag}" --argjson prerelease "${prerelease}" '
  .tag_name == $tag and .draft == true and .prerelease == $prerelease and .id != null
' <<< "${release}" >/dev/null || {
  echo "existing release is not the expected draft: ${tag}" >&2
  exit 1
}

downloaded=$(mktemp -d "${RUNNER_TEMP:-/tmp}/kube-memlens-draft-resume.XXXXXX")
cleanup() {
  rm -rf "${downloaded}"
}
trap cleanup EXIT
while IFS=$'\t' read -r asset_id asset_name; do
  if [ -z "${asset_name}" ] || [ "${asset_name}" != "$(basename "${asset_name}")" ]; then
    echo "existing draft contains an unsafe asset name: ${asset_name}" >&2
    exit 1
  fi
  [ ! -e "${downloaded}/${asset_name}" ] || {
    echo "existing draft contains duplicate asset name: ${asset_name}" >&2
    exit 1
  }
  gh api -H 'Accept: application/octet-stream' \
    "repos/${repository}/releases/assets/${asset_id}" > "${downloaded}/${asset_name}"
done < <(jq -r '.assets[] | [.id, .name] | @tsv' <<< "${release}")

"$(dirname "${BASH_SOURCE[0]}")/verify_draft_assets.sh" "${expected}" "${downloaded}" >/dev/null
echo "existing exact draft is safe to resume: ${tag}"
