#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${RELEASE_BUNDLE_DIR:?RELEASE_BUNDLE_DIR is required}
candidate_tag=${RELEASE_CANDIDATE_TAG:?RELEASE_CANDIDATE_TAG is required}
commit=${RELEASE_COMMIT:?RELEASE_COMMIT is required}
identity=${RELEASE_CERTIFICATE_IDENTITY:?RELEASE_CERTIFICATE_IDENTITY is required}
issuer=${RELEASE_CERTIFICATE_ISSUER:-https://token.actions.githubusercontent.com}
repository=${RELEASE_REPOSITORY:-danushkastanley/KubeMemLens}

for tool in cosign gh jq sha256sum; do
  command -v "${tool}" >/dev/null || {
    echo "candidate draft publication requires ${tool}" >&2
    exit 1
  }
done

releases_json=$(gh api --paginate --slurp "repos/${repository}/releases?per_page=100")
matches=$(jq -c --arg tag "${candidate_tag}" '[.[][] | select(.tag_name == $tag)]' <<< "${releases_json}")
test "$(jq 'length' <<< "${matches}")" -eq 1 || {
  echo 'candidate release lookup must return exactly one matching tag' >&2
  exit 1
}
release_json=$(jq -c '.[0]' <<< "${matches}")
jq -e --arg tag "${candidate_tag}" --arg repository "${repository}" '
  .tag_name == $tag and .draft == true and .prerelease == true and
  .id != null and .assets_url == ("https://api.github.com/repos/" + $repository + "/releases/" + (.id | tostring) + "/assets")
' <<< "${release_json}" >/dev/null || {
  echo 'candidate release must be the exact unpublished prerelease draft' >&2
  exit 1
}

mkdir -p "${bundle}"
bundle=$(cd "${bundle}" && pwd)
test -z "$(find "${bundle}" -mindepth 1 -print -quit)" || {
  echo 'candidate draft download directory must be empty' >&2
  exit 1
}
asset_count=$(jq '.assets | length' <<< "${release_json}")
test "${asset_count}" -gt 0 || {
  echo 'candidate draft has no assets' >&2
  exit 1
}
while IFS=$'\t' read -r asset_id asset_name; do
  if [ -z "${asset_name}" ] || [ "${asset_name}" != "$(basename "${asset_name}")" ]; then
    echo "candidate draft contains an unsafe asset name: ${asset_name}" >&2
    exit 1
  fi
  [ ! -e "${bundle}/${asset_name}" ] || {
    echo "candidate draft contains a duplicate asset name: ${asset_name}" >&2
    exit 1
  }
  gh api -H 'Accept: application/octet-stream' \
    "repos/${repository}/releases/assets/${asset_id}" > "${bundle}/${asset_name}"
done < <(jq -r '.assets[] | [.id, .name] | @tsv' <<< "${release_json}")

ga_tag=${candidate_tag%%-rc.*}
"$(dirname "${BASH_SOURCE[0]}")/validate_candidate_manifest.sh" \
  "${bundle}/candidate-manifest.json" "${candidate_tag}" "${ga_tag}" "${commit}" >/dev/null
cosign verify-blob --bundle "${bundle}/candidate-manifest.sigstore.json" \
  --certificate-identity "${identity}" --certificate-oidc-issuer "${issuer}" \
  "${bundle}/candidate-manifest.json" >/dev/null
cosign verify-blob --bundle "${bundle}/checksums.txt.sigstore.json" \
  --certificate-identity "${identity}" --certificate-oidc-issuer "${issuer}" \
  "${bundle}/checksums.txt" >/dev/null
(
  cd "${bundle}"
  sha256sum --check checksums.txt
)

image_subject=$(jq -r '.image.repository + "@" + .image.digest' \
  "${bundle}/candidate-manifest.json")
chart_subject=$(jq -r '.chart.repository + "@" + .chart.digest' \
  "${bundle}/candidate-manifest.json")
expected_subjects=$(printf 'tag=%s\ncandidate=%s\ncommit=%s\nimage=%s\nchart=%s' \
  "${ga_tag}" "${candidate_tag}" "${commit}" "${image_subject}" "${chart_subject}")
test "$(<"${bundle}/release-subjects.txt")" = "${expected_subjects}" || {
  echo 'candidate subject inventory differs from the signed manifest' >&2
  exit 1
}

expected_asset_names=()
while read -r digest name extra; do
  [[ "${digest}" =~ ^[0-9a-f]{64}$ ]] || {
    echo 'candidate checksum inventory contains an invalid digest' >&2
    exit 1
  }
  if [ -n "${extra}" ] || [ -z "${name}" ] || \
    [ "${name}" != "$(basename "${name}")" ]; then
    echo 'candidate checksum inventory contains an unsafe asset name' >&2
    exit 1
  fi
  expected_asset_names+=("${name}")
done < "${bundle}/checksums.txt"
expected_assets=$(printf '%s\n' "${expected_asset_names[@]}" \
  'checksums.txt
checksums.txt.sigstore.json
candidate-manifest.sigstore.json' | LC_ALL=C sort -u)
downloaded_assets=$(find "${bundle}" -maxdepth 1 -type f -exec basename '{}' \; | LC_ALL=C sort)
test "${downloaded_assets}" = "${expected_assets}" || {
  echo 'candidate draft asset inventory differs from the signed checksum set' >&2
  exit 1
}

release_id=$(jq -r .id <<< "${release_json}")
release_json_after=$(gh api "repos/${repository}/releases/${release_id}")
before_state=$(jq -cS '{id, tag_name, draft, prerelease, assets: ([.assets[] | {id, name, size, updated_at}] | sort_by(.id, .name))}' \
  <<< "${release_json}")
after_state=$(jq -cS '{id, tag_name, draft, prerelease, assets: ([.assets[] | {id, name, size, updated_at}] | sort_by(.id, .name))}' \
  <<< "${release_json_after}")
test "${after_state}" = "${before_state}" || {
  echo 'candidate release state changed during verification' >&2
  exit 1
}
jq -nc '{draft: false, prerelease: true}' | \
  gh api --method PATCH "repos/${repository}/releases/${release_id}" --input - >/dev/null
