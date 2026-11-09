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

# Candidate install paths live only in these notes. The archives and chart ship
# README files whose bytes stable promotion preserves, so those guides must
# describe the stable release alone.
image_repository=$(jq -er .image.repository "${manifest}")
image_digest=$(jq -er .image.digest "${manifest}")
chart_repository=$(jq -er .chart.repository "${manifest}")
chart_digest=$(jq -er .chart.digest "${manifest}")
notes=$(cat <<NOTES
Prospective ${ga_tag} artefacts. Do not promote until every PROD-012 gate and the recorded go/no-go review pass.

Stable promotion copies these exact archive, image and chart bytes to ${ga_tag}; nothing is rebuilt.

Install this candidate for qualification only, pinning the image digest from the signed \`candidate-manifest.json\`:

\`\`\`sh
helm upgrade --install kube-memlens \\
  oci://${chart_repository} \\
  --version ${ga_version} \\
  --namespace kube-memlens \\
  --create-namespace \\
  --set image.repository=${image_repository} \\
  --set image.digest=${image_digest}
\`\`\`

Chart digest: \`${chart_digest}\`. Report results on [release evidence issue #50](https://github.com/danushkastanley/KubeMemLens/issues/50).
NOTES
)

gh release edit "${candidate_tag}" --draft --prerelease \
  --title "${candidate_tag} candidate for ${ga_tag}" \
  --notes "${notes}"
