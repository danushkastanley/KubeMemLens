#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-release-tag-test.XXXXXX")
remote=${work_dir}/remote.git
checkout=${work_dir}/checkout

cleanup() {
  rm -rf "${work_dir}"
}
trap cleanup EXIT

git init --bare "${remote}" >/dev/null
git init -b main "${checkout}" >/dev/null
git -C "${checkout}" config user.name 'Release Test'
git -C "${checkout}" config user.email 'release-test@example.invalid'
git -C "${checkout}" commit --allow-empty -m baseline >/dev/null
git -C "${checkout}" remote add origin "${remote}"
git -C "${checkout}" push -u origin main >/dev/null
main_sha=$(git -C "${checkout}" rev-parse HEAD)
git -C "${checkout}" tag -a v1.2.3-rc.1 -m v1.2.3-rc.1
(
  cd "${checkout}"
  "${root}/hack/release/validate_tag.sh" v1.2.3-rc.1 "${main_sha}" origin >/dev/null
)

git -C "${checkout}" tag v1.2.4
if (cd "${checkout}" && "${root}/hack/release/validate_tag.sh" v1.2.4 "${main_sha}" origin >/dev/null 2>&1); then
  echo 'lightweight release tag passed validation' >&2
  exit 1
fi

git -C "${checkout}" switch -c side >/dev/null
git -C "${checkout}" commit --allow-empty -m side >/dev/null
side_sha=$(git -C "${checkout}" rev-parse HEAD)
git -C "${checkout}" tag -a v1.2.5 -m v1.2.5
if (cd "${checkout}" && "${root}/hack/release/validate_tag.sh" v1.2.5 "${side_sha}" origin >/dev/null 2>&1); then
  echo 'release tag outside main passed validation' >&2
  exit 1
fi

if (cd "${checkout}" && "${root}/hack/release/validate_tag.sh" invalid "${side_sha}" origin >/dev/null 2>&1); then
  echo 'invalid release version passed validation' >&2
  exit 1
fi

echo 'release tag validation tests passed'
