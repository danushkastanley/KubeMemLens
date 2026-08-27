#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${RELEASE_BUNDLE_DIR:?RELEASE_BUNDLE_DIR is required}
candidate_tag=${RELEASE_CANDIDATE_TAG:?RELEASE_CANDIDATE_TAG is required}
manifest=${bundle}/candidate-manifest.json
ga_tag=$(jq -r .intended_ga_tag "${manifest}")
ga_version=${ga_tag#v}
incomplete_title="INCOMPLETE qualification: ${candidate_tag}"

set +e
"$(dirname "${BASH_SOURCE[0]}")/resume_draft.sh" "${candidate_tag}" "${bundle}" true
resume_status=$?
set -e
case "${resume_status}" in
  0) ;;
  3)
    gh release create "${candidate_tag}" \
      "${bundle}"/kube-memlens_*.tar.gz \
      "${bundle}"/kube-memlens_*.zip \
      "${bundle}"/*.sbom.json \
      "${bundle}"/checksums.txt \
      "${bundle}"/checksums.txt.sigstore.json \
      "${bundle}/kube-memlens-${ga_version}.tgz" \
      "${bundle}"/candidate-manifest.json \
      "${bundle}"/candidate-manifest.sigstore.json \
      "${bundle}"/release-subjects.txt \
      --draft --prerelease --verify-tag --title "${incomplete_title}" \
      --notes "Prospective ${ga_tag} artefacts. Do not promote until every PROD-012 gate and the recorded go/no-go review pass."
    ;;
  *) exit "${resume_status}" ;;
esac

gh release edit "${candidate_tag}" --draft --prerelease \
  --title "${candidate_tag} candidate for ${ga_tag}"
