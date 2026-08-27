#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-candidate-manifest-test.XXXXXX")
manifest=${work_dir}/candidate-manifest.json
candidate_tag=v1.2.3-rc.4
ga_tag=v1.2.3
commit=0123456789abcdef0123456789abcdef01234567
candidate_version=${candidate_tag#v}
ga_version=${ga_tag#v}

cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

write_valid_manifest() {
  jq -cS -n \
    --arg candidate_tag "${candidate_tag}" \
    --arg ga_tag "${ga_tag}" \
    --arg commit "${commit}" \
    --arg candidate_version "${candidate_version}" \
    --arg ga_version "${ga_version}" '
      {
        schema_version: 1,
        candidate_tag: $candidate_tag,
        intended_ga_tag: $ga_tag,
        source_commit: $commit,
        cli_archives: {
          ("kube-memlens_" + $ga_version + "_darwin_amd64.tar.gz"): ("1" * 64),
          ("kube-memlens_" + $ga_version + "_darwin_arm64.tar.gz"): ("2" * 64),
          ("kube-memlens_" + $ga_version + "_linux_amd64.tar.gz"): ("3" * 64),
          ("kube-memlens_" + $ga_version + "_linux_arm64.tar.gz"): ("4" * 64),
          ("kube-memlens_" + $ga_version + "_windows_amd64.zip"): ("5" * 64),
          ("kube-memlens_" + $ga_version + "_windows_arm64.zip"): ("6" * 64)
        },
        image: {
          repository: (
            "ghcr.io/danushkastanley/candidates/" + $candidate_version + "/kube-memlens"
          ),
          digest: ("sha256:" + ("a" * 64))
        },
        chart: {
          repository: (
            "ghcr.io/danushkastanley/candidates/" + $candidate_version + "/charts/kube-memlens"
          ),
          digest: ("sha256:" + ("b" * 64)),
          package: {
            name: ("kube-memlens-" + $ga_version + ".tgz"),
            sha256: ("c" * 64)
          }
        },
        workflow_identity: (
          "https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/" +
          $candidate_tag
        )
      }
    ' > "${manifest}"
}

expect_rejected() {
  local description=$1
  shift
  if "$@" >/dev/null 2>&1; then
    echo "candidate manifest validation accepted ${description}" >&2
    exit 1
  fi
}

validate() {
  "${root}/hack/release/validate_candidate_manifest.sh" \
    "${manifest}" "${candidate_tag}" "${ga_tag}" "${commit}"
}

write_valid_manifest
canonical=$(validate)
test "${canonical}" = "$(jq -cS . "${manifest}")"

expect_rejected 'a malformed expected candidate tag' \
  "${root}/hack/release/validate_candidate_manifest.sh" \
  "${manifest}" v1.2.3-rc.0 "${ga_tag}" "${commit}"
expect_rejected 'a mismatched intended GA version' \
  "${root}/hack/release/validate_candidate_manifest.sh" \
  "${manifest}" "${candidate_tag}" v1.2.4 "${commit}"
expect_rejected 'an abbreviated expected commit' \
  "${root}/hack/release/validate_candidate_manifest.sh" \
  "${manifest}" "${candidate_tag}" "${ga_tag}" "${commit:0:12}"

write_valid_manifest
jq -cS '.candidate_tag = "v1.2.3-rc.5"' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a candidate identity different from the trusted input' validate

write_valid_manifest
jq -cS '.source_commit = ("f" * 40)' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a source commit different from the trusted input' validate

write_valid_manifest
jq -cS '(.cli_archives | keys[0]) as $key | del(.cli_archives[$key])' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a missing CLI archive digest' validate

write_valid_manifest
jq -cS '(.cli_archives | keys[0]) as $key | .cli_archives[$key] = ("A" * 64)' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a malformed CLI archive digest' validate

write_valid_manifest
jq -cS '
  .image.repository = "ghcr.io/danushkastanley/candidates/1.2.3-rc.5/kube-memlens"
' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'an image identity from a different candidate' validate

write_valid_manifest
jq -cS '.image.digest = ("sha512:" + ("a" * 64))' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a malformed image OCI digest' validate

write_valid_manifest
jq -cS '
  .chart.repository = "ghcr.io/danushkastanley/candidates/1.2.3-rc.5/charts/kube-memlens"
' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a chart identity from a different candidate' validate

write_valid_manifest
jq -cS '.chart.digest = ("sha256:" + ("b" * 63))' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a malformed chart OCI digest' validate

write_valid_manifest
jq -cS '.chart.package.name = "kube-memlens-1.2.3-rc.4.tgz"' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a chart package for a different version' validate

write_valid_manifest
jq -cS '.chart.package.sha256 = ("0" * 63)' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a malformed chart package digest' validate

write_valid_manifest
jq -cS '.workflow_identity |= sub("candidate.yml"; "publish.yml")' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'a tampered workflow identity' validate

write_valid_manifest
jq -cS '.unexpected = true' "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'an unknown manifest field' validate

printf '%s\n' '{"schema_version":1,"schema_version":1}' > "${manifest}"
expect_rejected 'duplicate JSON fields' validate

write_valid_manifest
jq . "${manifest}" > "${manifest}.tmp"
mv "${manifest}.tmp" "${manifest}"
expect_rejected 'non-canonical JSON encoding' validate

printf '%s\n' '{' > "${manifest}"
expect_rejected 'malformed JSON' validate

ln -s "${manifest}" "${manifest}.link"
expect_rejected 'a symbolic-link manifest' \
  "${root}/hack/release/validate_candidate_manifest.sh" \
  "${manifest}.link" "${candidate_tag}" "${ga_tag}" "${commit}"

echo 'candidate manifest contract tests passed'
