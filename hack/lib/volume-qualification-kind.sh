#!/usr/bin/env bash
# Sourced only by the owned CSI fixture, after the functional operator checks.
# shellcheck disable=SC2154
volume_qualification_measurements() {
  local profile=${VOLUME_QUALIFICATION_PROFILE:?}
  kctl apply -f hack/fixtures/volume-stats/workload.yaml >/dev/null
  kctl rollout status deployment/volume-workload -n kube-memlens-csi-e2e --timeout=120s >/dev/null
  helm "${helm_args[@]}" "${volume_args[@]}" --set nodeContext.volumeStats=false --set volumeContext.health=false --set volumeContext.workloads=true > "${work_dir}/volume-qualification-baseline-helm.log" 2>&1
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"].get("nodeContext",{}).get("freshRecords",0)==1 and d["store"]["totalContainers"]>0' volume-baseline-ready.json
  kctl create rolebinding qualification-workload -n kube-memlens-csi-e2e --clusterrole=kube-memlens-workload-volume-viewer --serviceaccount=kube-memlens-csi-e2e:volume-viewer >/dev/null
  kctl apply -f - >/dev/null <<YAML
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: volume-qualification-backend}
rules:
  - apiGroups: ['']
    resources: [persistentvolumes]
    resourceNames: [kube-memlens-csi-test]
    verbs: [get]
  - apiGroups: [storage.k8s.io]
    resources: [csinodes]
    resourceNames: [${node}]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: volume-qualification-backend}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: volume-qualification-backend}
subjects:
  - {kind: ServiceAccount, name: volume-viewer, namespace: kube-memlens-csi-e2e}
YAML
  python3 hack/volume-qualification/inventory.py --profile "${profile}" --cluster "${cluster}" --node "${node}" --kubeconfig "${kubeconfig}" --output-dir "${artifact_dir}"
  "${work_dir}/kubectl-memlens" --kubeconfig "${kubeconfig}" --context "kind-${cluster}" doctor -o json > "${work_dir}/volume-doctor.json"
  python3 hack/volume-qualification/sample.py --profile "${profile}" --cluster "${cluster}" --node "${node}" --kubeconfig "${kubeconfig}" --phase baseline --output "${artifact_dir}/volume-window-baseline.json"
  helm "${helm_args[@]}" "${volume_args[@]}" --set nodeContext.volumeStats=true --set volumeContext.health=true --set volumeContext.workloads=true > "${work_dir}/volume-qualification-enabled-helm.log" 2>&1
  volume_wait 'd["context"]["volumes"][0]["usage"]["availability"]=="reported"' volume-enabled-ready.json
  python3 hack/volume-qualification/sample.py --profile "${profile}" --cluster "${cluster}" --node "${node}" --kubeconfig "${kubeconfig}" --phase enabled --output "${artifact_dir}/volume-window-enabled.json"
  kctl delete rolebinding qualification-workload -n kube-memlens-csi-e2e >/dev/null
  kctl delete clusterrolebinding volume-qualification-backend >/dev/null
  kctl delete clusterrole volume-qualification-backend >/dev/null
  kctl delete deployment volume-workload -n kube-memlens-csi-e2e --wait=true >/dev/null
  source hack/lib/volume-pressure-verification.sh
  volume_pressure_verification
}
