#!/usr/bin/env bash

set -euo pipefail

kubeconfig=${AUTH_INGEST_KUBECONFIG:-}
context=${AUTH_INGEST_CONTEXT:-}
namespace=${AUTH_INGEST_NAMESPACE:-kube-memlens}
acknowledgement=${AUTH_INGEST_ACKNOWLEDGE:-}
test_pod=kube-memlens-ingestion-test
api_service=v1alpha1.memory.kubememlens.io
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-auth-ingest.XXXXXX")
port_forward_pid=
owner_changed=false

for command in curl jq kubectl; do
  command -v "${command}" >/dev/null 2>&1 || {
    echo "required command not found: ${command}" >&2
    exit 1
  }
done
[ -f "${kubeconfig}" ] || {
  echo "AUTH_INGEST_KUBECONFIG must name a kubeconfig" >&2
  exit 1
}
[[ "${context}" == kind-* ]] || {
  echo "AUTH_INGEST_CONTEXT must name a disposable kind context" >&2
  exit 1
}
[ "${acknowledgement}" = run-and-clean-authenticated-ingestion ] || {
  echo "set AUTH_INGEST_ACKNOWLEDGE=run-and-clean-authenticated-ingestion" >&2
  exit 1
}

kctl() {
  KUBECONFIG="${kubeconfig}" kubectl --context "${context}" "$@"
}

recover_agent() {
  if [ "${owner_changed}" != true ]; then
    return
  fi
  kctl rollout restart deployment/kube-memlens-collector -n "${namespace}" >/dev/null 2>&1 || true
  kctl rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=90s >/dev/null 2>&1 || true
}

cleanup() {
  status=$?
  if [ -n "${port_forward_pid}" ]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
    wait "${port_forward_pid}" 2>/dev/null || true
  fi
  kctl delete pod "${test_pod}" -n "${namespace}" --ignore-not-found --grace-period=0 --wait=false >/dev/null 2>&1 || true
  recover_agent
  rm -rf "${work_dir}"
  exit "${status}"
}
trap cleanup EXIT
trap 'echo "authenticated ingestion verification failed at line ${LINENO}" >&2' ERR

if kctl get pod "${test_pod}" -n "${namespace}" >/dev/null 2>&1; then
  echo "refusing pre-existing Pod ${namespace}/${test_pod}" >&2
  exit 1
fi

kctl wait --for=condition=Available "apiservice/${api_service}" --timeout=90s >/dev/null
kctl get --raw /apis/memory.kubememlens.io/v1alpha1 > "${work_dir}/discovery.json"
jq -e '
  .groupVersion == "memory.kubememlens.io/v1alpha1" and
  ([.resources[] | del(.group, .version, .storageVersionHash)] == [
    {name:"pods", singularName:"pod", namespaced:true, kind:"PodMemory", verbs:["get","list"]},
    {name:"pods/history", singularName:"", namespaced:true, kind:"PodMemoryHistory", verbs:["get"]},
    {name:"containers", singularName:"container", namespaced:true, kind:"ContainerMemory", verbs:["list"]},
    {name:"workloads", singularName:"workload", namespaced:true, kind:"WorkloadMemory", verbs:["list"]},
    {name:"nodes", singularName:"node", namespaced:false, kind:"NodeMemory", verbs:["get","list"]},
    {name:"clusterstatus", singularName:"clusterstatus", namespaced:false, kind:"ClusterStatus", verbs:["get"]},
    {name:"metrics", singularName:"metrics", namespaced:false, kind:"Metrics", verbs:["get"]},
    {name:"ingestionepochs", singularName:"ingestionepoch", namespaced:false, kind:"IngestionEpoch", verbs:["get"]},
    {name:"nodesnapshots", singularName:"nodesnapshot", namespaced:false, kind:"NodeSnapshot", verbs:["create"]}
  ])
' "${work_dir}/discovery.json" >/dev/null
ports=$(kctl get service kube-memlens-collector -n "${namespace}" -o jsonpath='{range .spec.ports[*]}{.port}{"\n"}{end}')
if grep -Fxq 8081 <<<"${ports}"; then
  echo "plaintext ingestion port remains exposed" >&2
  exit 1
fi

kctl create -f - >/dev/null <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: ${test_pod}
  namespace: ${namespace}
spec:
  serviceAccountName: kube-memlens-agent
  automountServiceAccountToken: false
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: hold
      image: public.ecr.aws/docker/library/busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
      imagePullPolicy: IfNotPresent
      command: ["/bin/sh", "-c", "exec sleep 600"]
      securityContext:
        runAsUser: 65532
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: {drop: ["ALL"]}
      resources:
        requests: {cpu: 1m, memory: 2Mi}
        limits: {memory: 16Mi}
YAML
kctl wait --for=condition=Ready "pod/${test_pod}" -n "${namespace}" --timeout=60s >/dev/null

token_one=$(kctl create token kube-memlens-agent -n "${namespace}" --bound-object-kind Pod --bound-object-name "${test_pod}" --duration=10m)
token_two=$(kctl create token kube-memlens-agent -n "${namespace}" --bound-object-kind Pod --bound-object-name "${test_pod}" --duration=10m)
[ "${token_one}" != "${token_two}" ]
server=$(kctl config view --raw -o jsonpath='{.clusters[0].cluster.server}')
kctl config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 --decode > "${work_dir}/ca.crt"
epoch=$(curl -fsS --cacert "${work_dir}/ca.crt" -H "Authorization: Bearer ${token_one}" "${server}/apis/memory.kubememlens.io/v1alpha1/ingestionepochs/current" | jq -r '.epoch')
[ -n "${epoch}" ]
node_name=$(kctl get pod "${test_pod}" -n "${namespace}" -o jsonpath='{.spec.nodeName}')
node_uid=$(kctl get node "${node_name}" -o jsonpath='{.metadata.uid}')
captured_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)

