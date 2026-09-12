#!/usr/bin/env bash

# Runs inside the disposable Node-context fixture; all raw data stays private.
node_analysis_verification() {
  local work_dir=$1 namespace=$2 node=$3
  local workload_image=public.ecr.aws/docker/library/busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
  for tenant in node-analysis-a node-analysis-b; do
    kctl create namespace "${tenant}" >/dev/null
    kctl apply -n "${tenant}" -f - >/dev/null <<EOF_WORKLOAD
apiVersion: apps/v1
kind: Deployment
metadata: {name: memory-hold}
spec:
  replicas: 1
  selector: {matchLabels: {app: memory-hold}}
  template:
    metadata: {labels: {app: memory-hold}}
    spec:
      nodeName: ${node}
      automountServiceAccountToken: false
      securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532, seccompProfile: {type: RuntimeDefault}}
      containers:
        - name: memory
          image: ${workload_image}
          command: ["sh", "-c", "dd if=/dev/zero of=/memory/buffer bs=1M count=8 2>/dev/null; sleep 900"]
          resources: {requests: {cpu: 10m, memory: 16Mi}, limits: {memory: 32Mi}}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: ["ALL"]}}
          volumeMounts: [{name: memory, mountPath: /memory}]
      volumes: [{name: memory, emptyDir: {medium: Memory, sizeLimit: 16Mi}}]
EOF_WORKLOAD
    kctl rollout status deployment/memory-hold -n "${tenant}" --timeout=120s >/dev/null
  done
  # Namespace Pod permission must not qualify a cluster-wide subtraction.
  kctl create rolebinding node-analysis-tenant -n node-analysis-a --clusterrole=kube-memlens-namespace-viewer --serviceaccount="${namespace}:node-viewer" >/dev/null
  node_context_wait_read "${work_dir}" node-viewer "/nodecontexts/${node}/analysis" 'd["analysis"]["contributorAccess"]=="node-only" and "rankings" not in d["analysis"] and "coverage" not in d["analysis"]' analysis-node-only.json
  kctl apply -f - >/dev/null <<EOF_ROLE
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: node-analysis-pod-list}
rules:
  - apiGroups: ["memory.kubememlens.io"]
    resources: ["pods"]
    verbs: ["list"]
EOF_ROLE
  kctl create clusterrolebinding node-analysis-pod-list --clusterrole=node-analysis-pod-list --serviceaccount="${namespace}:node-viewer" >/dev/null
  node_context_wait_read "${work_dir}" node-viewer "/nodecontexts/${node}/analysis?limit=100" 'd["analysis"]["contributorAccess"]=="cluster-pods" and len(d["analysis"].get("rankings",{}).get("pods",[]))>=2' analysis-full.json
  local matched=false status
  for _ in $(seq 1 30); do
    node_context_request "${work_dir}" node-viewer "/nodecontexts/${node}/analysis?limit=100" "${work_dir}/analysis-full.json" >/dev/null
    status=$(node_context_request "${work_dir}" admin '/containers?limit=500' "${work_dir}/analysis-containers.json")
    if [ "${status}" = 200 ] && python3 - "${work_dir}" <<'PY'
import json,pathlib,sys
root=pathlib.Path(sys.argv[1]); analysis=json.loads((root/'analysis-full.json').read_text())['analysis']
page=json.loads((root/'analysis-containers.json').read_text())
try:
    assert not page.get('metadata',{}).get('continue')
    rows=[i['snapshot'] for i in page['items']]
    assert rows and all(c['capturedAt']==analysis['coverage']['capturedAt'] for c in rows)
    mapped=[c for c in rows if all(c.get(k) for k in ('namespace','podName','podUID','containerName','containerID'))]
    assert sum(c['memory']['TotalBytes'] for c in mapped)==analysis['observedPodChargeBytes']
    namespaces={p['namespace'] for p in analysis['rankings']['pods']}
    assert {'node-analysis-a','node-analysis-b'}<=namespaces
    assert analysis['outsideObservedPods']['state']=='unreported' and 'bytes' not in analysis['outsideObservedPods']
    assert 'accounting-unqualified' in analysis['outsideObservedPods']['caveats']
except (AssertionError,KeyError,TypeError):
    raise SystemExit(1)
PY
    then matched=true; break; fi
    sleep 2
  done
  [ "${matched}" = true ] || { echo 'Node analysis did not match a paired cgroup frame' >&2; return 1; }
  for rank in total anon cache shmem residual psi oom; do
    node_context_wait_read "${work_dir}" node-viewer "/nodecontexts/${node}/analysis?rank=${rank}&limit=10" "d[\"analysis\"].get(\"rankings\",{}).get(\"metric\")==\"${rank}\"" "analysis-${rank}.json"
  done
  kctl delete clusterrolebinding node-analysis-pod-list >/dev/null
  node_context_wait_read "${work_dir}" node-viewer "/nodecontexts/${node}/analysis" 'd["analysis"]["contributorAccess"]=="node-only" and "rankings" not in d["analysis"] and "coverage" not in d["analysis"] and "observedPodChargeBytes" not in d["analysis"]' analysis-revoked.json
  python3 - "${work_dir}" <<'PY'
import json,pathlib,sys
root=pathlib.Path(sys.argv[1])
for name in ('analysis-node-only.json','analysis-revoked.json'):
    text=(root/name).read_text()
    assert 'node-analysis-a' not in text and 'node-analysis-b' not in text and 'memory-hold' not in text
(root/'analysis-result.json').write_text(json.dumps({'pairedCgroupSum':True,'twoTenantContributors':True,
 'nodeOnlyPrivacy':True,'namespacePermissionInsufficient':True,'secondaryRevocation':True,
 'allRankingModes':True,'unqualifiedGapSuppressed':True,'accountingQualification':'not claimed for kind'}))
PY
  echo 'PASS paired Node analysis, contributor rankings and secondary authorisation revocation'
}
