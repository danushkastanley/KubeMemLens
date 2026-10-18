#!/usr/bin/env bash
set -Eeuo pipefail
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-topology-contract.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT
render() {
  helm template kube-memlens charts/kube-memlens --set nodeContext.enabled=true \
    --set nodeContext.kubeletCAConfigMap=qualified-ca --set nodeContext.kubeletAudience=qualified-audience \
    --set 'nodeContext.apiServerCIDRs[0]=10.96.0.1/32' --set 'nodeContext.nodeCIDRs[0]=172.18.0.0/16' "$@" |
    sed -E 's/^([[:space:]]+(ca.crt|tls.crt|tls.key|caBundle):).*/\1 "<generated>"/'
}
render > "${work_dir}/ordinary.yaml"
render --set nodeContext.topology.enabled=true > "${work_dir}/topology.yaml"
go run ./hack/topology-contract "${work_dir}/ordinary.yaml" "${work_dir}/topology.yaml"
if helm template kube-memlens charts/kube-memlens --set nodeContext.topology.enabled=true > /dev/null 2> "${work_dir}/error.txt"; then
  echo 'topology without Node-context profile rendered' >&2; exit 1
fi
if render --set nodeContext.topology.enabled=true --set nodeContext.topology.cgroupRoot=/sys/fs/cgroup/../.. > /dev/null 2> "${work_dir}/error.txt"; then
  echo 'unqualified cgroup root rendered' >&2; exit 1
fi
echo 'topology profile contract passed; no resources installed'
