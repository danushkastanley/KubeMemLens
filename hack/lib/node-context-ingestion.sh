#!/usr/bin/env bash

# Uses only the kubeconfig and resources owned by verify-node-context-kind.sh.
node_context_ingestion() {
  local work_dir=$1 namespace=$2 node=$3 image=$4 audience=$5 ip=$6
  local server_ip
  server_ip=$(kctl get service kubernetes -n default -o jsonpath='{.spec.clusterIP}')
  kctl config view --raw -o json > "${work_dir}/api-config.json"
  python3 - "${work_dir}" <<'PY'
import base64,json,pathlib,sys
root=pathlib.Path(sys.argv[1]); config=json.loads((root/'api-config.json').read_text())
cluster=config['clusters'][0]['cluster']; user=config['users'][0]['user']
for key,name,data in [('certificate-authority-data','api-ca.crt',cluster),('client-certificate-data','api-client.crt',user),('client-key-data','api-client.key',user)]:
    (root/name).write_bytes(base64.b64decode(data[key]))
(root/'api-server').write_text(cluster['server'])
PY
  local helm_args=(upgrade --install kube-memlens charts/kube-memlens --kubeconfig "${kubeconfig}" --kube-context "kind-${cluster}"
    -n "${namespace}" --set "namespace.name=${namespace}" --set "image.repository=${image%:*}" --set "image.tag=${image##*:}"
    --set image.pullPolicy=Never --set agent.tokenExpirationSeconds=600 --set collector.store.maxNodes=10 --set collector.store.maxContainers=2000
    --set collector.resources.limits.memory=512Mi --set 'agent.tolerations[0].operator=Exists'
    --set "nodeContext.kubeletCAConfigMap=node-context-trust" --set "nodeContext.kubeletAudience=${audience}"
    --set "nodeContext.apiServerCIDRs[0]=${server_ip}/32" --set "nodeContext.apiServerCIDRs[1]=${ip}/32"
    --set "nodeContext.nodeCIDRs[0]=${ip}/32" --wait --timeout 180s)
  helm "${helm_args[@]}" --set nodeContext.enabled=false > "${work_dir}/helm-standard.log" 2>&1
  kctl rollout status daemonset/kube-memlens-agent -n "${namespace}" --timeout=120s > "${work_dir}/agent-rollout.log"
  kctl wait --for=condition=Available apiservice/v1alpha1.memory.kubememlens.io --timeout=120s >/dev/null
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"]["totalContainers"] > 0' standard.json
  if [ -n "${NODE_CONTEXT_QUALIFICATION_PROFILE:-}" ]; then
    source hack/lib/node-qualification-kind.sh
    node_qualification_baseline
  fi
  if [ -n "${NODE_CONTEXT_OBSERVER_PROFILE:-}" ]; then
    source hack/lib/node-observer-kind.sh
    node_observer_check baseline
  fi
  helm "${helm_args[@]}" --set nodeContext.enabled=true > "${work_dir}/helm-enabled.log" 2>&1
  kctl rollout status daemonset/kube-memlens-node-context -n "${namespace}" --timeout=120s > "${work_dir}/context-rollout.log"
  kctl create serviceaccount node-viewer -n "${namespace}" >/dev/null
  kctl create serviceaccount tenant-viewer -n "${namespace}" >/dev/null
  kctl create clusterrolebinding node-context-test-viewer --clusterrole=kube-memlens-node-context-viewer --serviceaccount="${namespace}:node-viewer" >/dev/null
  kctl create rolebinding node-context-test-tenant -n "${namespace}" --clusterrole=kube-memlens-namespace-viewer --serviceaccount="${namespace}:tenant-viewer" >/dev/null
  for account in node-viewer tenant-viewer; do
    kctl create token "${account}" -n "${namespace}" --duration=30m > "${work_dir}/${account}.token"
    python3 - "${work_dir}" "${account}" <<'PY'
import pathlib,sys
root=pathlib.Path(sys.argv[1]); account=sys.argv[2]
(root/(account+'.header')).write_text('Authorization: Bearer '+(root/(account+'.token')).read_text().strip()+'\n')
PY
  done
  node_context_wait_read "${work_dir}" node-viewer "/nodecontexts/${node}" 'd["record"].get("lastGood",{}).get("stats",{}).get("memory",{}).get("usageBytes",0)>0' node-good.json
  node_context_wait_read "${work_dir}" node-viewer "/nodecontexts/${node}/history?limit=1" 'len(d.get("series",[]))==1 and len(d["series"][0]["points"])>=2' history-before.json
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"]["totalContainers"]>0 and d["store"].get("nodeContext",{}).get("freshRecords",0)==1' both-sources.json
  if [ -n "${NODE_CONTEXT_QUALIFICATION_PROFILE:-}" ]; then
    node_qualification_measure enabled
  fi
  if [ -n "${NODE_CONTEXT_OBSERVER_PROFILE:-}" ]; then
    node_observer_check enabled
  fi
  node_context_expect_status "${work_dir}" tenant-viewer "/nodecontexts/${node}" 403
  node_context_expect_status "${work_dir}" node-viewer /pods 403
  node_context_expect_status "${work_dir}" node-viewer /containers 403
  node_context_expect_status "${work_dir}" node-viewer '/nodecontexts?limit=101' 400
  source hack/lib/node-analysis-verification.sh
  node_analysis_verification "${work_dir}" "${namespace}" "${node}"
  source hack/lib/node-cockpit-verification.sh
  node_cockpit_verification "${work_dir}" "${namespace}" "${node}" "${kubeconfig}" "kind-${cluster}"
  if [ -n "${NODE_CONTEXT_QUALIFICATION_PROFILE:-}" ]; then
    node_qualification_lifecycle
  elif [ -n "${NODE_CONTEXT_LIFECYCLE_PROFILE:-}" ]; then
    source hack/lib/node-qualification-kind.sh
    node_qualification_lifecycle_diagnostic
  fi
  kctl rollout restart daemonset/kube-memlens-node-context -n "${namespace}" >/dev/null
  kctl rollout status daemonset/kube-memlens-node-context -n "${namespace}" --timeout=120s >/dev/null
  node_context_wait_read "${work_dir}" node-viewer "/nodecontexts/${node}" 'd["record"].get("report",{}).get("reportedAt","")>previous["record"]["report"]["reportedAt"]' producer-replaced.json node-good.json
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"]["reliability"]["lastSnapshotAt"]>previous["store"]["reliability"]["lastSnapshotAt"]' cgroup-after-replacement.json both-sources.json
  kctl rollout restart deployment/kube-memlens-collector -n "${namespace}" >/dev/null
  kctl rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=150s >/dev/null
  node_context_wait_read "${work_dir}" node-viewer "/nodecontexts/${node}/history?limit=1" 'd.get("generation")!=previous["generation"] and d.get("completeness")=="partial" and len(d.get("series",[]))>0' history-rebuilt.json history-before.json
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"]["totalContainers"]>0 and d["store"].get("nodeContext",{}).get("freshRecords",0)==1' both-recovered.json
  kctl delete clusterrolebinding node-context-test-viewer >/dev/null
  node_context_expect_status "${work_dir}" node-viewer "/nodecontexts/${node}" 403
  helm "${helm_args[@]}" --set nodeContext.enabled=false > "${work_dir}/helm-disabled.log" 2>&1
  node_context_wait_read "${work_dir}" admin /clusterstatus/current 'd["store"]["totalContainers"]>0 and "nodeContext" not in d["store"]' standard-after-rollback.json
  node_context_expect_status "${work_dir}" admin "/nodecontexts/${node}" 404
  python3 - "${work_dir}" <<'PY'
import json,pathlib,sys
root=pathlib.Path(sys.argv[1])
for name in ('node-good.json','history-before.json','history-rebuilt.json'):
    data=json.loads((root/name).read_text())
    text=json.dumps(data)
    for forbidden in ('podUID','containerID','cgroupPath','containerCount','podName','namespace'):
        assert '"'+forbidden+'"' not in text, 'Node read retained Pod data'
result={'authenticatedIngestion':True,'independentProducers':True,'nodeOnlyRead':True,
 'tenantNodeDenied':True,'nodeViewerPodDenied':True,'paginationBounded':True,'revocationImmediate':True,
 'producerReplacement':True,'collectorRestartRebuilding':True,'profileUpgradeRollback':True}
(root/'ingestion-result.json').write_text(json.dumps(result))
PY
  echo 'PASS authenticated Node ingestion, source isolation, scoped reads, restart and profile rollback'
}

