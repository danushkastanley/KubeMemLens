#!/usr/bin/env bash

set -Eeuo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
cd "${repo_root}"

# shellcheck source=hack/lib/provider-qualification-cleanup.sh
source "${repo_root}/hack/lib/provider-qualification-cleanup.sh"

test_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-cleanup-test.XXXXXX")
trap 'rm -rf -- "${test_dir}"' EXIT
namespace=kube-memlens-qualification-cleanup-test
namespace_uid=12345678-1234-1234-1234-123456789abc
request_body=${test_dir}/delete-options.json

k() {
  [ "$1" = delete ]
  [ "$2" = --raw ]
  [ "$3" = "/api/v1/namespaces/${namespace}" ]
  [ "$4" = -f ]
  [ "$5" = - ]
  cat > "${request_body}"
}

delete_owned_namespace
jq -e --arg uid "${namespace_uid}" '
  . == {apiVersion:"v1",kind:"DeleteOptions",preconditions:{uid:$uid},
        propagationPolicy:"Background"}
' "${request_body}" >/dev/null

namespace_uid=
if delete_owned_namespace; then
  echo "namespace cleanup accepted an empty UID" >&2
  exit 1
fi

echo "provider cleanup checks passed"
