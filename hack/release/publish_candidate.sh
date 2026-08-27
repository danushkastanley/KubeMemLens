#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${1:?usage: publish_candidate.sh BUNDLE}
candidate_tag=${CANDIDATE_TAG:?CANDIDATE_TAG is required}
ga_tag=${GA_TAG:?GA_TAG is required}
ga_version=${GA_VERSION:?GA_VERSION is required}
commit=${GITHUB_SHA:?GITHUB_SHA is required}
image=${CANDIDATE_IMAGE_REPOSITORY:?CANDIDATE_IMAGE_REPOSITORY is required}
chart=${CANDIDATE_CHART_REPOSITORY:?CANDIDATE_CHART_REPOSITORY is required}
token=${GH_TOKEN:?GH_TOKEN is required}
skopeo_image=${SKOPEO_IMAGE:?SKOPEO_IMAGE is required}

for tool in cosign docker helm jq oras sha256sum; do
  command -v "${tool}" >/dev/null || {
    echo "candidate publication requires ${tool}" >&2
    exit 1
  }
done

expected_version=${candidate_tag#v}
expected_image="ghcr.io/danushkastanley/candidates/${expected_version}/kube-memlens"
expected_chart="ghcr.io/danushkastanley/candidates/${expected_version}/charts/kube-memlens"
test "${candidate_tag%%-rc.*}" = "${ga_tag}"
test "${ga_tag#v}" = "${ga_version}"
test "${image}" = "${expected_image}"
test "${chart}" = "${expected_chart}"

bundle=$(cd "${bundle}" && pwd)
(
  cd "${bundle}"
  sha256sum --check internal/transfer-checksums.txt
  sha256sum --check checksums.txt
)

image_digest=$(<"${bundle}/internal/image-digest.txt")
case "${image_digest}" in sha256:????????????????????????????????????????????????????????????????) ;; *) exit 2 ;; esac

set +e
image_preflight=$(docker run --rm \
  -v "${DOCKER_CONFIG:-$HOME/.docker}:/auth:ro" \
  "${skopeo_image}" inspect --authfile /auth/config.json \
  "docker://${image}:${ga_version}" 2>&1)
image_preflight_status=$?
chart_preflight=$(oras manifest fetch --descriptor "${chart}:${ga_version}" 2>&1)
chart_preflight_status=$?
set -e
existing_image_digest=
if [ "${image_preflight_status}" -eq 0 ]; then
  existing_image_digest=$(docker run --rm \
    -v "${DOCKER_CONFIG:-$HOME/.docker}:/auth:ro" \
    "${skopeo_image}" inspect --raw --authfile /auth/config.json \
    "docker://${image}:${ga_version}" | sha256sum | awk '{print "sha256:" $1}')
  test "${existing_image_digest}" = "${image_digest}" || {
    echo "candidate image exists with a different digest: ${image}:${ga_version}" >&2
    exit 1
  }
elif ! grep -Eqi 'not found|manifest unknown' <<< "${image_preflight}"; then
  echo "candidate image preflight failed: ${image_preflight}" >&2
  exit 1
fi
if [ "${chart_preflight_status}" -ne 0 ] && \
  ! grep -Eqi 'not found|manifest unknown' <<< "${chart_preflight}"; then
  echo "candidate chart preflight failed: ${chart_preflight}" >&2
  exit 1
fi

if [ -z "${existing_image_digest}" ]; then
  docker run --rm \
    -v "${bundle}:/work:ro" \
    -v "${DOCKER_CONFIG:-$HOME/.docker}:/auth:ro" \
    "${skopeo_image}" copy --all --preserve-digests \
    --authfile /auth/config.json \
    oci-archive:/work/internal/kube-memlens-image.tar \
    "docker://${image}:${ga_version}"
fi
published_image_digest=$(docker run --rm \
  -v "${DOCKER_CONFIG:-$HOME/.docker}:/auth:ro" \
  "${skopeo_image}" inspect --raw --authfile /auth/config.json \
  "docker://${image}:${ga_version}" | sha256sum | awk '{print "sha256:" $1}')
test "${published_image_digest}" = "${image_digest}"
cosign sign --yes "${image}@${image_digest}"

