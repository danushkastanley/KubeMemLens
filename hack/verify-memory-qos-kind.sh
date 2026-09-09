#!/usr/bin/env bash
set -Eeuo pipefail

kubeconfig=${QOS_KUBECONFIG:?QOS_KUBECONFIG is required}
context=${QOS_CONTEXT:?QOS_CONTEXT is required}
cli=${QOS_CLI:?QOS_CLI is required}
artifact_dir=${QOS_ARTIFACT_DIR:?QOS_ARTIFACT_DIR is required}
profile=${QOS_PROFILE:-default}
namespace=kube-memlens-qos-e2e
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-qos.XXXXXX")
created=false
load_pid=
case "${context}" in kind-*) ;; *) echo 'MemoryQoS verification requires kind' >&2; exit 1 ;; esac
case "${profile}" in default|throttling) ;; *) echo 'unknown MemoryQoS profile' >&2; exit 1 ;; esac
[ "${QOS_ACKNOWLEDGE:-}" = run-and-remove-memory-qos-fixtures ] || { echo 'set QOS_ACKNOWLEDGE=run-and-remove-memory-qos-fixtures' >&2; exit 1; }
kctl() { kubectl --kubeconfig "${kubeconfig}" --context "${context}" "$@"; }
cli_args=(--connect-mode kubernetes-api --kubeconfig "${kubeconfig}" --context "${context}")
cleanup() {
  local result=$?
  if [ -n "${load_pid}" ]; then kill "${load_pid}" 2>/dev/null || true; wait "${load_pid}" 2>/dev/null || true; fi
  if [ "${created}" = true ]; then kctl delete namespace "${namespace}" --wait=true --timeout=90s >/dev/null || result=1; fi
  rm -rf "${work_dir}"
  exit "${result}"
}
trap cleanup EXIT
trap 'echo "MemoryQoS verification failed at line ${LINENO}" >&2' ERR
if kctl get namespace "${namespace}" >/dev/null 2>&1; then echo 'refusing to replace MemoryQoS fixture namespace' >&2; exit 1; fi
"${cli}" "${cli_args[@]}" --help >/dev/null
kctl create namespace "${namespace}" >/dev/null
created=true
kctl apply -n "${namespace}" -f - >/dev/null <<'YAML'
apiVersion: v1
kind: Pod
metadata:
  name: qos-budget
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    runAsGroup: 1000
    fsGroup: 1000
    seccompProfile:
      type: RuntimeDefault
  resources:
    requests:
      memory: 96Mi
    limits:
      memory: 256Mi
  containers:
    - name: worker
      image: public.ecr.aws/docker/library/busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
      command: ["sh", "-c", "exec sleep 86400"]
      resources:
        requests:
          memory: 32Mi
        limits:
          memory: 128Mi
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
      volumeMounts:
        - name: load
          mountPath: /load
    - name: shared
      image: public.ecr.aws/docker/library/busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
      command: ["sh", "-c", "exec sleep 86400"]
      resources:
        requests:
          memory: 16Mi
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
  volumes:
    - name: load
      emptyDir:
        sizeLimit: 320Mi
YAML
kctl wait --for=condition=Ready pod/qos-budget -n "${namespace}" --timeout=120s >/dev/null
wait_for_qos() {
  local predicate=$1
  for _ in $(seq 1 60); do
    if "${cli}" "${cli_args[@]}" explain pod qos-budget -n "${namespace}" -o json > "${work_dir}/explanation.json" 2>/dev/null &&
      jq -e "${predicate}" "${work_dir}/explanation.json" >/dev/null; then return; fi
    sleep 2
  done
  echo "MemoryQoS observation did not meet: ${predicate}" >&2
  jq '[.containers[]? | {name,memoryQoS}]' "${work_dir}/explanation.json" >&2 || true
  return 1
}
wait_for_qos '.schemaVersion == 3 and (.containers | length) == 2 and all(.containers[]; .memoryQoS.state == "observed") and any(.containers[]; .name == "shared" and .memoryQoS.throttleHigh.state == "unlimited" and .memoryQoS.podConfiguredLimit.bytes == 268435456)'
"${cli}" "${cli_args[@]}" capture -n "${namespace}" --pod qos-budget -o "${work_dir}/before.json" >/dev/null
crossed=false
if [ "${profile}" = throttling ]; then
  wait_for_qos 'any(.containers[]; .name == "worker" and .memoryQoS.throttleHigh.bytes == 83886080 and .memoryQoS.reclaimProtectionLow.bytes == 33554432)'
  kctl exec qos-budget -n "${namespace}" -c worker -- timeout 45 sh -ec 'while [ ! -e /load/stop ]; do dd if=/dev/zero of=/load/pressure bs=1M count=256 >/dev/null 2>&1; done' > "${work_dir}/load.log" 2>&1 &
  load_pid=$!
  wait_for_qos 'any(.containers[]; .name == "worker" and .memoryQoS.activity == "crossed-with-stalls" and .memoryQoS.confidence == "high" and .memoryQoS.highDelta > 0)'
  "${cli}" "${cli_args[@]}" capture -n "${namespace}" --pod qos-budget -o "${work_dir}/after.json" >/dev/null
  "${cli}" compare --before "${work_dir}/before.json" --after "${work_dir}/after.json" --pod "${namespace}/qos-budget" > "${work_dir}/comparison.txt"
  grep -Fq 'throttle activity:' "${work_dir}/comparison.txt"
  jq -e 'any(.pods[0].containers[]; .containerName == "worker" and .memory.OOMKillEvents == 0 and .memory.LocalOOMKillEvents == 0)' "${work_dir}/after.json" >/dev/null
  kctl get pod qos-budget -n "${namespace}" -o json | jq -e 'all(.status.containerStatuses[]; .restartCount == 0)' >/dev/null
  kctl exec qos-budget -n "${namespace}" -c worker -- touch /load/stop
  wait "${load_pid}"
  load_pid=
  crossed=true
else
  wait_for_qos 'all(.containers[]; .memoryQoS.throttleHigh.state == "unlimited" and .memoryQoS.reclaimProtectionMin.state == "zero" and .memoryQoS.reclaimProtectionLow.state == "zero")'
fi
"${cli}" replay "${work_dir}/before.json" --pod "${namespace}/qos-budget" > "${work_dir}/replay.txt"
grep -Fq 'Reclaim protection memory.min:' "${work_dir}/replay.txt"
grep -Fq 'parent cgroup controls are not observed' "${work_dir}/replay.txt"
mkdir -p "${artifact_dir}"
source_dirty=false
[ -z "$(git status --porcelain)" ] || source_dirty=true
jq -n --arg commit "$(git rev-parse HEAD)" --arg profile "${profile}" --argjson crossed "${crossed}" --argjson sourceDirty "${source_dirty}" --slurpfile observation "${work_dir}/explanation.json" \
  '{outcome:"passed",sourceCommit:$commit,sourceDirty:$sourceDirty,profile:$profile,observedWorker:($observation[0].containers[] | select(.name=="worker") | .memoryQoS),checks:{observedBoundaries:true,unlimitedLeafWithPodBudget:true,captureReplay:true,recentCrossingWithPSI:$crossed},privacy:{runtimeIdentifiersIncluded:false}}' > "${artifact_dir}/memory-qos-summary.json"
echo "MemoryQoS ${profile} kind verification passed"
