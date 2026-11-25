#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-krew-release-test.XXXXXX")
pristine=${work}/pristine
release=${work}/release
fake_bin=${work}/bin
output=${work}/staged/memlens.yaml
tag=v1.2.3

trap 'rm -rf "${work}"' EXIT
mkdir -p "${pristine}" "${fake_bin}"

for name in \
  kube-memlens_1.2.3_linux_amd64.tar.gz \
  kube-memlens_1.2.3_linux_arm64.tar.gz \
  kube-memlens_1.2.3_darwin_amd64.tar.gz \
  kube-memlens_1.2.3_darwin_arm64.tar.gz \
  kube-memlens_1.2.3_windows_amd64.zip \
  kube-memlens_1.2.3_windows_arm64.zip; do
  printf '%s\n' "${name}" > "${pristine}/${name}"
done
(cd "${pristine}" && sha256sum kube-memlens_* > checksums.txt)
(cd "${root}" && hack/render-krew-manifest.sh "${tag}" "${pristine}/checksums.txt" "${pristine}/memlens.yaml")
(cd "${pristine}" && sha256sum checksums.txt memlens.yaml > promotion-checksums.txt)
printf 'signature\n' > "${pristine}/promotion-checksums.sigstore.json"

# Serves release assets from a directory and records each requested URL.
cat > "${fake_bin}/curl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
destination=
while [ "$#" -gt 1 ]; do
  if [ "$1" = --output ]; then
    destination=$2
    shift
  fi
  shift
done
printf '%s\n' "$1" >> "${FAKE_CURL_LOG}"
cp "${FAKE_RELEASE_DIR}/${1##*/}" "${destination}" 2>/dev/null || exit 22
SH
cat > "${fake_bin}/cosign" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$*" >> "${FAKE_COSIGN_LOG}"
exit "${FAKE_COSIGN_STATUS}"
SH
chmod +x "${fake_bin}/curl" "${fake_bin}/cosign"

verify() {
  PATH="${fake_bin}:${PATH}" FAKE_RELEASE_DIR="${release}" FAKE_COSIGN_STATUS="${cosign_status}" \
    FAKE_CURL_LOG="${work}/curl.log" FAKE_COSIGN_LOG="${work}/cosign.log" \
    "${root}/hack/release/verify_krew_release.sh" "$1" "${output}"
}

reset_release() {
  rm -rf "${release}" "${work}/staged" "${work}/curl.log" "${work}/cosign.log"
  cp -R "${pristine}" "${release}"
  cosign_status=0
}

expect_rejected() {
  local defect=$1 release_tag=$2 message=$3
  if verify "${release_tag}" > "${work}/failure.log" 2>&1; then
    echo "Krew release verification accepted ${defect}" >&2
    exit 1
  fi
  grep -Fq -- "${message}" "${work}/failure.log" || {
    echo "Krew release verification rejected ${defect} for another reason:" >&2
    cat "${work}/failure.log" >&2
    exit 1
  }
  if [ -e "${output}" ]; then
    echo "Krew release verification staged a manifest despite ${defect}" >&2
    exit 1
  fi
}

reset_release
verify "${tag}" >/dev/null
cmp "${release}/memlens.yaml" "${output}" || {
  echo 'staged Krew manifest is not the release asset byte for byte' >&2
  exit 1
}
grep -Fq -- "--certificate-identity https://github.com/danushkastanley/KubeMemLens/.github/workflows/promote.yml@refs/tags/${tag} " \
  "${work}/cosign.log" || {
  echo 'promotion record was not verified against the promotion workflow of this tag' >&2
  exit 1
}
if grep -Fv "https://github.com/danushkastanley/KubeMemLens/releases/download/${tag}/" "${work}/curl.log"; then
  echo 'verification downloaded from outside the published release' >&2
  exit 1
fi
test "$(grep -c 'kube-memlens_1.2.3_' "${work}/curl.log")" -eq 6 || {
  echo 'verification did not download all six Krew archives' >&2
  exit 1
}

reset_release
expect_rejected 'a release candidate tag' v1.2.3-rc.1 'need a stable vX.Y.Z release tag'
if [ -e "${work}/curl.log" ]; then
  echo 'a release candidate tag reached the download step' >&2
  exit 1
fi

reset_release
rm "${release}/memlens.yaml"
expect_rejected 'a release without a public memlens.yaml' "${tag}" 'not publicly downloadable'

reset_release
cosign_status=1
expect_rejected 'an unsigned promotion record' "${tag}" 'is not signed by the v1.2.3 promotion workflow'

reset_release
sed -i.bak 's/shortDescription: .*/shortDescription: Replaced after signing/' "${release}/memlens.yaml"
expect_rejected 'a manifest changed after signing' "${tag}" 'differs from the signed promotion record'

reset_release
grep -v ' memlens.yaml$' "${pristine}/promotion-checksums.txt" > "${release}/promotion-checksums.txt"
expect_rejected 'a promotion record that omits memlens.yaml' "${tag}" 'does not cover both'

reset_release
sed -i.bak 's/shortDescription: .*/shortDescription: Signed but not from the template/' "${release}/memlens.yaml"
(cd "${release}" && sha256sum checksums.txt memlens.yaml > promotion-checksums.txt)
expect_rejected 'a signed manifest that the template does not produce' "${tag}" 'differs from deploy/krew/memlens.yaml.tmpl'

reset_release
printf 'replaced\n' > "${release}/kube-memlens_1.2.3_linux_arm64.tar.gz"
expect_rejected 'a public archive that differs from the signed checksums' "${tag}" 'differs from the signed checksums'

echo 'Krew release verification tests passed'