helm_config=${RUNNER_TEMP:?RUNNER_TEMP is required}/candidate-helm/config.json
mkdir -p "$(dirname "${helm_config}")"
printf '%s' "${token}" | helm registry login ghcr.io \
  --registry-config "${helm_config}" --username "${GITHUB_ACTOR}" --password-stdin
if [ "${chart_preflight_status}" -eq 0 ]; then
  chart_digest=$(jq -r .digest <<< "${chart_preflight}")
  existing_chart_dir=$(mktemp -d "${RUNNER_TEMP}/candidate-chart-existing.XXXXXX")
  helm pull "oci://${chart}" --version "${ga_version}" --destination "${existing_chart_dir}" \
    --registry-config "${helm_config}" >/dev/null
  cmp "${bundle}/kube-memlens-${ga_version}.tgz" \
    "${existing_chart_dir}/kube-memlens-${ga_version}.tgz" || {
    echo "candidate chart exists with different package bytes: ${chart}:${ga_version}" >&2
    exit 1
  }
  rm -rf "${existing_chart_dir}"
else
  chart_output=$(helm push "${bundle}/kube-memlens-${ga_version}.tgz" \
    "oci://${chart%/*}" --registry-config "${helm_config}" 2>&1)
  printf '%s\n' "${chart_output}"
  chart_digest=$(grep -Eo 'sha256:[0-9a-f]{64}' <<< "${chart_output}" | tail -1)
fi
case "${chart_digest}" in sha256:????????????????????????????????????????????????????????????????) ;; *) exit 2 ;; esac
published_chart_digest=$(oras manifest fetch --descriptor "${chart}:${ga_version}" | jq -r .digest)
test "${published_chart_digest}" = "${chart_digest}"
cosign sign --yes "${chart}@${chart_digest}"

archive_json=$(find "${bundle}" -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.zip' \) \
  -print0 | sort -z | xargs -0 sha256sum | \
  jq -Rn '[inputs | capture("^(?<digest>[0-9a-f]{64})  (?<path>.*)$") |
    {key: (.path | split("/") | last), value: .digest}] | from_entries')
chart_package="kube-memlens-${ga_version}.tgz"
chart_sha=$(sha256sum "${bundle}/${chart_package}" | awk '{print $1}')
workflow_identity="https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/${candidate_tag}"
jq -cS -n \
  --arg candidate_tag "${candidate_tag}" --arg ga_tag "${ga_tag}" \
  --arg commit "${commit}" --argjson archives "${archive_json}" \
  --arg image "${image}" --arg image_digest "${image_digest}" \
  --arg chart "${chart}" --arg chart_digest "${chart_digest}" \
  --arg chart_package "${chart_package}" --arg chart_sha "${chart_sha}" \
  --arg workflow_identity "${workflow_identity}" '
    {schema_version: 1, candidate_tag: $candidate_tag, intended_ga_tag: $ga_tag,
     source_commit: $commit, cli_archives: $archives,
     image: {repository: $image, digest: $image_digest},
     chart: {repository: $chart, digest: $chart_digest,
             package: {name: $chart_package, sha256: $chart_sha}},
     workflow_identity: $workflow_identity}' > "${bundle}/candidate-manifest.json"
"$(dirname "${BASH_SOURCE[0]}")/validate_candidate_manifest.sh" \
  "${bundle}/candidate-manifest.json" "${candidate_tag}" "${ga_tag}" "${commit}" >/dev/null

{
  echo "tag=${ga_tag}"
  echo "candidate=${candidate_tag}"
  echo "commit=${commit}"
  echo "image=${image}@${image_digest}"
  echo "chart=${chart}@${chart_digest}"
} > "${bundle}/release-subjects.txt"
(
  cd "${bundle}"
  sha256sum candidate-manifest.json release-subjects.txt >> checksums.txt
  sha256sum --check checksums.txt
)
cosign sign-blob --yes --bundle="${bundle}/candidate-manifest.sigstore.json" \
  "${bundle}/candidate-manifest.json"
cosign sign-blob --yes --bundle="${bundle}/checksums.txt.sigstore.json" \
  "${bundle}/checksums.txt"

echo "image-digest=${image_digest}" >> "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
echo "chart-digest=${chart_digest}" >> "${GITHUB_OUTPUT}"
