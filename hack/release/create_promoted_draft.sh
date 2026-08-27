#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${RELEASE_BUNDLE_DIR:?RELEASE_BUNDLE_DIR is required}
version=${RELEASE_VERSION:?RELEASE_VERSION is required}
chart_version=${version#v}
incomplete_title="INCOMPLETE promotion: ${version}"

set +e
"$(dirname "${BASH_SOURCE[0]}")/resume_draft.sh" "${version}" "${bundle}" false
resume_status=$?
set -e
case "${resume_status}" in
  0) ;;
  3)
    gh release create "${version}" \
      "${bundle}"/kube-memlens_*.tar.gz \
      "${bundle}"/kube-memlens_*.zip \
      "${bundle}"/*.sbom.json \
      "${bundle}"/checksums.txt \
      "${bundle}"/checksums.txt.sigstore.json \
      "${bundle}/kube-memlens-${chart_version}.tgz" \
      "${bundle}"/candidate-manifest.json \
      "${bundle}"/candidate-manifest.sigstore.json \
      "${bundle}"/release-subjects.txt \
      "${bundle}"/memlens.yaml \
      "${bundle}"/promotion-checksums.txt \
      "${bundle}"/promotion-checksums.sigstore.json \
      "${bundle}"/promotion-subjects.txt \
      --draft --verify-tag --title "${incomplete_title}" \
      --notes 'Promoted from the reviewed candidate without rebuilding product artefacts. Verify both candidate and promotion signatures before installation.'
    ;;
  *) exit "${resume_status}" ;;
esac

gh release edit "${version}" --draft --title "${version}"
