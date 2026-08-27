#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-publish-resume.XXXXXX")
bundle=${work}/bundle
fake_bin=${work}/bin
candidate=v1.2.3-rc.4
ga=v1.2.3
commit=0123456789abcdef0123456789abcdef01234567
image_raw='exact-image-manifest'
image_digest=sha256:$(printf '%s' "${image_raw}" | sha256sum | awk '{print $1}')
chart_digest=sha256:$(printf 'c%.0s' {1..64})

cleanup() { rm -rf "${work}"; }
trap cleanup EXIT
mkdir -p "${bundle}/internal" "${fake_bin}"
for name in \
  kube-memlens_1.2.3_darwin_amd64.tar.gz kube-memlens_1.2.3_darwin_arm64.tar.gz \
  kube-memlens_1.2.3_linux_amd64.tar.gz kube-memlens_1.2.3_linux_arm64.tar.gz \
  kube-memlens_1.2.3_windows_amd64.zip kube-memlens_1.2.3_windows_arm64.zip; do
  printf '%s\n' "${name}" > "${bundle}/${name}"
done
printf 'chart\n' > "${bundle}/kube-memlens-1.2.3.tgz"
printf 'image archive\n' > "${bundle}/internal/kube-memlens-image.tar"
printf '%s\n' "${image_digest}" > "${bundle}/internal/image-digest.txt"
(
  cd "${bundle}"
  sha256sum kube-memlens_* kube-memlens-1.2.3.tgz > checksums.txt
  sha256sum internal/kube-memlens-image.tar internal/image-digest.txt checksums.txt \
    > internal/transfer-checksums.txt
)
cp "${bundle}/checksums.txt" "${work}/checksums.baseline"
cp "${bundle}/internal/transfer-checksums.txt" "${work}/transfer-checksums.baseline"

cat > "${fake_bin}/docker" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
args=" $* "
if [[ "${args}" == *' copy '* ]]; then touch "${FAKE_STATE}/image-published"; exit 0; fi
if [[ "${args}" == *' inspect '* ]]; then
  state=${FAKE_IMAGE_STATE}
  [ -e "${FAKE_STATE}/image-published" ] && state=same
  if [ "${state}" = absent ]; then echo 'manifest unknown' >&2; exit 1; fi
  if [[ "${args}" == *' --raw '* ]]; then
    [ "${state}" = same ] && printf '%s' "${FAKE_IMAGE_RAW}" || printf 'different-image-manifest'
  else
    printf '{}\n'
  fi
  exit 0
fi
exit 0
SH
cat > "${fake_bin}/oras" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
state=${FAKE_CHART_STATE}
[ -e "${FAKE_STATE}/chart-published" ] && state=same
if [ "${state}" = absent ]; then echo 'manifest unknown' >&2; exit 1; fi
printf '{"digest":"%s"}\n' "${FAKE_CHART_DIGEST}"
SH
cat > "${fake_bin}/helm" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
case "${1:-}" in
  registry) exit 0 ;;
  pull)
    destination=
    while [ "$#" -gt 0 ]; do
      [ "$1" != --destination ] || { destination=$2; break; }
      shift
    done
    mkdir -p "${destination}"
    if [ "${FAKE_CHART_STATE}" = different ]; then
      printf 'different\n' > "${destination}/kube-memlens-1.2.3.tgz"
    else
      cp "${FAKE_BUNDLE}/kube-memlens-1.2.3.tgz" "${destination}/"
    fi
    ;;
  push)
    touch "${FAKE_STATE}/chart-published"
    printf 'Digest: %s\n' "${FAKE_CHART_DIGEST}"
    ;;
esac
SH
cat > "${fake_bin}/cosign" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "${fake_bin}"/*

run_publish() {
  local image_state=$1 chart_state=$2 state
  state=${work}/state-${image_state}-${chart_state}
  rm -rf "${state}"; mkdir -p "${state}"
  cp "${work}/checksums.baseline" "${bundle}/checksums.txt"
  cp "${work}/transfer-checksums.baseline" "${bundle}/internal/transfer-checksums.txt"
  rm -f "${bundle}/candidate-manifest.json" "${bundle}/candidate-manifest.sigstore.json" \
    "${bundle}/checksums.txt.sigstore.json" "${bundle}/release-subjects.txt"
  PATH="${fake_bin}:${PATH}" FAKE_STATE="${state}" FAKE_IMAGE_STATE="${image_state}" \
    FAKE_CHART_STATE="${chart_state}" FAKE_IMAGE_RAW="${image_raw}" \
    FAKE_CHART_DIGEST="${chart_digest}" FAKE_BUNDLE="${bundle}" \
    CANDIDATE_TAG="${candidate}" GA_TAG="${ga}" GA_VERSION=1.2.3 \
    GITHUB_SHA="${commit}" GITHUB_ACTOR=test GITHUB_OUTPUT="${state}/output" \
    GH_TOKEN=test RUNNER_TEMP="${state}" SKOPEO_IMAGE=test-skopeo \
    CANDIDATE_IMAGE_REPOSITORY=ghcr.io/danushkastanley/candidates/1.2.3-rc.4/kube-memlens \
    CANDIDATE_CHART_REPOSITORY=ghcr.io/danushkastanley/candidates/1.2.3-rc.4/charts/kube-memlens \
    "${root}/hack/release/publish_candidate.sh" "${bundle}"
}

run_publish same same >/dev/null
run_publish absent absent >/dev/null
if run_publish different same >/dev/null 2>&1; then
  echo 'candidate publication accepted an occupied image with a different digest' >&2; exit 1
fi
if run_publish same different >/dev/null 2>&1; then
  echo 'candidate publication accepted an occupied chart with different bytes' >&2; exit 1
fi

echo 'candidate publication resume tests passed'
