#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${RELEASE_BUNDLE_DIR:?RELEASE_BUNDLE_DIR is required}
candidate_tag=${RELEASE_CANDIDATE_TAG:?RELEASE_CANDIDATE_TAG is required}
ga_tag=${RELEASE_VERSION:?RELEASE_VERSION is required}
commit=${RELEASE_COMMIT:?RELEASE_COMMIT is required}
identity=${RELEASE_CERTIFICATE_IDENTITY:?RELEASE_CERTIFICATE_IDENTITY is required}
issuer=${RELEASE_CERTIFICATE_ISSUER:-https://token.actions.githubusercontent.com}

"$(dirname "${BASH_SOURCE[0]}")/validate_candidate_manifest.sh" \
  "${bundle}/candidate-manifest.json" "${candidate_tag}" "${ga_tag}" "${commit}" >/dev/null
cosign verify-blob \
  --bundle "${bundle}/candidate-manifest.sigstore.json" \
  --certificate-identity "${identity}" \
  --certificate-oidc-issuer "${issuer}" \
  "${bundle}/candidate-manifest.json" >/dev/null
jq -e \
  --arg image "${RELEASE_IMAGE}" --arg image_digest "${RELEASE_IMAGE_DIGEST}" \
  --arg chart "${RELEASE_CHART}" --arg chart_digest "${RELEASE_CHART_DIGEST}" '
    .image == {repository: $image, digest: $image_digest} and
    .chart.repository == $chart and .chart.digest == $chart_digest
  ' "${bundle}/candidate-manifest.json" >/dev/null

"$(dirname "${BASH_SOURCE[0]}")/verify_consumer.sh"
