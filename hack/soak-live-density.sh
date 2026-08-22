#!/usr/bin/env bash

set -Eeuo pipefail

# shellcheck source=hack/lib/retry.sh
source hack/lib/retry.sh

# Create a dedicated, finite workload namespace against an explicitly selected
# cluster. KubeMemLens must already be installed and healthy.

required_acknowledgement=run-and-remove-kube-memlens-density-soak
context=${SOAK_CONTEXT:-}
namespace=${SOAK_NAMESPACE:-}
collector_namespace=${SOAK_COLLECTOR_NAMESPACE:-kube-memlens}
image=${SOAK_WORKLOAD_IMAGE:-}
artifact_dir=${SOAK_ARTIFACT_DIR:-}
profile=${SOAK_PROFILE:-gate}
containers=${SOAK_CONTAINERS:-}
containers_per_pod=${SOAK_CONTAINERS_PER_POD:-10}
duration=${SOAK_DURATION_SECONDS:-1800}
sample_interval=${SOAK_SAMPLE_INTERVAL_SECONDS:-30}
timeout=${SOAK_READY_TIMEOUT:-30m}
kubeconfig=${KUBECONFIG:-}
work_dir=
cli=
namespace_created=false
outcome=failed
churn_recovery_seconds=0

usage() {
  cat <<'EOF'
Run a live KubeMemLens container-density and churn soak.

Required environment:
  SOAK_CONTEXT                 Exact kubeconfig context
  SOAK_NAMESPACE               New kube-memlens-soak-* namespace
  SOAK_WORKLOAD_IMAGE          Digest-pinned image with /bin/sh and sleep
  SOAK_ARTIFACT_DIR            New or empty local evidence directory
  SOAK_CONTAINERS              5000 or 10000 for the gate profile
  SOAK_ACKNOWLEDGE             run-and-remove-kube-memlens-density-soak

Optional:
  SOAK_COLLECTOR_NAMESPACE     Existing KubeMemLens namespace (default kube-memlens)
  SOAK_CONTAINERS_PER_POD      Exact divisor of SOAK_CONTAINERS (default 10)
  SOAK_DURATION_SECONDS        Steady-state seconds (gate minimum 1800)
  SOAK_SAMPLE_INTERVAL_SECONDS Sampling interval (default 30)
  SOAK_PROFILE=development     Allows 1-500 containers and 30+ seconds

The script creates and removes only its labelled namespace. It does not install
KubeMemLens, create infrastructure, publish artefacts, or change cluster RBAC.
EOF
}

fail() {
  echo "density soak error: $*" >&2
  exit 1
}

is_uint() { [[ "$1" =~ ^[0-9]+$ ]]; }

[ "${SOAK_ACKNOWLEDGE:-}" = "${required_acknowledgement}" ] || {
  usage >&2
  fail "set SOAK_ACKNOWLEDGE=${required_acknowledgement} after reviewing the target"
}
[ -n "${context}" ] || fail "SOAK_CONTEXT is required"
[[ "${namespace}" =~ ^kube-memlens-soak-[a-z0-9]([a-z0-9-]{0,43}[a-z0-9])?$ ]] ||
  fail "SOAK_NAMESPACE must be a new lower-case kube-memlens-soak-* namespace"
[[ "${collector_namespace}" =~ ^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$ ]] || fail "invalid collector namespace"
[[ "${image}" =~ @sha256:[a-f0-9]{64}$ ]] || fail "SOAK_WORKLOAD_IMAGE must be digest-pinned"
is_uint "${containers}" || fail "SOAK_CONTAINERS must be an integer"
is_uint "${containers_per_pod}" || fail "SOAK_CONTAINERS_PER_POD must be an integer"
is_uint "${duration}" || fail "SOAK_DURATION_SECONDS must be an integer"
is_uint "${sample_interval}" || fail "SOAK_SAMPLE_INTERVAL_SECONDS must be an integer"
if [ "${containers_per_pod}" -lt 1 ] || [ "${containers_per_pod}" -gt 50 ]; then
  fail "containers per Pod must be 1-50"
