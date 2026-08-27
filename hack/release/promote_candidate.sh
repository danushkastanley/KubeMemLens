#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${1:?usage: promote_candidate.sh BUNDLE}
candidate_tag=${CANDIDATE_TAG:?CANDIDATE_TAG is required}
ga_tag=${GA_TAG:?GA_TAG is required}
commit=${GITHUB_SHA:?GITHUB_SHA is required}
: "${GH_TOKEN:?GH_TOKEN is required}"
skopeo_image=${SKOPEO_IMAGE:?SKOPEO_IMAGE is required}
repository=${GITHUB_REPOSITORY:-danushkastanley/KubeMemLens}
production_image=ghcr.io/danushkastanley/kube-memlens
production_chart=ghcr.io/danushkastanley/charts/kube-memlens
ga_version=${ga_tag#v}
candidate_identity="https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/${candidate_tag}"

for tool in cosign docker gh helm jq oras sha256sum; do
  command -v "${tool}" >/dev/null || {
    echo "candidate promotion requires ${tool}" >&2
    exit 1
  }
done

bundle=$(cd "${bundle}" && pwd)
"$(dirname "${BASH_SOURCE[0]}")/validate_tag.sh" "${ga_tag}" "${commit}"
candidate_commit=$(git rev-parse "refs/tags/${candidate_tag}^{commit}")
test "${candidate_commit}" = "${commit}" || {
  echo "candidate and GA tags do not target the same commit" >&2
  exit 1
}
"$(dirname "${BASH_SOURCE[0]}")/validate_candidate_manifest.sh" \
  "${bundle}/candidate-manifest.json" "${candidate_tag}" "${ga_tag}" "${commit}" >/dev/null

release_json=$(gh api "repos/${repository}/releases/tags/${candidate_tag}")
jq -e '.draft == false and .prerelease == true' <<< "${release_json}" >/dev/null || {
  echo "candidate release must be a published prerelease" >&2
  exit 1
}
cosign verify-blob --bundle "${bundle}/candidate-manifest.sigstore.json" \
  --certificate-identity "${candidate_identity}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "${bundle}/candidate-manifest.json" >/dev/null
cosign verify-blob --bundle "${bundle}/checksums.txt.sigstore.json" \
  --certificate-identity "${candidate_identity}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "${bundle}/checksums.txt" >/dev/null
(
  cd "${bundle}"
  sha256sum --check checksums.txt
)

candidate_image=$(jq -r .image.repository "${bundle}/candidate-manifest.json")
image_digest=$(jq -r .image.digest "${bundle}/candidate-manifest.json")
candidate_chart=$(jq -r .chart.repository "${bundle}/candidate-manifest.json")
chart_digest=$(jq -r .chart.digest "${bundle}/candidate-manifest.json")
cosign verify --certificate-identity "${candidate_identity}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "${candidate_image}@${image_digest}" >/dev/null
cosign verify --certificate-identity "${candidate_identity}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "${candidate_chart}@${chart_digest}" >/dev/null
gh attestation verify "oci://${candidate_image}@${image_digest}" --repo "${repository}" >/dev/null
gh attestation verify "oci://${candidate_chart}@${chart_digest}" --repo "${repository}" >/dev/null

set +e
current_image_raw=$(docker run --rm \
  -v "${DOCKER_CONFIG:-$HOME/.docker}:/auth:ro" "${skopeo_image}" \
  inspect --raw --authfile /auth/config.json "docker://${production_image}:${ga_version}" 2>&1)
current_image_status=$?
set -e
current_image_digest=
if [ "${current_image_status}" -eq 0 ]; then
  current_image_digest=$(printf '%s' "${current_image_raw}" | sha256sum | awk '{print "sha256:" $1}')
elif ! grep -Eqi 'not found|manifest unknown' <<< "${current_image_raw}"; then
  echo "production image preflight failed: ${current_image_raw}" >&2
  exit 1
fi
if [ -n "${current_image_digest}" ] && [ "${current_image_digest}" != "${image_digest}" ]; then
  echo "production image tag exists with a different digest" >&2
  exit 1
fi
if [ -z "${current_image_digest}" ]; then
  docker run --rm \
    -v "${DOCKER_CONFIG:-$HOME/.docker}:/auth:ro" "${skopeo_image}" \
    copy --all --preserve-digests --authfile /auth/config.json \
    "docker://${candidate_image}@${image_digest}" "docker://${production_image}:${ga_version}"
fi
published_image_digest=$(docker run --rm \
  -v "${DOCKER_CONFIG:-$HOME/.docker}:/auth:ro" "${skopeo_image}" \
  inspect --raw --authfile /auth/config.json "docker://${production_image}:${ga_version}" |
  sha256sum | awk '{print "sha256:" $1}')
test "${published_image_digest}" = "${image_digest}"

set +e
current_chart_output=$(oras manifest fetch --descriptor "${production_chart}:${ga_version}" 2>&1)
current_chart_status=$?
set -e
current_chart_digest=
if [ "${current_chart_status}" -eq 0 ]; then
  current_chart_digest=$(jq -r .digest <<< "${current_chart_output}")
elif ! grep -Eqi 'not found|manifest unknown' <<< "${current_chart_output}"; then
  echo "production chart preflight failed: ${current_chart_output}" >&2
  exit 1
fi
if [ -n "${current_chart_digest}" ] && [ "${current_chart_digest}" != "${chart_digest}" ]; then
  echo "production chart tag exists with a different digest" >&2
  exit 1
fi
if [ -z "${current_chart_digest}" ]; then
  oras cp --recursive "${candidate_chart}@${chart_digest}" "${production_chart}:${ga_version}"
fi
published_chart_digest=$(oras manifest fetch --descriptor "${production_chart}:${ga_version}" | jq -r .digest)
test "${published_chart_digest}" = "${chart_digest}"

cosign sign --yes "${production_image}@${image_digest}"
cosign sign --yes "${production_chart}@${chart_digest}"
hack/render-krew-manifest.sh "${ga_tag}" "${bundle}/checksums.txt" "${bundle}/memlens.yaml"
{
  echo "tag=${ga_tag}"
  echo "candidate=${candidate_tag}"
  echo "commit=${commit}"
  echo "image=${production_image}@${image_digest}"
  echo "chart=${production_chart}@${chart_digest}"
} > "${bundle}/promotion-subjects.txt"
(
  cd "${bundle}"
  sha256sum checksums.txt candidate-manifest.json memlens.yaml promotion-subjects.txt \
    > promotion-checksums.txt
  sha256sum --check promotion-checksums.txt
)
cosign sign-blob --yes --bundle="${bundle}/promotion-checksums.sigstore.json" \
  "${bundle}/promotion-checksums.txt"

echo "image-digest=${image_digest}" >> "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
echo "chart-digest=${chart_digest}" >> "${GITHUB_OUTPUT}"
