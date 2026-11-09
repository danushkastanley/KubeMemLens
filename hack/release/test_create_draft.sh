#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-release-draft-test.XXXXXX")
bundle=${work_dir}/bundle
fake_bin=${work_dir}/bin
log=${work_dir}/gh.log

cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

mkdir -p "${bundle}" "${fake_bin}"
touch \
  "${bundle}/kube-memlens_1.2.3_linux_amd64.tar.gz" \
  "${bundle}/kube-memlens_1.2.3_windows_amd64.zip" \
  "${bundle}/kube-memlens-1.2.3.chart.sbom.json" \
  "${bundle}/checksums.txt" \
  "${bundle}/checksums.txt.sigstore.json" \
  "${bundle}/kube-memlens-1.2.3.tgz" \
  "${bundle}/memlens.yaml" \
  "${bundle}/release-subjects.txt" \
  "${bundle}/candidate-manifest.sigstore.json" \
  "${bundle}/promotion-checksums.txt" \
  "${bundle}/promotion-checksums.sigstore.json" \
  "${bundle}/promotion-subjects.txt"
digest_a="sha256:$(printf 'a%.0s' {1..64})"
digest_b="sha256:$(printf 'b%.0s' {1..64})"
jq -cn --arg image_digest "${digest_a}" --arg chart_digest "${digest_b}" '
  {intended_ga_tag:"v1.2.3",
   image:{repository:"ghcr.io/danushkastanley/candidates/1.2.3-rc.1/kube-memlens",digest:$image_digest},
   chart:{repository:"ghcr.io/danushkastanley/candidates/1.2.3-rc.1/charts/kube-memlens",digest:$chart_digest}}' \
  > "${bundle}/candidate-manifest.json"

cat > "${fake_bin}/gh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
if [ "${1:-}" = api ]; then
  printf '[]\n'
  exit 0
fi
# Keep one log line per call; multi-line release notes are flattened.
args="$*"
printf '%s\n' "${args//$'\n'/\\n}" >> "${GH_LOG}"
if [ "${GH_FAIL_CREATE:-false}" = true ] && [ "${1:-}" = release ] && [ "${2:-}" = create ]; then
  exit 1
fi
SH
chmod +x "${fake_bin}/gh"

PATH="${fake_bin}:${PATH}" \
GH_LOG="${log}" \
RELEASE_BUNDLE_DIR="${bundle}" \
RELEASE_VERSION=v1.2.3 \
  "${root}/hack/release/create_draft.sh"

test "$(wc -l < "${log}" | tr -d ' ')" -eq 2
sed -n '1p' "${log}" | grep -Fq 'release create v1.2.3'
sed -n '1p' "${log}" | grep -Fq -- '--title INCOMPLETE publication: v1.2.3'
sed -n '2p' "${log}" | grep -Fq 'release edit v1.2.3 --draft --title v1.2.3'

: > "${log}"
if PATH="${fake_bin}:${PATH}" \
  GH_LOG="${log}" \
  GH_FAIL_CREATE=true \
  RELEASE_BUNDLE_DIR="${bundle}" \
  RELEASE_VERSION=v1.2.3 \
    "${root}/hack/release/create_draft.sh"; then
  echo 'simulated partial draft creation succeeded' >&2
  exit 1
fi
test "$(wc -l < "${log}" | tr -d ' ')" -eq 1
grep -Fq -- '--title INCOMPLETE publication: v1.2.3' "${log}"

: > "${log}"
PATH="${fake_bin}:${PATH}" \
GH_LOG="${log}" \
RELEASE_BUNDLE_DIR="${bundle}" \
RELEASE_CANDIDATE_TAG=v1.2.3-rc.1 \
  "${root}/hack/release/create_candidate_draft.sh"
test "$(wc -l < "${log}" | tr -d ' ')" -eq 2
sed -n '1p' "${log}" | grep -Fq 'release create v1.2.3-rc.1'
sed -n '1p' "${log}" | grep -Fq -- '--prerelease --verify-tag --title INCOMPLETE qualification: v1.2.3-rc.1'
sed -n '2p' "${log}" | grep -Fq 'release edit v1.2.3-rc.1 --draft --prerelease --title v1.2.3-rc.1 candidate for v1.2.3'
candidate_notes=$(sed -n '2p' "${log}")
for expected in \
  '--notes Prospective v1.2.3 artefacts.' \
  'oci://ghcr.io/danushkastanley/candidates/1.2.3-rc.1/charts/kube-memlens' \
  '--version 1.2.3' \
  '--set image.repository=ghcr.io/danushkastanley/candidates/1.2.3-rc.1/kube-memlens' \
  "--set image.digest=${digest_a}" \
  "Chart digest: \`${digest_b}\`" \
  'release evidence issue #50'; do
  grep -Fq -- "${expected}" <<< "${candidate_notes}" || {
    echo "candidate notes are missing: ${expected}" >&2
    exit 1
  }
done

: > "${log}"
jq 'del(.image.digest)' "${bundle}/candidate-manifest.json" > "${work_dir}/incomplete.json"
cp "${bundle}/candidate-manifest.json" "${work_dir}/complete.json"
cp "${work_dir}/incomplete.json" "${bundle}/candidate-manifest.json"
if PATH="${fake_bin}:${PATH}" \
  GH_LOG="${log}" \
  RELEASE_BUNDLE_DIR="${bundle}" \
  RELEASE_CANDIDATE_TAG=v1.2.3-rc.1 \
    "${root}/hack/release/create_candidate_draft.sh" 2>/dev/null; then
  echo 'candidate draft completed without an image digest' >&2
  exit 1
fi
if grep -Fq 'release edit' "${log}"; then
  echo 'candidate draft was marked complete without install notes' >&2
  exit 1
fi
cp "${work_dir}/complete.json" "${bundle}/candidate-manifest.json"

: > "${log}"
PATH="${fake_bin}:${PATH}" \
GH_LOG="${log}" \
RELEASE_BUNDLE_DIR="${bundle}" \
RELEASE_VERSION=v1.2.3 \
  "${root}/hack/release/create_promoted_draft.sh"
test "$(wc -l < "${log}" | tr -d ' ')" -eq 2
sed -n '1p' "${log}" | grep -Fq 'release create v1.2.3'
sed -n '1p' "${log}" | grep -Fq -- '--title INCOMPLETE promotion: v1.2.3'
sed -n '2p' "${log}" | grep -Fq 'release edit v1.2.3 --draft --title v1.2.3'

echo 'release draft failure-state tests passed'
