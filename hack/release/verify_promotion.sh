#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${RELEASE_BUNDLE_DIR:?RELEASE_BUNDLE_DIR is required}
candidate_tag=${RELEASE_CANDIDATE_TAG:?RELEASE_CANDIDATE_TAG is required}
ga_tag=${RELEASE_VERSION:?RELEASE_VERSION is required}
commit=${RELEASE_COMMIT:?RELEASE_COMMIT is required}
candidate_identity="https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/${candidate_tag}"
promotion_identity=${RELEASE_CERTIFICATE_IDENTITY:?RELEASE_CERTIFICATE_IDENTITY is required}
issuer=${RELEASE_CERTIFICATE_ISSUER:-https://token.actions.githubusercontent.com}

"$(dirname "${BASH_SOURCE[0]}")/validate_candidate_manifest.sh" \
  "${bundle}/candidate-manifest.json" "${candidate_tag}" "${ga_tag}" "${commit}" >/dev/null
cosign verify-blob --bundle "${bundle}/candidate-manifest.sigstore.json" \
  --certificate-identity "${candidate_identity}" --certificate-oidc-issuer "${issuer}" \
  "${bundle}/candidate-manifest.json" >/dev/null
cosign verify-blob --bundle "${bundle}/promotion-checksums.sigstore.json" \
  --certificate-identity "${promotion_identity}" --certificate-oidc-issuer "${issuer}" \
  "${bundle}/promotion-checksums.txt" >/dev/null
(
  cd "${bundle}"
  sha256sum --check promotion-checksums.txt
)
for promoted_file in memlens.yaml promotion-checksums.txt promotion-subjects.txt; do
  gh attestation verify "${bundle}/${promoted_file}" \
    --repo "${RELEASE_REPOSITORY:-danushkastanley/KubeMemLens}" >/dev/null
done

export RELEASE_BUNDLE_CERTIFICATE_IDENTITY=${candidate_identity}
export RELEASE_SUBJECTS_FILE=promotion-subjects.txt
"$(dirname "${BASH_SOURCE[0]}")/verify_consumer.sh"
