#!/usr/bin/env bash
# Disposable local CSI qualification. No existing cluster or product RBAC changes.
set -Eeuo pipefail
[ "${CSI_HEALTH_ACKNOWLEDGE:-}" = create-and-remove-local-csi-clusters ] || { echo 'set CSI_HEALTH_ACKNOWLEDGE=create-and-remove-local-csi-clusters' >&2; exit 1; }
artifact_dir=${CSI_HEALTH_ARTIFACT_DIR:?CSI_HEALTH_ARTIFACT_DIR is required}
cluster_prefix=${CSI_HEALTH_CLUSTER_PREFIX:-kube-memlens-volume-health}
[[ "${cluster_prefix}" =~ ^kube-memlens-[a-z0-9-]+$ ]] || { echo 'use a kube-memlens- cluster prefix' >&2; exit 1; }
profiles=(off alpha)
case "${CSI_HEALTH_PROFILE:-both}" in both) ;; off|alpha) profiles=("${CSI_HEALTH_PROFILE}");; *) echo 'unknown CSI health profile' >&2; exit 1;; esac
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-volume-health.XXXXXX")
owned_cluster=
cleanup() {
  local result=$?
  if [ -n "${owned_cluster}" ]; then kind delete cluster --name "${owned_cluster}" >/dev/null 2>&1 || result=1; fi
  rm -rf "${work_dir}"
  exit "${result}"
}
trap cleanup EXIT
trap 'echo "CSI health verification failed at line ${LINENO}" >&2' ERR
mkdir -p "${artifact_dir}"
upstream_commit=eccd681b18a2c96332f33c2cac5db38656edd500
curl --fail --silent --show-error --location --max-time 120 --max-filesize 10485760 \
  "https://codeload.github.com/kubernetes-csi/csi-driver-host-path/tar.gz/${upstream_commit}" -o "${work_dir}/hostpath.tar.gz"
archive_sha=$(shasum -a 256 "${work_dir}/hostpath.tar.gz" | awk '{print $1}')
[ "${archive_sha}" = 3b9e40a1f49435d1b264a28bc821580647d51fdec69d514e61bb70ec83f72bd8 ] || { echo 'upstream source checksum mismatch' >&2; exit 1; }
tar -xzf "${work_dir}/hostpath.tar.gz" -C "${work_dir}"
docker build -f hack/fixtures/csi-health/Dockerfile -t kube-memlens-csi-hostpath:eccd681-versioned "${work_dir}/csi-driver-host-path-${upstream_commit}"
go build -o "${work_dir}/probe" ./hack/fixtures/volume-health-probe
kubeconfig="${work_dir}/kubeconfig"
kctl() { kubectl --kubeconfig "${kubeconfig}" --context "kind-${owned_cluster}" "$@"; }
probe() {
  "${work_dir}/probe" --kubeconfig "${kubeconfig}" --token-file "${work_dir}/caller.token" "$@"
}
mark() { kctl exec driver -n kube-memlens-csi-e2e -c driver -- /healthctl "$@" >/dev/null; }
for profile in "${profiles[@]}"; do
  cluster_name="${cluster_prefix}-${profile}"
  if kind get clusters | grep -Fxq "${cluster_name}"; then echo 'refusing to replace an existing cluster' >&2; exit 1; fi
  gate=false
  [ "${profile}" != alpha ] || gate=true
  cat > "${work_dir}/kind.yaml" <<YAML
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
featureGates:
  CSIVolumeHealth: ${gate}
kubeadmConfigPatches:
  - |
    apiVersion: kubelet.config.k8s.io/v1beta1
    kind: KubeletConfiguration
    volumeStatsAggPeriod: 5s
YAML
  owned_cluster=${cluster_name}
  kind create cluster --name "${owned_cluster}" --config "${work_dir}/kind.yaml" --kubeconfig "${kubeconfig}" \
    --image kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5 --wait 120s
  kind load docker-image kube-memlens-csi-hostpath:eccd681-versioned --name "${owned_cluster}"
  kctl apply -f hack/fixtures/csi-health/driver.yaml >/dev/null
  kctl wait pod/driver -n kube-memlens-csi-e2e --for=condition=Ready --timeout=120s >/dev/null
  node="${owned_cluster}-control-plane"
  registered=false
  for _ in $(seq 1 30); do
    if kctl get csinode "${node}" -o json | jq -e 'any(.spec.drivers[]?; .name == "hostpath.csi.k8s.io")' >/dev/null; then registered=true; break; fi
    sleep 2
  done
  [ "${registered}" = true ] || { echo 'CSI driver did not register' >&2; exit 1; }
  kctl apply -f hack/fixtures/csi-health/seed.yaml >/dev/null
  kctl wait pod/workload -n kube-memlens-csi-e2e --for=condition=Ready --timeout=120s >/dev/null
  handle=$(kctl exec driver -n kube-memlens-csi-e2e -c driver -- cat /csi-data-dir/state.json | jq -er '.Volumes | select(length == 1) | .[0].VolID')
  [[ "${handle}" =~ ^csi-[a-f0-9]+$ ]] || { echo 'invalid fixture volume handle' >&2; exit 1; }
  sed "s/VOLUME_HANDLE/${handle}/g" hack/fixtures/csi-health/persistent.yaml > "${work_dir}/persistent.yaml"
  kctl apply -f "${work_dir}/persistent.yaml" >/dev/null
  kctl wait pod/persistent -n kube-memlens-csi-e2e --for=condition=Ready --timeout=120s >/dev/null
  kctl create namespace kube-memlens-csi-other >/dev/null
  sed 's/kube-memlens-csi-e2e/kube-memlens-csi-other/g' "${work_dir}/persistent.yaml" | kctl apply -f - >/dev/null
  kctl apply -f - >/dev/null <<YAML
