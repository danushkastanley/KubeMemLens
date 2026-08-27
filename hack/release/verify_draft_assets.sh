#!/usr/bin/env bash
set -Eeuo pipefail

expected=${1:?usage: verify_draft_assets.sh EXPECTED DOWNLOADED}
downloaded=${2:?usage: verify_draft_assets.sh EXPECTED DOWNLOADED}

for directory in "${expected}" "${downloaded}"; do
  [ -d "${directory}" ] || {
    echo "draft asset directory is missing: ${directory}" >&2
    exit 1
  }
  if find "${directory}" -mindepth 1 ! -type f -print -quit | grep -q .; then
    echo "draft assets must be regular files: ${directory}" >&2
    exit 1
  fi
done

expected_list=$(find "${expected}" -maxdepth 1 -type f -exec basename '{}' \; | LC_ALL=C sort)
downloaded_list=$(find "${downloaded}" -maxdepth 1 -type f -exec basename '{}' \; | LC_ALL=C sort)
if [ "${expected_list}" != "${downloaded_list}" ]; then
  echo 'draft asset inventory differs from the verified bundle' >&2
  diff -u <(printf '%s\n' "${expected_list}") <(printf '%s\n' "${downloaded_list}") >&2 || true
  exit 1
fi

while IFS= read -r name; do
  cmp "${expected}/${name}" "${downloaded}/${name}" || {
    echo "draft asset differs from the verified bundle: ${name}" >&2
    exit 1
  }
done <<< "${expected_list}"

echo 'draft assets match the verified bundle'