jq -n --arg epoch "${epoch}" --arg node "${node_name}" --arg node_uid "${node_uid}" --arg captured "${captured_at}" '{
  apiVersion:"memory.kubememlens.io/v1alpha1", kind:"NodeSnapshot", metadata:{},
  nodeUID:$node_uid, epoch:$epoch, sequence:1000000,
  snapshot:{schemaVersion:1,nodeName:$node,capturedAt:$captured,environment:{},containers:[]}
}' > "${work_dir}/request.json"
jq '.snapshot.nodeName = "forged-node"' "${work_dir}/request.json" > "${work_dir}/wrong-node.json"

post() {
  token=$1
  body=$2
  shift 2
  curl -sS --output "${work_dir}/response.json" --write-out '%{http_code}' \
    --cacert "${work_dir}/ca.crt" -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' "$@" --data-binary @"${body}" \
    "${server}/apis/memory.kubememlens.io/v1alpha1/nodesnapshots"
}

post_after_rate_limit() {
  local token=$1
  local body=$2
  local code=429
  for _ in $(seq 1 8); do
    code=$(post "${token}" "${body}")
    [ "${code}" = 429 ] || break
    sleep 2
  done
  echo "${code}"
}

[ "$(post "${token_one}" "${work_dir}/wrong-node.json")" = 403 ]
jq -e '.reason == "node_claim_mismatch"' "${work_dir}/response.json" >/dev/null
[ "$(post_after_rate_limit "${token_one}" "${work_dir}/request.json")" = 200 ]
owner_changed=true
jq -e '.accepted == true and .duplicate == false' "${work_dir}/response.json" >/dev/null
[ "$(post "${token_two}" "${work_dir}/request.json")" = 429 ]
[ "$(post_after_rate_limit "${token_two}" "${work_dir}/request.json")" = 200 ]
jq -e '.accepted == true and .duplicate == true' "${work_dir}/response.json" >/dev/null

jq '.snapshot.environment.cgroupDriver = "changed"' "${work_dir}/request.json" > "${work_dir}/changed.json"
[ "$(post_after_rate_limit "${token_two}" "${work_dir}/changed.json")" = 409 ]
jq -e '.reason == "sequence_conflict"' "${work_dir}/response.json" >/dev/null
jq '.sequence = 999999' "${work_dir}/request.json" > "${work_dir}/lower.json"
[ "$(post_after_rate_limit "${token_two}" "${work_dir}/lower.json")" = 409 ]
jq -e '.reason == "sequence_replayed"' "${work_dir}/response.json" >/dev/null
sleep 2
[ "$(post "${token_two}" "${work_dir}/request.json" -H 'Content-Encoding: gzip')" = 415 ]
dd if=/dev/zero of="${work_dir}/oversized" bs=1048576 count=5 2>/dev/null
sleep 2
[ "$(post "${token_two}" "${work_dir}/oversized")" = 413 ]

kctl port-forward -n "${namespace}" service/kube-memlens-collector 18443:443 > "${work_dir}/port-forward.log" 2>&1 &
port_forward_pid=$!
for _ in $(seq 1 20); do
  code=$(curl -sk --output /dev/null --write-out '%{http_code}' https://127.0.0.1:18443/readyz || true)
  [ "${code}" = 200 ] && break
  sleep 0.25
done
code=$(curl -sk --output "${work_dir}/direct.json" --write-out '%{http_code}' \
  -H 'Authorization: Bearer credential-sentinel' \
  -H 'X-Remote-User: system:serviceaccount:kube-memlens:kube-memlens-agent' \
  https://127.0.0.1:18443/apis/memory.kubememlens.io/v1alpha1/ingestionepochs/current)
[ "${code}" = 401 ]
kill "${port_forward_pid}" >/dev/null 2>&1 || true
wait "${port_forward_pid}" 2>/dev/null || true
port_forward_pid=

kctl delete pod "${test_pod}" -n "${namespace}" --grace-period=0 --wait=false >/dev/null
revoked=false
for _ in $(seq 1 45); do
  code=$(curl -sS --output "${work_dir}/revoked.json" --write-out '%{http_code}' \
    --cacert "${work_dir}/ca.crt" -H "Authorization: Bearer ${token_one}" \
    "${server}/apis/memory.kubemlens.io/v1alpha1/ingestionepochs/current")
  if [ "${code}" = 401 ]; then
    revoked=true
    break
  fi
  sleep 2
done
[ "${revoked}" = true ]
recover_agent
owner_changed=false
sleep 10
kctl logs daemonset/kube-memlens-agent -n "${namespace}" --tail=8 > "${work_dir}/agent.log"
grep -q 'posted=true' "${work_dir}/agent.log"
kctl logs deployment/kube-memlens-collector -n "${namespace}" > "${work_dir}/collector.log"
if grep -q 'credential-sentinel' "${work_dir}/collector.log"; then
  echo "credential sentinel appeared in collector logs" >&2
  exit 1
fi

echo "authenticated ingestion kind verification passed"