node_context_request() {
  local work_dir=$1 principal=$2 path=$3 output=$4
  local server
  server=$(cat "${work_dir}/api-server")
  local credentials=(--cert "${work_dir}/api-client.crt" --key "${work_dir}/api-client.key")
  if [ "${principal}" != admin ]; then credentials=(--header "@${work_dir}/${principal}.header"); fi
  curl --silent --show-error --max-time 8 --cacert "${work_dir}/api-ca.crt" "${credentials[@]}" \
    --header 'X-KubeMemLens-Snapshot-Schema: 3' --output "${output}" --write-out '%{http_code}' \
    "${server}/apis/memory.kubememlens.io/v1alpha1${path}"
}

node_context_wait_read() {
  local work_dir=$1 principal=$2 path=$3 predicate=$4 output=$5 previous=${6:-}
  local status
  for _ in $(seq 1 75); do
    status=$(node_context_request "${work_dir}" "${principal}" "${path}" "${work_dir}/${output}" || true)
    if [ "${status}" = 200 ] && python3 - "${work_dir}/${output}" "${predicate}" "${work_dir}/${previous}" <<'PY'
import json,pathlib,sys
try:
    d=json.loads(pathlib.Path(sys.argv[1]).read_text())
    p=pathlib.Path(sys.argv[3]); previous=json.loads(p.read_text()) if p.is_file() else {}
    assert eval(sys.argv[2],{'__builtins__':{},'len':len},{'d':d,'previous':previous})
except (ValueError,KeyError,AssertionError,TypeError):
    raise SystemExit(1)
PY
    then return 0; fi
    sleep 2
  done
  echo 'Node-context read did not reach expected state within the bounded wait' >&2
  return 1
}

node_context_expect_status() {
  local work_dir=$1 principal=$2 path=$3 expected=$4
  local status
  status=$(node_context_request "${work_dir}" "${principal}" "${path}" "${work_dir}/denied-read.json")
  [ "${status}" = "${expected}" ] || { echo "unexpected Node-context read status: ${status}, expected ${expected}" >&2; return 1; }
}
