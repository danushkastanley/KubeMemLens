#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-history-contract.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT
render() {
  helm template kube-memlens charts/kube-memlens "$@" |
    sed -E 's/^([[:space:]]+(ca.crt|tls.crt|tls.key|caBundle):).*/\1 "<generated>"/'
}
render > "${work_dir}/default.yaml"
render --set memoryHistory.enabled=false > "${work_dir}/disabled.yaml"
cmp "${work_dir}/default.yaml" "${work_dir}/disabled.yaml"
for value in 'memoryHistory.enabled=true' 'memoryHistory.nodes=true' 'memoryHistory.workloads=true' 'memoryHistory.markers=true' 'memoryHistory.url=http://history.example' 'memoryHistory.tokenKey=token' 'memoryHistory.namespaces[0]=INVALID'; do
  if render --set "${value}" > "${work_dir}/invalid.yaml" 2> "${work_dir}/error.txt"; then
    echo 'invalid history profile rendered' >&2; exit 1
  fi
done
common=(--set memoryHistory.enabled=true --set memoryHistory.url=https://history.example --set memoryHistory.cluster=cluster-a
  --set 'memoryHistory.namespaces[0]=team-a' --set 'memoryHistory.namespaces[1]=team-b'
  --set memoryHistory.credentialsSecret=history-access --set memoryHistory.caKey=ca.crt --set memoryHistory.tokenKey=token)
render "${common[@]}" > "${work_dir}/namespaces.yaml"
render "${common[@]}" --set memoryHistory.nodes=true --set memoryHistory.workloads=true > "${work_dir}/full.yaml"
if render "${common[@]}" --set collector.enabled=false > "${work_dir}/invalid.yaml" 2> "${work_dir}/error.txt"; then
  echo 'history rendered without its authenticated collector' >&2; exit 1
fi
render "${common[@]}" --set memoryHistory.markers=true > "${work_dir}/markers.yaml"
render "${common[@]}" --set memoryHistory.markers=true --set memoryHistory.workloads=true --set memoryHistory.nodes=true > "${work_dir}/markers-full.yaml"
go run ./hack/memory-history-contract "${work_dir}/default.yaml" "${work_dir}/namespaces.yaml" "${work_dir}/full.yaml" "${work_dir}/markers.yaml" "${work_dir}/markers-full.yaml"