fi
[ $((containers % containers_per_pod)) -eq 0 ] || fail "container target must divide exactly by containers per Pod"
if [ "${sample_interval}" -lt 5 ] || [ "${sample_interval}" -gt 300 ]; then
  fail "sample interval must be 5-300 seconds"
fi
case "${profile}" in
  gate)
    { [ "${containers}" -eq 5000 ] || [ "${containers}" -eq 10000 ]; } || fail "gate profile requires 5000 or 10000 containers"
    [ "${duration}" -ge 1800 ] || fail "gate profile requires at least 1800 steady-state seconds"
    ;;
  development)
    if [ "${containers}" -lt 1 ] || [ "${containers}" -gt 500 ]; then
      fail "development profile allows 1-500 containers"
    fi
    [ "${duration}" -ge 30 ] || fail "development profile requires at least 30 seconds"
    ;;
  *) fail "SOAK_PROFILE must be gate or development" ;;
esac

[ -n "${artifact_dir}" ] || fail "SOAK_ARTIFACT_DIR is required"
if [ "${artifact_dir}" = "/" ] || [ "${artifact_dir}" = "." ]; then
  fail "unsafe artifact directory"
fi
if [ -d "${artifact_dir}" ] && [ -n "$(find "${artifact_dir}" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
  fail "SOAK_ARTIFACT_DIR must be empty"
fi
mkdir -p "${artifact_dir}"
artifact_dir=$(cd "${artifact_dir}" && pwd -P)
repo_root=$(pwd -P)
if [ "${artifact_dir}" = "/" ] || [ "${artifact_dir}" = "${repo_root}" ]; then
  fail "unsafe resolved artifact directory"
fi
chmod 700 "${artifact_dir}"

for command in go jq kubectl python3; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-density.XXXXXX")
cli=${work_dir}/kubectl-memlens
samples=${work_dir}/samples.jsonl
kubectl_args=(--context "${context}")
if [ -n "${kubeconfig}" ]; then
  kubectl_args=(--kubeconfig "${kubeconfig}" "${kubectl_args[@]}")
fi
k() { kubectl "${kubectl_args[@]}" "$@"; }
cli_args=(--context "${context}" --collector-namespace "${collector_namespace}")
if [ -n "${kubeconfig}" ]; then
  cli_args=(--kubeconfig "${kubeconfig}" "${cli_args[@]}")
fi

write_summary() {
  local completed_at=$1
  local samples_json=${work_dir}/samples.json
  jq -s '.' "${samples}" > "${samples_json}"
  jq -n \
    --arg outcome "${outcome}" --arg profile "${profile}" --arg completedAt "${completed_at}" \
    --arg imageDigest "${image##*@}" --argjson target "${containers}" \
    --argjson perPod "${containers_per_pod}" --argjson steadySeconds "${duration}" \
    --argjson churnRecoverySeconds "${churn_recovery_seconds}" --slurpfile samples "${samples_json}" \
    '{schemaVersion: 1, outcome: $outcome, profile: $profile, completedAt: $completedAt,
      target: {containers: $target, containersPerPod: $perPod, steadyStateSeconds: $steadySeconds},
      workloadImage: {repository: "redacted", digest: $imageDigest},
      churn: {rollingRestartRecoverySeconds: $churnRecoverySeconds}, samples: $samples[0],
      privacy: {clusterIdentifiersIncluded: false, workloadIdentifiersIncluded: false},
      caveats: ["Resource telemetry is reported only when the cluster Metrics API is available"]}' \
    > "${artifact_dir}/density-soak-summary.json"
  chmod 600 "${artifact_dir}/density-soak-summary.json"
}

cleanup() {
  local status=$?
  trap - EXIT
  if [ "${namespace_created}" = true ]; then
    owner=$(k get namespace "${namespace}" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}' 2>/dev/null || true)
    if [ "${owner}" = kube-memlens-density-soak ]; then
      k delete namespace "${namespace}" --wait=false >/dev/null 2>&1 || true
    fi
  fi
  write_summary "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  if [[ "${work_dir}" == "${TMPDIR:-/tmp}/kube-memlens-density."* ]]; then
    rm -rf -- "${work_dir}"
  fi
  if [ "${status}" -ne 0 ]; then
    echo "density soak failed; sanitised evidence: ${artifact_dir}" >&2
  fi
  exit "${status}"
}
trap cleanup EXIT

