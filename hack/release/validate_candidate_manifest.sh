#!/usr/bin/env bash
set -Eeuo pipefail

manifest=${1:?usage: validate_candidate_manifest.sh MANIFEST EXPECTED_CANDIDATE_TAG EXPECTED_GA_TAG EXPECTED_COMMIT}
expected_candidate_tag=${2:?usage: validate_candidate_manifest.sh MANIFEST EXPECTED_CANDIDATE_TAG EXPECTED_GA_TAG EXPECTED_COMMIT}
expected_ga_tag=${3:?usage: validate_candidate_manifest.sh MANIFEST EXPECTED_CANDIDATE_TAG EXPECTED_GA_TAG EXPECTED_COMMIT}
expected_commit=${4:?usage: validate_candidate_manifest.sh MANIFEST EXPECTED_CANDIDATE_TAG EXPECTED_GA_TAG EXPECTED_COMMIT}

candidate_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rc\.[1-9][0-9]*$'
ga_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
commit_pattern='^[0-9a-f]{40}$'

if [ ! -f "${manifest}" ] || [ -L "${manifest}" ]; then
  echo "candidate manifest must be a regular, non-symbolic-link file: ${manifest}" >&2
  exit 1
fi

command -v jq >/dev/null 2>&1 || {
  echo "required candidate manifest validation tool is unavailable: jq" >&2
  exit 1
}

if [[ ! "${expected_candidate_tag}" =~ ${candidate_pattern} ]]; then
  echo "expected candidate tag is invalid: ${expected_candidate_tag}" >&2
  exit 1
fi
if [[ ! "${expected_ga_tag}" =~ ${ga_pattern} ]]; then
  echo "expected GA tag is invalid: ${expected_ga_tag}" >&2
  exit 1
fi
if [ "${expected_candidate_tag%%-rc.*}" != "${expected_ga_tag}" ]; then
  echo "candidate and intended GA tags do not share a base version" >&2
  exit 1
fi
if [[ ! "${expected_commit}" =~ ${commit_pattern} ]]; then
  echo "expected source commit must be a full lowercase 40-character Git SHA" >&2
  exit 1
fi

if ! canonical_manifest=$(jq -ceS . "${manifest}" 2>/dev/null); then
  echo "candidate manifest is not valid JSON" >&2
  exit 1
fi

if [ "$(<"${manifest}")" != "${canonical_manifest}" ]; then
  echo "candidate manifest must use canonical sorted compact JSON encoding" >&2
  exit 1
fi

candidate_version=${expected_candidate_tag#v}
ga_version=${expected_ga_tag#v}
expected_archives=(
  "kube-memlens_${ga_version}_darwin_amd64.tar.gz"
  "kube-memlens_${ga_version}_darwin_arm64.tar.gz"
  "kube-memlens_${ga_version}_linux_amd64.tar.gz"
  "kube-memlens_${ga_version}_linux_arm64.tar.gz"
  "kube-memlens_${ga_version}_windows_amd64.zip"
  "kube-memlens_${ga_version}_windows_arm64.zip"
)
archive_names=$(jq -cn --args '$ARGS.positional | sort' "${expected_archives[@]}")
expected_chart_package="kube-memlens-${ga_version}.tgz"
expected_image_repository="ghcr.io/danushkastanley/candidates/${candidate_version}/kube-memlens"
expected_chart_repository="ghcr.io/danushkastanley/candidates/${candidate_version}/charts/kube-memlens"
expected_workflow_identity="https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/${expected_candidate_tag}"

if ! jq -e \
  --arg candidate_tag "${expected_candidate_tag}" \
  --arg ga_tag "${expected_ga_tag}" \
  --arg commit "${expected_commit}" \
  --argjson archive_names "${archive_names}" \
  --arg chart_package "${expected_chart_package}" \
  --arg image_repository "${expected_image_repository}" \
  --arg chart_repository "${expected_chart_repository}" \
  --arg workflow_identity "${expected_workflow_identity}" '
    def exact_keys($expected):
      type == "object" and ((keys | sort) == ($expected | sort));
    def sha256:
      type == "string" and test("^[0-9a-f]{64}$");
    def oci_digest:
      type == "string" and test("^sha256:[0-9a-f]{64}$");

    exact_keys([
      "schema_version",
      "candidate_tag",
      "intended_ga_tag",
      "source_commit",
      "cli_archives",
      "image",
      "chart",
      "workflow_identity"
    ]) and
    .schema_version == 1 and
    .candidate_tag == $candidate_tag and
    .intended_ga_tag == $ga_tag and
    .source_commit == $commit and
    (.cli_archives | exact_keys($archive_names)) and
    all(.cli_archives[]; sha256) and
    (.image | exact_keys(["repository", "digest"])) and
    .image.repository == $image_repository and
    (.image.digest | oci_digest) and
    (.chart | exact_keys(["repository", "digest", "package"])) and
    .chart.repository == $chart_repository and
    (.chart.digest | oci_digest) and
    (.chart.package | exact_keys(["name", "sha256"])) and
    .chart.package.name == $chart_package and
    (.chart.package.sha256 | sha256) and
    .workflow_identity == $workflow_identity
  ' "${manifest}" >/dev/null; then
  echo "candidate manifest does not satisfy the release promotion contract" >&2
  exit 1
fi

printf '%s\n' "${canonical_manifest}"
