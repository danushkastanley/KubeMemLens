#!/usr/bin/env bash
set -Eeuo pipefail

image=${SKOPEO_IMAGE:?SKOPEO_IMAGE is required}

# Run without mounting registry credentials so tool failures cannot look like
# a missing destination manifest during publication preflight.
if ! version=$(docker run --rm "${image}" --version); then
  echo 'pinned Skopeo image is unavailable or cannot start' >&2
  exit 1
fi
if [[ ! "${version}" =~ ^skopeo\ version\ 1\.22\.2([[:space:]]|$) ]]; then
  echo "pinned Skopeo image reported an unexpected version: ${version}" >&2
  exit 1
fi
printf '%s\n' "${version}"
