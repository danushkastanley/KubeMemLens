#!/usr/bin/env bash
# Existing variables and helpers belong to the owned volume fixture run.
# shellcheck disable=SC2154
volume_stats_lifecycle() {
  # Retain route and core Pod access, revoke only the already-used PVC right.
  kctl patch clusterrole kube-memlens-volume-viewer --type=json \
    -p='[{"op":"replace","path":"/rules/1/resources","value":["pods"]}]' >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"]["availability"]=="forbidden" and "pvcName" not in d["context"]["volumes"][0] and "lastGood" not in d["context"]["volumes"][0]["usage"]' volume-pvc-denied.json
  kctl patch clusterrole kube-memlens-volume-viewer --type=json \
    -p='[{"op":"replace","path":"/rules/1/resources","value":["persistentvolumeclaims"]}]' >/dev/null
  node_context_expect_status "${work_dir}" volume-viewer "${volume_path}" 403 4
  kctl patch clusterrole kube-memlens-volume-viewer --type=json \
    -p='[{"op":"replace","path":"/rules/1/resources","value":["pods","persistentvolumeclaims"]}]' >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"]["availability"]=="reported"' volume-authorised-again.json
  local collector_start
  collector_start=$(volume_recovery_now)
  kctl rollout restart deployment/kube-memlens-collector -n "${namespace}" >/dev/null
  kctl rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=150s >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"]["availability"]=="reported"' volume-collector-restarted.json
  local collector_seconds producer_start
  collector_seconds=$(volume_recovery_seconds "${collector_start}")
  producer_start=$(volume_recovery_now)
  kctl rollout restart daemonset/kube-memlens-node-context -n "${namespace}" >/dev/null
  kctl rollout status daemonset/kube-memlens-node-context -n "${namespace}" --timeout=120s >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"].get("filesystem",{}).get("capturedAt","")>previous["context"]["volumes"][0]["usage"]["filesystem"]["capturedAt"]' volume-producer-restarted.json volume-collector-restarted.json
  local producer_seconds
  producer_seconds=$(volume_recovery_seconds "${producer_start}")
  # Stop only the owned producer so retained source timestamps age naturally.
  kctl patch daemonset kube-memlens-node-context -n "${namespace}" --type=merge \
    -p='{"spec":{"template":{"spec":{"nodeSelector":{"kubememlens.io/volume-fixture-pause":"true"}}}}}' >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"]["freshness"]=="stale"' volume-stale.json
  volume_wait 'd["context"]["volumes"][0]["usage"]["availability"]=="unreported" and "filesystem" not in d["context"]["volumes"][0]["usage"] and "lastGood" not in d["context"]["volumes"][0]["usage"]' volume-expired.json
  local pod_start
  pod_start=$(volume_recovery_now)
  kctl delete pod persistent -n kube-memlens-csi-e2e --wait=true >/dev/null
  node_context_expect_status "${work_dir}" volume-viewer "${volume_path}" 404 4
  kctl apply -f "${work_dir}/persistent.yaml" >/dev/null
  kctl wait pod/persistent -n kube-memlens-csi-e2e --for=condition=Ready --timeout=120s >/dev/null
  volume_wait 'd["metadata"]["uid"]!=previous["metadata"]["uid"] and d["context"]["volumes"][0]["usage"]["availability"]=="unreported"' volume-recreated-unreported.json volume-written.json
  local pod_seconds source_start
  pod_seconds=$(volume_recovery_seconds "${pod_start}")
  source_start=$(volume_recovery_now)
  kctl patch daemonset kube-memlens-node-context -n "${namespace}" --type=merge \
    -p='{"spec":{"template":{"spec":{"nodeSelector":{"kubememlens.io/volume-fixture-pause":null}}}}}' >/dev/null
  kctl rollout status daemonset/kube-memlens-node-context -n "${namespace}" --timeout=120s >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"]["availability"]=="reported" and d["metadata"]["uid"]==previous["metadata"]["uid"]' volume-recreated-fresh.json volume-recreated-unreported.json
  local source_seconds
  source_seconds=$(volume_recovery_seconds "${source_start}")
  helm "${helm_args[@]}" "${volume_args[@]}" --set nodeContext.volumeStats=false > "${work_dir}/helm-volume-stats-off.log" 2>&1
  volume_wait 'd["context"]["volumes"][0]["usage"]["availability"]=="disabled" and "filesystem" not in d["context"]["volumes"][0]["usage"] and "lastGood" not in d["context"]["volumes"][0]["usage"]' volume-disabled.json
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"].get("nodeContext",{}).get("freshRecords",0)==1 and d["store"]["totalContainers"]>0' volume-disabled-node-fresh.json
  helm "${helm_args[@]}" --set nodeContext.enabled=true > "${work_dir}/helm-volume-rollback.log" 2>&1
  # Helm readiness can precede aggregation readiness after the collector changes.
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"]["totalContainers"]>0' volume-rollback-ready.json
  node_context_expect_status "${work_dir}" admin "${volume_path}" 404 4
  python3 - "${work_dir}" "${collector_seconds}" "${producer_seconds}" "${source_seconds}" "${pod_seconds}" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1]); before=json.loads((p/'volume-producer-restarted.json').read_text()); stale=json.loads((p/'volume-stale.json').read_text())
assert stale['context']['volumes'][0]['usage']['filesystem']['capturedAt']>=before['context']['volumes'][0]['usage']['filesystem']['capturedAt']
(p/'volume-lifecycle.json').write_text(json.dumps({'warmPVCRevocation':True,'warmPodRevocation':True,'collectorRestart':True,
 'producerReplacement':True,'staleObserved':True,'expiredValuesRemoved':True,'podDeletion404':True,'podRecreationBound':True,
 'volumeDisabledNodeMemoryFresh':True,'profileRollback':True,'recovery':dict(zip(('collector','producer','source','pod'),map(float,sys.argv[2:])))}))
PY
}

# Whole-second counters can record a sub-second recovery as zero.
volume_recovery_now() {
  python3 -c 'import time; print(time.monotonic_ns())'
}

volume_recovery_seconds() {
  python3 -c 'import sys,time; print((time.monotonic_ns()-int(sys.argv[1]))/1_000_000_000)' "$1"
}
