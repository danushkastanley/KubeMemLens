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
work_dir=${test_dir}
test "${required_kube_system_rolebinding_verbs[*]}" = 'create get delete patch'

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

owned_resources=(rolebinding/kube-system/kube-memlens-extension-authentication-reader)
release=kube-memlens-qualification
namespace=kube-memlens-qualification-cleanup-test
mock_mode=owned

k() {
  if [ "$1" = get ]; then
    [ "$2" = rolebinding/kube-memlens-extension-authentication-reader ]
    [ "$3" = --namespace ]
    [ "$4" = kube-system ]
    [ "$5" = -o ]
    [ "$6" = json ]
    case "${mock_mode}" in
      absent)
        echo 'Error from server (NotFound): rolebindings.rbac.authorization.k8s.io not found' >&2
        return 1
        ;;
      forbidden)
        echo 'Error from server (Forbidden): rolebindings.rbac.authorization.k8s.io is forbidden' >&2
        return 1
        ;;
      foreign)
        jq -n --arg uid 87654321-4321-4321-4321-cba987654321 '
          {metadata:{uid:$uid,annotations:{
            "meta.helm.sh/release-name":"another-release",
            "meta.helm.sh/release-namespace":"another-namespace"}}}'
        ;;
      missing-uid)
        jq -n --arg release "${release}" --arg namespace "${namespace}" '
          {metadata:{annotations:{
            "meta.helm.sh/release-name":$release,
            "meta.helm.sh/release-namespace":$namespace}}}'
        ;;
      owned)
        jq -n --arg uid 87654321-4321-4321-4321-cba987654321 \
          --arg release "${release}" --arg namespace "${namespace}" '
          {metadata:{uid:$uid,annotations:{
            "meta.helm.sh/release-name":$release,
            "meta.helm.sh/release-namespace":$namespace}}}'
        ;;
      *) return 1 ;;
    esac
    return
  fi
  [ "$1" = delete ]
  [ "$2" = --raw ]
  printf '%s' "$3" > "${test_dir}/${mock_mode}-delete-uri"
  [ "$4" = -f ]
  [ "$5" = - ]
  cat > "${test_dir}/${mock_mode}-delete-options.json"
}

delete_owned_resources
test "$(cat "${test_dir}/owned-delete-uri")" = \
  /apis/rbac.authorization.k8s.io/v1/namespaces/kube-system/rolebindings/kube-memlens-extension-authentication-reader
jq -e '
  . == {apiVersion:"v1",kind:"DeleteOptions",
        preconditions:{uid:"87654321-4321-4321-4321-cba987654321"},
        propagationPolicy:"Background"}
' "${test_dir}/owned-delete-options.json" >/dev/null

mock_mode=absent
owned_resources_removed

for mock_mode in foreign missing-uid forbidden; do
  if delete_owned_resources; then
    echo "owned resource cleanup accepted ${mock_mode} metadata" >&2
    exit 1
  fi
  test ! -e "${test_dir}/${mock_mode}-delete-uri"
done

mock_mode=owned
if owned_resources_removed 2>/dev/null; then
  echo "owned resource absence check accepted a remaining RoleBinding" >&2
  exit 1
fi

echo "provider cleanup checks passed"
