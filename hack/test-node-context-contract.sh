#!/usr/bin/env bash
set -Eeuo pipefail

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-node-contract.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT

helm lint charts/kube-memlens
# Helm generates a fresh serving key/certificate on every offline render.
# Compare every other field without retaining unused private key material.
render() {
  helm template kube-memlens charts/kube-memlens "$@" |
    sed -E 's/^([[:space:]]+(ca.crt|tls.crt|tls.key|caBundle):).*/\1 "<generated>"/'
}
render > "${work_dir}/default.yaml"
render --set nodeContext.enabled=false > "${work_dir}/disabled.yaml"
cmp "${work_dir}/default.yaml" "${work_dir}/disabled.yaml"

if helm template kube-memlens charts/kube-memlens --set nodeContext.enabled=true > "${work_dir}/enabled.yaml" 2> "${work_dir}/error.txt"; then
  echo 'node-context profile without qualified configuration must not render' >&2
  exit 1
fi
grep -q 'nodeContext' "${work_dir}/error.txt"
if grep -Eq 'nodes/(stats|metrics|proxy)|name: kube-memlens-node-context' "${work_dir}/default.yaml"; then
  echo 'standard chart unexpectedly grants node-context permissions' >&2
  exit 1
fi
render --set nodeContext.enabled=true --set nodeContext.kubeletCAConfigMap=qualified-ca \
  --set nodeContext.kubeletAudience=qualified-audience \
  --set 'nodeContext.apiServerCIDRs[0]=10.96.0.1/32' \
  --set 'nodeContext.nodeCIDRs[0]=172.18.0.0/16' > "${work_dir}/enabled.yaml"
go run ./hack/node-context-contract "${work_dir}/default.yaml" "${work_dir}/enabled.yaml" docs/security/node-context-rbac.json
go test -v ./internal/nodecontext
echo 'node-context contract passed; no optional profile installed'
