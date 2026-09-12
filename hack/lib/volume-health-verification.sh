#!/usr/bin/env bash
# Parent owns and validates the disposable context, images, resources and arrays.
# shellcheck disable=SC2154
volume_health_verification() {
  local profile=${NODE_CONTEXT_VOLUME_HEALTH_PROFILE:?}
  helm "${helm_args[@]}" "${volume_args[@]}" --set nodeContext.volumeStats=true --set volumeContext.health=true > "${work_dir}/helm-health.log" 2>&1
  volume_health_wait pv-denied health-pv-denied.json
  kctl apply -f - >/dev/null <<YAML
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: volume-fixture-pv}
rules:
  - apiGroups: ['']
    resources: [persistentvolumes]
    resourceNames: [kube-memlens-csi-test]
    verbs: [get]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: volume-fixture-node-health}
rules:
  - apiGroups: [storage.k8s.io]
    resources: [csinodes]
    resourceNames: [${node}]
    verbs: [get]
YAML
  kctl create clusterrolebinding volume-fixture-pv --clusterrole=volume-fixture-pv --serviceaccount=kube-memlens-csi-e2e:volume-viewer >/dev/null
  volume_health_wait backend-denied health-node-denied.json
  kctl create clusterrolebinding volume-fixture-node-health --clusterrole=volume-fixture-node-health --serviceaccount=kube-memlens-csi-e2e:volume-viewer >/dev/null
  local handle
  handle=$(python3 - "${work_dir}/csi-volume.json" <<'PY'
import json,pathlib,re,sys
value=json.loads(pathlib.Path(sys.argv[1]).read_text())['volumeID']
assert re.fullmatch('[a-f0-9-]{36}',value)
print(value)
PY
)
  kctl exec driver -n kube-memlens-csi-e2e -c driver -- /healthctl mark-volume-unhealthy "${handle}" --scope node --status DEGRADED --reason LocalFixture --message r5-private-marker > "${work_dir}/health-mark.log" 2>&1
  kctl exec driver -n kube-memlens-csi-e2e -c driver -- /healthctl mark-storage-unhealthy --status STORAGE_DEGRADED --reason LocalFixture --message r5-private-marker >> "${work_dir}/health-mark.log" 2>&1
  if [ "${profile}" = off ]; then
    volume_health_wait off health-off.json
  else
    # The controller source is an explicit API fixture; Pod/backend reports come
    # from the real pinned driver's health calls through the kubelet.
    kctl patch pvc data -n kube-memlens-csi-e2e --subresource=status --type=merge \
      -p='{"status":{"healthStatus":{"healthConditions":[]}}}' >/dev/null
    volume_health_wait adverse health-adverse.json
    kctl rollout restart deployment/kube-memlens-collector -n "${namespace}" >/dev/null
    kctl rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=150s >/dev/null
    volume_health_wait adverse health-restarted.json
    kctl exec driver -n kube-memlens-csi-e2e -c driver -- /healthctl mark-volume-healthy "${handle}" --scope node > "${work_dir}/health-recover.log" 2>&1
    kctl exec driver -n kube-memlens-csi-e2e -c driver -- /healthctl mark-storage-healthy >> "${work_dir}/health-recover.log" 2>&1
    kctl patch pvc data -n kube-memlens-csi-e2e --subresource=status --type=merge \
      -p='{"status":{"healthStatus":{"healthConditions":[{"status":"DataLoss","reason":"ControllerFixture","message":"r5-private-marker"}]}}}' >/dev/null
    volume_health_wait recovered health-recovered.json
    volume_health_wait schema4 health-schema4.json 4
  fi
  "${work_dir}/volume-probe" --kubeconfig "${kubeconfig}" --token-file "${work_dir}/volume-viewer.token" > "${work_dir}/health-client.json"
  kctl delete clusterrolebinding volume-fixture-node-health >/dev/null
  volume_health_wait backend-denied health-warm-node-denied.json
  kctl delete clusterrolebinding volume-fixture-pv >/dev/null
  volume_health_wait pv-denied health-warm-pv-denied.json
  helm "${helm_args[@]}" "${volume_args[@]}" --set nodeContext.volumeStats=true --set volumeContext.health=false > "${work_dir}/helm-health-off.log" 2>&1
  volume_health_wait disabled health-disabled.json
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"].get("nodeContext",{}).get("freshRecords",0)==1 and d["store"]["totalContainers"]>0' health-disabled-memory.json
  kctl delete clusterrole volume-fixture-node-health volume-fixture-pv >/dev/null
  python3 - "${work_dir}" "${profile}" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]);alpha=sys.argv[2]=='alpha'
result={'outcome':'passed','profile':sys.argv[2],'nodeAndBackendSource':'pinned upstream CSI driver through kubelet',
 'productionClient':json.loads((p/'health-client.json').read_text()),
 'controllerSource':'controlled Kubernetes API fixture' if alpha else 'not reported',
 'conflictingSourcesVerified':alpha,'separateRecoveryAndHistoricalBackend':alpha,'schema4Projection':alpha,'collectorRestart':alpha,
 'warmPVRevocation':True,'warmCSINodeRevocation':True,'messagesRemoved':True,'healthDisabledUsageAndMemoryContinue':True,
 'credentialsRetained':False,'runtimeIdentifiersIncluded':False,'providerQualification':False}
(p/'volume-health-result.json').write_text(json.dumps(result))
PY
  echo 'PASS source-separated CSI health, individual permissions, redaction and independent health rollback'
}

# The parent supplies the validated work directory and exact named Pod route.
# shellcheck disable=SC2154
volume_health_wait() {
  local expected=$1 output=$2 schema=${3:-5} status
  for _ in $(seq 1 75); do
    status=$(node_context_request "${work_dir}" volume-viewer "${volume_path}" "${work_dir}/${output}" "${schema}" || true)
    if [ "${status}" = 200 ] && python3 hack/fixtures/volume-stats/check_health.py "${work_dir}/${output}" "${expected}"; then return 0; fi
    sleep 2
  done
  echo "volume health fixture did not reach ${expected} within the bounded wait" >&2
  return 1
}
