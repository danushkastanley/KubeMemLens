#!/usr/bin/env bash
set -Eeuo pipefail

# Stages the Krew manifest of a published stable release for krew-index.
# The staged file is the signed release asset itself, never a second
# rendering, so the index receives the bytes that promotion signed.

tag=${1:?usage: verify_krew_release.sh TAG OUTPUT}
output=${2:?usage: verify_krew_release.sh TAG OUTPUT}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
release_url="https://github.com/danushkastanley/KubeMemLens/releases/download/${tag}"
identity="https://github.com/danushkastanley/KubeMemLens/.github/workflows/promote.yml@refs/tags/${tag}"
issuer=https://token.actions.githubusercontent.com

fail() {
  echo "$1" >&2
  exit 1
}

if [[ ! "${tag}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  fail "Krew index updates need a stable vX.Y.Z release tag, not: ${tag}"
fi

for tool in cmp cosign curl sha256sum; do
  command -v "${tool}" >/dev/null || fail "Krew release verification requires ${tool}"
done

work=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-krew-release.XXXXXX")
trap 'rm -rf "${work}"' EXIT

# Downloads carry no credential. A draft or missing release is not publicly
# readable, so nothing is submitted before the release is published.
fetch() {
  local url=$1
  curl --fail --silent --show-error --location --retry 3 \
    --connect-timeout 20 --max-time 300 \
    --output "${work}/${url##*/}" "${url}" ||
    fail "published release asset is not publicly downloadable: ${url}"
}

for asset in memlens.yaml checksums.txt promotion-checksums.txt promotion-checksums.sigstore.json; do
  fetch "${release_url}/${asset}"
done

cosign verify-blob --bundle "${work}/promotion-checksums.sigstore.json" \
  --certificate-identity "${identity}" --certificate-oidc-issuer "${issuer}" \
  "${work}/promotion-checksums.txt" >/dev/null ||
  fail "promotion-checksums.txt is not signed by the ${tag} promotion workflow"

awk '$2 == "checksums.txt" || $2 == "memlens.yaml"' "${work}/promotion-checksums.txt" \
  > "${work}/signed-files.txt"
if [ "$(wc -l < "${work}/signed-files.txt" | tr -d ' ')" != 2 ]; then
  fail 'signed promotion record does not cover both checksums.txt and memlens.yaml'
fi
(cd "${work}" && sha256sum --check signed-files.txt >/dev/null) ||
  fail 'checksums.txt or memlens.yaml differs from the signed promotion record'

(cd "${root}" && hack/render-krew-manifest.sh "${tag}" "${work}/checksums.txt" "${work}/rendered-memlens.yaml")
cmp -s "${work}/rendered-memlens.yaml" "${work}/memlens.yaml" ||
  fail 'signed memlens.yaml differs from deploy/krew/memlens.yaml.tmpl rendered with the signed checksums'

: > "${work}/archives.txt"
while IFS= read -r uri; do
  archive_checksum=$(awk -v name="${uri##*/}" '$2 == name' "${work}/checksums.txt")
  [ -n "${archive_checksum}" ] || fail "signed checksums do not list the Krew archive: ${uri##*/}"
  printf '%s\n' "${archive_checksum}" >> "${work}/archives.txt"
  fetch "${uri}"
done < <(awk '$1 == "uri:" {print $2}' "${work}/memlens.yaml")
[ -s "${work}/archives.txt" ] || fail 'memlens.yaml names no release archives'
(cd "${work}" && sha256sum --check archives.txt >/dev/null) ||
  fail 'a published Krew archive differs from the signed checksums'

mkdir -p "$(dirname "${output}")"
cp "${work}/memlens.yaml" "${output}"
echo "Krew manifest for ${tag} matches the signed release and its public archives: ${output}"
