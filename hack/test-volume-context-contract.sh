#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-volume-contract.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT
render() {
  helm template kube-memlens charts/kube-memlens "$@" |
    sed -E 's/^([[:space:]]+(ca.crt|tls.crt|tls.key|caBundle):).*/\1 "<generated>"/'
}
render > "${work_dir}/default.yaml"
render --set volumeContext.enabled=false --set nodeContext.volumeStats=false > "${work_dir}/disabled.yaml"
cmp "${work_dir}/default.yaml" "${work_dir}/disabled.yaml"
for args in 'nodeContext.volumeStats=true' 'volumeContext.enabled=true' 'volumeContext.namespaces[0]=INVALID' 'volumeContext.health=true' 'volumeContext.workloads=true'; do
  if render --set "${args}" > "${work_dir}/invalid.yaml" 2> "${work_dir}/error.txt"; then
    echo 'invalid volume profile rendered' >&2; exit 1
  fi
done
render --set nodeContext.enabled=true --set nodeContext.volumeStats=true \
  --set volumeContext.enabled=true --set 'volumeContext.namespaces[0]=team-a' --set 'volumeContext.namespaces[1]=team-b' \
  --set nodeContext.kubeletCAConfigMap=qualified-ca --set nodeContext.kubeletAudience=qualified-audience \
  --set 'nodeContext.apiServerCIDRs[0]=10.96.0.1/32' --set 'nodeContext.nodeCIDRs[0]=172.18.0.0/16' > "${work_dir}/enabled.yaml"
render --set nodeContext.enabled=true --set nodeContext.volumeStats=true \
  --set volumeContext.enabled=true --set volumeContext.health=true --set 'volumeContext.namespaces[0]=team-a' --set 'volumeContext.namespaces[1]=team-b' \
  --set nodeContext.kubeletCAConfigMap=qualified-ca --set nodeContext.kubeletAudience=qualified-audience \
  --set 'nodeContext.apiServerCIDRs[0]=10.96.0.1/32' --set 'nodeContext.nodeCIDRs[0]=172.18.0.0/16' > "${work_dir}/health.yaml"
render --set nodeContext.enabled=true --set nodeContext.volumeStats=true \
  --set volumeContext.enabled=true --set volumeContext.workloads=true --set 'volumeContext.namespaces[0]=team-a' --set 'volumeContext.namespaces[1]=team-b' \
  --set nodeContext.kubeletCAConfigMap=qualified-ca --set nodeContext.kubeletAudience=qualified-audience \
  --set 'nodeContext.apiServerCIDRs[0]=10.96.0.1/32' --set 'nodeContext.nodeCIDRs[0]=172.18.0.0/16' > "${work_dir}/workloads.yaml"
go run ./hack/volume-context-contract "${work_dir}/default.yaml" "${work_dir}/enabled.yaml" "${work_dir}/health.yaml" "${work_dir}/workloads.yaml"