: > "${samples}"
k config get-contexts "${context}" -o name | grep -Fxq "${context}" || fail "kubeconfig context not found"
k version -o json >/dev/null
if k get namespace "${namespace}" >/dev/null 2>&1; then
  fail "namespace already exists"
fi
k get deployment kube-memlens-collector -n "${collector_namespace}" >/dev/null || fail "collector deployment not found"
k get daemonset kube-memlens-agent -n "${collector_namespace}" >/dev/null || fail "agent DaemonSet not found"
k auth can-i create namespaces | grep -Fxq yes || fail "identity cannot create namespaces"
k auth can-i create deployments.apps -n "${namespace}" | grep -Fxq yes || fail "identity cannot create the workload"
k auth can-i get services/proxy -n "${collector_namespace}" | grep -Fxq yes || fail "identity cannot use the collector service proxy"
k auth can-i get pods/proxy -n "${collector_namespace}" | grep -Fxq yes || fail "identity cannot scrape agent metrics through the Pod proxy"

nodes_json=${work_dir}/nodes.json
pods_json=${work_dir}/pods.json
k get nodes -o json > "${nodes_json}"
k get pods -A -o json > "${pods_json}"
linux_nodes=$(jq '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux") | select((.spec.unschedulable // false) == false)] | length' "${nodes_json}")
[ "${linux_nodes}" -gt 0 ] || fail "no schedulable Linux nodes"
pod_capacity=$(jq '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux") | select((.spec.unschedulable // false) == false) | (.status.allocatable.pods | tonumber)] | add // 0' "${nodes_json}")
scheduled_pods=$(jq '[.items[] | select(.spec.nodeName != null) | select(.status.phase != "Succeeded" and .status.phase != "Failed")] | length' "${pods_json}")
pod_count=$((containers / containers_per_pod))
[ $((scheduled_pods + pod_count + linux_nodes * 2)) -le "${pod_capacity}" ] ||
  fail "aggregate Pod capacity is insufficient for ${pod_count} new Pods plus safety margin"

go build -trimpath -o "${cli}" ./cmd/kubectl-memlens
status_json=${work_dir}/status.json
"${cli}" "${cli_args[@]}" status --output json > "${status_json}"
current_containers=$(jq '.store.totalContainers' "${status_json}")
store_capacity=$(jq '.store.maxContainers' "${status_json}")
[ $((current_containers + containers)) -lt "${store_capacity}" ] || fail "collector store capacity is insufficient"

k create namespace "${namespace}" >/dev/null
namespace_created=true
k label namespace "${namespace}" \
  app.kubernetes.io/managed-by=kube-memlens-density-soak \
  pod-security.kubernetes.io/enforce=restricted >/dev/null

containers_json=${work_dir}/containers.json
jq -n --arg image "${image}" --argjson count "${containers_per_pod}" '
  [range(0; $count) | {
    name: ("worker-" + tostring), image: $image, imagePullPolicy: "IfNotPresent",
    command: ["/bin/sh", "-c", "exec sleep 86400"],
    resources: {requests: {cpu: "1m", memory: "1Mi"}},
    securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true,
      runAsNonRoot: true, runAsUser: 65532, capabilities: {drop: ["ALL"]}}
  }]' > "${containers_json}"

jq -n --arg namespace "${namespace}" --argjson replicas "${pod_count}" \
  --slurpfile containers "${containers_json}" '
  {apiVersion: "apps/v1", kind: "Deployment", metadata: {name: "density-workers", namespace: $namespace,
    labels: {"app.kubernetes.io/name": "density-workers", "app.kubernetes.io/managed-by": "kube-memlens-density-soak"}},
   spec: {replicas: $replicas, progressDeadlineSeconds: 1800,
    strategy: {type: "RollingUpdate", rollingUpdate: {maxSurge: 0, maxUnavailable: "10%"}},
    selector: {matchLabels: {"app.kubernetes.io/name": "density-workers"}},
    template: {metadata: {labels: {"app.kubernetes.io/name": "density-workers"}},
      spec: {automountServiceAccountToken: false, terminationGracePeriodSeconds: 0,
        securityContext: {seccompProfile: {type: "RuntimeDefault"}},
        topologySpreadConstraints: [{maxSkew: 1, topologyKey: "kubernetes.io/hostname",
          whenUnsatisfiable: "ScheduleAnyway", labelSelector: {matchLabels: {"app.kubernetes.io/name": "density-workers"}}}],
        containers: $containers[0]}}}}' | k apply -f - >/dev/null

