#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${RELEASE_BUNDLE_DIR:?RELEASE_BUNDLE_DIR is required}
version=${RELEASE_VERSION:?RELEASE_VERSION is required}
chart_version=${version#v}
incomplete_title="INCOMPLETE publication: ${version}"

release_flags=(--draft --verify-tag --title "${incomplete_title}" --generate-notes)
if [[ "${version}" == *-* ]]; then
  release_flags+=(--prerelease)
fi

gh release create "${version}" \
  "${bundle}"/kube-memlens_*.tar.gz \
  "${bundle}"/kube-memlens_*.zip \
  "${bundle}"/*.sbom.json \
  "${bundle}"/checksums.txt \
  "${bundle}"/checksums.txt.sigstore.json \
  "${bundle}/kube-memlens-${chart_version}.tgz" \
  "${bundle}"/memlens.yaml \
  "${bundle}"/release-subjects.txt \
  "${release_flags[@]}" \
  --notes 'Verify checksums, signatures, attestations and exact OCI digests before installation.'

gh release edit "${version}" --draft --title "${version}"
