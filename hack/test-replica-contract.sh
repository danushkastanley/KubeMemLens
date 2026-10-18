#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
replica_work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-replica-contract.XXXXXX")
trap 'rm -rf -- "${replica_work_dir}"' EXIT
render() {
  helm template kube-memlens charts/kube-memlens "$@" |
    sed -E 's/^([[:space:]]+(ca.crt|tls.crt|tls.key|caBundle):).*/\1 "<generated>"/'
}
render > "${replica_work_dir}/default.yaml"
render --set replicaBaselines.enabled=false > "${replica_work_dir}/disabled.yaml"
cmp "${replica_work_dir}/default.yaml" "${replica_work_dir}/disabled.yaml"
for value in 'replicaBaselines.enabled=true' 'replicaBaselines.namespaces[0]=team-a' 'replicaBaselines.enabled=true,replicaBaselines.namespaces[0]=INVALID' 'replicaBaselines.enabled=true,replicaBaselines.namespaces[0]=team-a,replicaBaselines.namespaces[1]=team-a' 'replicaBaselines.enabled=true,replicaBaselines.namespaces[0]=team-a,collector.enabled=false'; do
  if render --set "$value" > "${replica_work_dir}/invalid.yaml" 2> "${replica_work_dir}/error.txt"; then
    echo 'invalid replica profile rendered' >&2; exit 1
  fi
done
render --set replicaBaselines.enabled=true --set 'replicaBaselines.namespaces[0]=team-a' --set 'replicaBaselines.namespaces[1]=team-b' > "${replica_work_dir}/enabled.yaml"
go run ./hack/replica-contract "${replica_work_dir}/default.yaml" "${replica_work_dir}/enabled.yaml"