k rollout status deployment/density-workers -n "${namespace}" --timeout="${timeout}"

mapped_count() {
  "${cli}" "${cli_args[@]}" top containers --all-namespaces \
    --field-selector metadata.namespace="${namespace}" --output json 2>/dev/null | jq 'length'
}

wait_for_mapping() {
  local deadline=$((SECONDS + 900)) count=0
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    count=$(mapped_count || true)
    if [ "${count:-0}" -eq "${containers}" ]; then
      return 0
    fi
    sleep 10
  done
  return 1
}

wait_for_mapping || fail "KubeMemLens did not map all ${containers} workload containers within 15 minutes"

sample_once() {
  local phase=$1 start_ns end_ns query_ms doctor_json count metrics_available=false
  local resource_json='{"available":false}' agent_metrics_ok=true agent_metrics operational_json
  doctor_json=${work_dir}/doctor-sample.json
  start_ns=$(python3 -c 'import time; print(time.monotonic_ns())')
  "${cli}" "${cli_args[@]}" doctor --output json > "${doctor_json}"
  end_ns=$(python3 -c 'import time; print(time.monotonic_ns())')
  query_ms=$(((end_ns - start_ns) / 1000000))
  count=$(mapped_count)
  if k top pods -n "${collector_namespace}" --containers --no-headers > "${work_dir}/top-resources.txt" 2>/dev/null; then
    metrics_available=true
    resource_json=$(python3 - "${work_dir}/top-resources.txt" <<'PY'
import json, re, sys

def cpu_m(value):
    if value.endswith("m"):
        return float(value[:-1])
    if value.endswith("n"):
        return float(value[:-1]) / 1_000_000
    return float(value) * 1000

def memory_bytes(value):
    match = re.fullmatch(r"([0-9.]+)([KMGTE]i?)?", value)
    if not match:
        raise ValueError(value)
    number, unit = float(match.group(1)), match.group(2) or ""
    powers = {"": 0, "K": 1, "Ki": 1, "M": 2, "Mi": 2, "G": 3, "Gi": 3, "T": 4, "Ti": 4, "E": 5, "Ei": 5}
    base = 1024 if unit.endswith("i") else 1000
    return int(number * (base ** powers[unit]))

components = {
    "agent": {"containerCount": 0, "cpuMillicores": 0.0, "memoryBytes": 0},
    "collector": {"containerCount": 0, "cpuMillicores": 0.0, "memoryBytes": 0},
}
with open(sys.argv[1], encoding="utf-8") as source:
    for line in source:
        fields = line.split()
        if len(fields) >= 4 and fields[1] in components:
            component = components[fields[1]]
            component["containerCount"] += 1
            component["cpuMillicores"] += cpu_m(fields[2])
            component["memoryBytes"] += memory_bytes(fields[3])
total = {
    "containerCount": sum(item["containerCount"] for item in components.values()),
    "cpuMillicores": sum(item["cpuMillicores"] for item in components.values()),
    "memoryBytes": sum(item["memoryBytes"] for item in components.values()),
}
print(json.dumps({"available": True, "components": components, "total": total}))
PY
)
  fi
  agent_metrics=${work_dir}/agent-metrics.txt
  : > "${agent_metrics}"
  while IFS= read -r agent_pod; do
    agent_pod=${agent_pod#pod/}
    if ! k get --raw "/api/v1/namespaces/${collector_namespace}/pods/${agent_pod}:8082/proxy/metrics" >> "${agent_metrics}" 2>/dev/null; then
      agent_metrics_ok=false
    fi
  done < <(k get pods -n "${collector_namespace}" -l app.kubernetes.io/name=kube-memlens-agent -o name)
  agent_found=$(awk '$1 == "kubememlens_agent_last_scan_containers{kind=\"found\"}" {sum += $2} END {print sum + 0}' "${agent_metrics}")
  agent_mapped=$(awk '$1 == "kubememlens_agent_last_scan_containers{kind=\"mapped\"}" {sum += $2} END {print sum + 0}' "${agent_metrics}")
  agent_unmapped=$(awk '$1 == "kubememlens_agent_last_scan_containers{kind=\"unmapped\"}" {sum += $2} END {print sum + 0}' "${agent_metrics}")
  agent_scan_max=$(awk '$1 == "kubememlens_agent_last_scan_duration_seconds" && $2 > max {max = $2} END {print max + 0}' "${agent_metrics}")
  agent_post_failures=$(awk '$1 == "kubememlens_agent_snapshot_posts_total{result=\"failure\"}" {sum += $2} END {print sum + 0}' "${agent_metrics}")
  operational_json=$(k get pods -n "${collector_namespace}" -o json | jq '
    {pods: (.items | length), ready: ([.items[] | select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] | length),
     restarts: ([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKilled: ([.items[].status.containerStatuses[]? | select(.lastState.terminated.reason == "OOMKilled")] | length)}')
  jq -n --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg phase "${phase}" \
    --argjson queryMs "${query_ms}" --argjson workloadContainers "${count}" \
    --argjson reportedContainers "$(jq '.mapping.containers' "${doctor_json}")" \
    --argjson mapped "$(jq '.mapping.mapped' "${doctor_json}")" \
    --argjson unmapped "$(jq '.mapping.unmapped' "${doctor_json}")" \
    --argjson coverage "$(jq '.mapping.coverage' "${doctor_json}")" \
    --argjson metricsAvailable "${metrics_available}" --argjson resources "${resource_json}" --argjson operational "${operational_json}" \
    --argjson agentMetricsAvailable "${agent_metrics_ok}" --argjson agentFound "${agent_found}" \
    --argjson agentMapped "${agent_mapped}" --argjson agentUnmapped "${agent_unmapped}" \
    --argjson agentScanMaxSeconds "${agent_scan_max}" --argjson agentPostFailures "${agent_post_failures}" \
    '{at: $at, phase: $phase, queryMs: $queryMs, workloadContainers: $workloadContainers,
      clusterMapping: {reported: $reportedContainers, mapped: $mapped, unmapped: $unmapped, coverage: $coverage},
      metricsAPIAvailable: $metricsAvailable, kubeMemLensResources: $resources, kubeMemLensPods: $operational,
      agents: {metricsAvailable: $agentMetricsAvailable, found: $agentFound, mapped: $agentMapped,
        unmapped: $agentUnmapped, maxScanSeconds: $agentScanMaxSeconds, postFailures: $agentPostFailures}}' >> "${samples}"
}

sample_once steady
steady_deadline=$((SECONDS + duration))
while [ "${SECONDS}" -lt "${steady_deadline}" ]; do
  sleep_for=${sample_interval}
  [ $((SECONDS + sleep_for)) -le "${steady_deadline}" ] || sleep_for=$((steady_deadline - SECONDS))
  [ "${sleep_for}" -gt 0 ] && sleep "${sleep_for}"
  sample_once steady
done

churn_started=${SECONDS}
k rollout restart deployment/density-workers -n "${namespace}" >/dev/null
k rollout status deployment/density-workers -n "${namespace}" --timeout="${timeout}"
wait_for_mapping || fail "mapping did not recover after rolling restart"
churn_recovery_seconds=$((SECONDS - churn_started))
sample_once post-churn

final_doctor=${work_dir}/final-doctor.json
if ! retry_to_file 24 5 "${final_doctor}" \
  "${cli}" "${cli_args[@]}" doctor --strict --output json; then
  jq '{checks, mapping}' "${final_doctor}" \
    | tee "${artifact_dir}/final-doctor-failure.json" >&2
  chmod 600 "${artifact_dir}/final-doctor-failure.json"
  fail "final strict doctor check failed"
fi
outcome=passed
echo "density soak passed; sanitised evidence: ${artifact_dir}"