apiVersion: v1
kind: ServiceAccount
metadata:
  name: health-reader
  namespace: kube-memlens-csi-e2e
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: health-reader
  namespace: kube-memlens-csi-e2e
rules:
  - apiGroups: ['']
    resources: [pods, persistentvolumeclaims]
    verbs: [get]
    resourceNames: [persistent, data]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: health-reader
  namespace: kube-memlens-csi-e2e
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: health-reader}
subjects:
  - {kind: ServiceAccount, name: health-reader, namespace: kube-memlens-csi-e2e}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-memlens-csi-volume-reader
rules:
  - apiGroups: ['']
    resources: [persistentvolumes]
    verbs: [get]
    resourceNames: [kube-memlens-csi-test]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-memlens-csi-volume-reader
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kube-memlens-csi-volume-reader}
subjects:
  - {kind: ServiceAccount, name: health-reader, namespace: kube-memlens-csi-e2e}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kube-memlens-csi-node-reader
rules:
  - apiGroups: [storage.k8s.io]
    resources: [csinodes]
    verbs: [get]
    resourceNames: [${node}]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-memlens-csi-node-reader
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kube-memlens-csi-node-reader}
subjects:
  - {kind: ServiceAccount, name: health-reader, namespace: kube-memlens-csi-e2e}
YAML
  (umask 077; kctl create token health-reader -n kube-memlens-csi-e2e --duration=10m > "${work_dir}/caller.token")
  probe --namespace kube-memlens-csi-other --expect denied > "${artifact_dir}/${profile}-namespace-denied.json"
  probe --namespace kube-memlens-csi-other --pod absent --expect denied > "${artifact_dir}/${profile}-missing-denied.json"
  mark mark-volume-unhealthy "${handle}" --scope node --status DEGRADED --reason LocalTest --message LocalTest
  mark mark-storage-unhealthy --status STORAGE_DEGRADED --reason LocalTest --message LocalTest
  kctl patch pvc data -n kube-memlens-csi-e2e --subresource=status --type=merge -p '{"status":{"healthStatus":{"healthConditions":[]}}}' >/dev/null
  if [ "${profile}" = off ]; then
    probe --expect off > "${artifact_dir}/off-unreported.json"
  else
    probe --expect adverse > "${artifact_dir}/alpha-adverse.json"
    mark mark-volume-healthy "${handle}" --scope node
    mark mark-storage-healthy
    kctl patch pvc data -n kube-memlens-csi-e2e --subresource=status --type=merge -p '{"status":{"healthStatus":{"healthConditions":[{"status":"DataLoss","reason":"LocalControllerTest","message":"Local controller fixture"}]}}}' >/dev/null
    probe --expect recovered > "${artifact_dir}/alpha-recovered.json"
    kctl delete clusterrolebinding kube-memlens-csi-node-reader >/dev/null
    probe --expect node-denied > "${artifact_dir}/alpha-node-denied.json"
  fi
  kind delete cluster --name "${owned_cluster}"
  owned_cluster=
done
source_dirty=false
[ -z "$(git status --porcelain)" ] || source_dirty=true
jq -n --arg commit "$(git rev-parse HEAD)" --arg upstreamCommit "${upstream_commit}" --argjson sourceDirty "${source_dirty}" \
  --arg profiles "${profiles[*]}" '{outcome:"passed",sourceCommit:$commit,sourceDirty:$sourceDirty,upstreamDriverCommit:$upstreamCommit,profiles:$profiles,controllerStatusSource:"controlled Kubernetes API fixture",nodeStatusSource:"upstream CSI driver via kubelet",credentialsRetained:false}' > "${artifact_dir}/summary.json"
echo 'local CSI volume-health verification passed'
