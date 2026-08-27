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
  "${bundle}/release-subjects.txt"

cat > "${fake_bin}/gh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$*" >> "${GH_LOG}"
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

echo 'release draft failure-state tests passed'
