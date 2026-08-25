#!/usr/bin/env bash

set -Eeuo pipefail

# shellcheck source=hack/lib/retry.sh
source hack/lib/retry.sh
# shellcheck source=hack/lib/density-telemetry.sh
source hack/lib/density-telemetry.sh
# shellcheck source=hack/lib/density-summary.sh
source hack/lib/density-summary.sh
# shellcheck source=hack/lib/density-runtime.sh
source hack/lib/density-runtime.sh

# Create a dedicated, finite workload namespace against an explicitly selected
# cluster. KubeMemLens must already be installed and healthy.

required_acknowledgement=run-and-remove-kube-memlens-density-soak
context=${SOAK_CONTEXT:-}
namespace=${SOAK_NAMESPACE:-}
collector_namespace=${SOAK_COLLECTOR_NAMESPACE:-kube-memlens}
artifact_dir=${SOAK_ARTIFACT_DIR:-}
profile_path=${SOAK_PROFILE_PATH:-}
timeout=${SOAK_READY_TIMEOUT:-30m}
kubeconfig=${KUBECONFIG:-}
work_dir=
cli=
namespace_created=false
outcome=failed
churn_recovery_seconds=0
port_forward_pid=
agent_blocked=false
agent_node_selector=
baseline_restarts=0
baseline_oom_kills=0
baseline_workload_restarts=0
baseline_workload_oom_kills=0
baseline_component_restarts=0
baseline_component_oom_kills=0
component_pod_uids='[]'
workload_pod_uids='[]'
disruption_unexplained_restarts=0
disruption_oom_kills=0
paused_kind_node=
# shellcheck disable=SC2034 # consumed by the sourced summary library
worker_node_recovery_seconds=0

usage() {
  cat <<'EOF'
Run a live KubeMemLens container-density and churn soak.

Required environment:
  SOAK_CONTEXT                 Exact kubeconfig context
  SOAK_NAMESPACE               New kube-memlens-soak-* namespace
  SOAK_ARTIFACT_DIR            New or empty local evidence directory
  SOAK_PROFILE_PATH            Reviewed profile under hack/scale-profiles
  SOAK_ACKNOWLEDGE             run-and-remove-kube-memlens-density-soak

Optional:
  SOAK_COLLECTOR_NAMESPACE     Existing KubeMemLens namespace (default kube-memlens)
  SOAK_READY_TIMEOUT           Workload readiness timeout (default 30m)
  SOAK_AGENT_METRICS_LOCAL_PORT Local loopback port for one-at-a-time agent metric forwarding

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
for command in jq python3; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done
[ -f "${profile_path}" ] || fail "SOAK_PROFILE_PATH must name a reviewed profile"
python3 hack/scale-profiles/evaluate.py --profile "${profile_path}" --validate-profile ||
  fail "SOAK_PROFILE_PATH is not a valid self-authenticating profile"
for legacy_override in SOAK_WORKLOAD_IMAGE SOAK_PROFILE SOAK_CONTAINERS SOAK_CONTAINERS_PER_POD SOAK_DURATION_SECONDS SOAK_SAMPLE_INTERVAL_SECONDS; do
  [ -z "${!legacy_override:-}" ] || fail "${legacy_override} cannot override a versioned profile"
done
profile_mode=$(jq -er '.mode' "${profile_path}")
profile_digest=$(jq -er '.profileDigest' "${profile_path}")
telemetry_required=$(jq -r '.telemetryRequired' "${profile_path}")
image=$(jq -er '.workload.image' "${profile_path}")
containers=$(jq -er '.workload.containers' "${profile_path}")
containers_per_pod=$(jq -er '.workload.containersPerPod' "${profile_path}")
duration=$(jq -er '.workload.steadyStateSeconds' "${profile_path}")
sample_interval=$(jq -er '.workload.sampleIntervalSeconds' "${profile_path}")
canary_mib=$(jq -er '.workload.canaryMiB' "${profile_path}")
agent_interval=$(jq -er '.workload.agentInterval' "${profile_path}")
canary_control_samples=$(jq -er '.evidence.canaryControlSamples' "${profile_path}")
[[ "${profile_digest}" =~ ^sha256:[a-f0-9]{64}$ ]] || fail "profile digest must be sha256"
{ [ "${telemetry_required}" = true ] || [ "${telemetry_required}" = false ]; } || fail "profile telemetryRequired must be boolean"
[[ "${image}" =~ @sha256:[a-f0-9]{64}$ ]] || fail "profile workload image must be digest-pinned"
is_uint "${containers}" || fail "profile container count must be an integer"
is_uint "${containers_per_pod}" || fail "profile containers per Pod must be an integer"
is_uint "${duration}" || fail "profile duration must be an integer"
is_uint "${sample_interval}" || fail "profile sample interval must be an integer"
is_uint "${canary_mib}" || fail "profile canary MiB must be an integer"
is_uint "${canary_control_samples}" || fail "profile canary control sample count must be an integer"
if [ "${containers_per_pod}" -lt 1 ] || [ "${containers_per_pod}" -gt 50 ]; then
  fail "containers per Pod must be 1-50"
fi
[ $((containers % containers_per_pod)) -eq 0 ] || fail "container target must divide exactly by containers per Pod"
if [ "${sample_interval}" -lt 5 ] || [ "${sample_interval}" -gt 300 ]; then
  fail "sample interval must be 5-300 seconds"
fi
case "${profile_mode}" in
  development | qualification) ;;
  *) fail "profile mode must be development or qualification" ;;
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

for command in curl go kubectl; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-density.XXXXXX")
cli=${work_dir}/kubectl-memlens
samples=${work_dir}/samples.jsonl
canary_control=${work_dir}/canary-control.jsonl
canary_observed=${work_dir}/canary-observed.jsonl
api_baseline='{"available":false}'
api_steady_end='{"available":false}'
control_agent_recovery_seconds=0
reliability_summary=${work_dir}/reliability-summary.json
kubectl_args=(--context "${context}")
if [ -n "${kubeconfig}" ]; then
  kubectl_args=(--kubeconfig "${kubeconfig}" "${kubectl_args[@]}")
fi
k() { kubectl "${kubectl_args[@]}" "$@"; }
cli_args=(--context "${context}" --collector-namespace "${collector_namespace}")
if [ -n "${kubeconfig}" ]; then
  cli_args=(--kubeconfig "${kubeconfig}" "${cli_args[@]}")
fi

restore_agent_selector() {
  local patch
  if [ "${agent_blocked}" = true ]; then
    patch=$(jq -cn --argjson value "${agent_node_selector}" '[{op:"replace",path:"/spec/template/spec/nodeSelector",value:$value}]')
    k patch daemonset kube-memlens-agent -n "${collector_namespace}" --type=json -p="${patch}" >/dev/null
    agent_blocked=false
  fi
}

cleanup() {
  local status=$?
  trap - EXIT
  density_stop_port_forward
  density_restore_paused_node || {
    echo "density soak failed to restore a paused kind worker" >&2
    status=1
  }
  restore_agent_selector || {
    echo "density soak failed to restore the agent node selector" >&2
    status=1
  }
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
: > "${canary_control}"
: > "${canary_observed}"
printf '{}\n' > "${reliability_summary}"
k config get-contexts "${context}" -o name | grep -Fxq "${context}" || fail "kubeconfig context not found"
k version -o json >/dev/null
if k get namespace "${namespace}" >/dev/null 2>&1; then
  fail "namespace already exists"
fi
k get deployment kube-memlens-collector -n "${collector_namespace}" >/dev/null || fail "collector deployment not found"
k get daemonset kube-memlens-agent -n "${collector_namespace}" >/dev/null || fail "agent DaemonSet not found"
agent_node_selector=$(k get daemonset kube-memlens-agent -n "${collector_namespace}" -o json | jq -c '.spec.template.spec.nodeSelector')
installed_agent_interval=$(k get daemonset kube-memlens-agent -n "${collector_namespace}" -o json | jq -er '
  [.spec.template.spec.containers[] | select(.name == "agent") | .args[] |
    select(startswith("--interval=")) | split("=")[1]] |
  if length == 1 then .[0] else error("agent interval argument is missing or ambiguous") end')
[ "${installed_agent_interval}" = "${agent_interval}" ] ||
  fail "installed agent interval ${installed_agent_interval} does not match profile ${agent_interval}"
k auth can-i create namespaces | grep -Fxq yes || fail "identity cannot create namespaces"
k auth can-i create deployments.apps -n "${namespace}" | grep -Fxq yes || fail "identity cannot create the workload"

nodes_json=${work_dir}/nodes.json
pods_json=${work_dir}/pods.json
k get nodes -o json > "${nodes_json}"
k get pods -A -o json > "${pods_json}"
kind_nodes=()
observer_state=${work_dir}/kind-telemetry-state.json
observer_args=(--namespace "${collector_namespace}" --state-file "${observer_state}")
kind_observer_available=true
while IFS= read -r node; do
  kind_nodes+=("${node}")
  observer_args+=(--node "${node}")
done < <(jq -r '.items[].metadata.name' "${nodes_json}")
if ! command -v docker >/dev/null 2>&1; then
  kind_observer_available=false
else
  for node in "${kind_nodes[@]}"; do
    if ! docker inspect "${node}" >/dev/null 2>&1; then
      kind_observer_available=false
      break
    fi
  done
fi
[ "${telemetry_required}" != true ] || [ "${kind_observer_available}" = true ] ||
  fail "qualification telemetry requires local kind node containers visible to Docker"
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
if [ "${profile_mode}" = qualification ]; then
  command -v expect >/dev/null 2>&1 || fail "qualification telemetry requires Expect"
  [ "$(density_api_server_counters | jq -r '.available')" = true ] ||
    fail "qualification telemetry requires API server metrics access"
  [ "$(density_collect_agent_metrics "${work_dir}/agent-preflight.txt" | jq -r '.available')" = true ] ||
    fail "qualification telemetry requires agent loopback metric access"
  python3 hack/observe_kind_telemetry.py "${observer_args[@]}" >/dev/null ||
    fail "qualification telemetry requires complete kind runtime observations"
  density_measure_tui_ms >/dev/null || fail "qualification telemetry requires a working TUI probe"
fi

k create namespace "${namespace}" >/dev/null
namespace_created=true
k label namespace "${namespace}" \
  app.kubernetes.io/managed-by=kube-memlens-density-soak \
  pod-security.kubernetes.io/enforce=restricted >/dev/null

k create -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: density-canary
  namespace: ${namespace}
  labels:
    app.kubernetes.io/name: density-canary
    app.kubernetes.io/managed-by: kube-memlens-density-soak
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: density-canary
  template:
    metadata:
      labels:
        app.kubernetes.io/name: density-canary
    spec:
      automountServiceAccountToken: false
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: canary
          image: ${image}
          imagePullPolicy: IfNotPresent
          command: ["/bin/sh", "-c", "exec sleep 86400"]
          resources:
            requests:
              cpu: 10m
              memory: 2Mi
            limits:
              memory: 32Mi
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            capabilities:
              drop: ["ALL"]
EOF
k rollout status deployment/density-canary -n "${namespace}" --timeout="${timeout}"

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
    --field-selector metadata.namespace="${namespace}" \
    --selector app.kubernetes.io/name=density-workers --output json 2>/dev/null | jq 'length'
}

wait_for_mapping() {
  local timeout_seconds=${1:-120} deadline count=0 status state complete
  deadline=$((SECONDS + timeout_seconds))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    count=$(mapped_count || true)
    status=$("${cli}" "${cli_args[@]}" status --output json 2>/dev/null || true)
    state=$(jq -r '.store.reliability.state // empty' <<<"${status}" 2>/dev/null || true)
    complete=$(jq -r '
      .store.reliability.expectedNodes == .store.reliability.freshNodes and
      .store.reliability.staleNodes == 0 and .store.reliability.missingNodes == 0
    ' <<<"${status}" 2>/dev/null || true)
    if [ "${count:-0}" -eq "${containers}" ] && [ "${state}" = ready ] && [ "${complete}" = true ]; then
      return 0
    fi
    sleep 10
  done
  return 1
}

wait_for_mapping 900 || fail "KubeMemLens did not map all ${containers} workload containers within 15 minutes"

blocked_selector=$(jq -c '. + {"kubememlens.io/density-control":"true"}' <<<"${agent_node_selector}")
agent_patch=$(jq -cn --argjson value "${blocked_selector}" '[{op:"replace",path:"/spec/template/spec/nodeSelector",value:$value}]')
k patch daemonset kube-memlens-agent -n "${collector_namespace}" --type=json -p="${agent_patch}" >/dev/null
agent_blocked=true
k wait --for=delete pod -n "${collector_namespace}" -l app.kubernetes.io/name=kube-memlens-agent --timeout=120s >/dev/null
for _ in $(seq 1 "${canary_control_samples}"); do
  density_measure_canary_ms "${canary_mib}" >> "${canary_control}"
done
control_recovery_started=${SECONDS}
restore_agent_selector
k rollout status daemonset/kube-memlens-agent -n "${collector_namespace}" --timeout=120s >/dev/null
wait_for_mapping 120 || fail "mapping did not recover within 120 seconds after the canary control phase"
control_agent_recovery_seconds=$((SECONDS - control_recovery_started))
if [ "${kind_observer_available}" = true ]; then
  rm -f -- "${observer_state}"
  python3 hack/observe_kind_telemetry.py "${observer_args[@]}" >/dev/null
fi

density_capture_operational_baseline

sample_once() {
  local phase=$1 start_ns end_ns query_ms tui_ms doctor_json count status_json
  local agent_json observer_json operational_json node_pressure canary_ms elapsed_seconds
  elapsed_seconds=$((SECONDS - steady_started))
  doctor_json=${work_dir}/doctor-sample.json
  status_json=${work_dir}/status-sample.json
  start_ns=$(density_monotonic_ns)
  "${cli}" "${cli_args[@]}" doctor --output json > "${doctor_json}"
  end_ns=$(density_monotonic_ns)
  query_ms=$(((end_ns - start_ns) / 1000000))
  tui_ms=$(density_measure_tui_ms)
  count=$(mapped_count)
  "${cli}" "${cli_args[@]}" status --output json > "${status_json}"
  agent_json=$(density_collect_agent_metrics "${work_dir}/agent-metrics.txt")
  observer_json='{"available":false}'
  if [ "${kind_observer_available}" = true ]; then
    observer_json=$(python3 hack/observe_kind_telemetry.py "${observer_args[@]}")
  fi
  operational_json=$(density_collect_operational_json)
  node_pressure=$(k get nodes -o json | jq '[.items[].status.conditions[]? | select(.type == "MemoryPressure" and .status == "True")] | length')
  canary_ms=null
  if [ "${phase}" = steady ]; then
    canary_ms=$(density_measure_canary_ms "${canary_mib}")
    echo "${canary_ms}" >> "${canary_observed}"
  fi
  jq -n --arg phase "${phase}" --argjson target "${containers}" --argjson count "${count}" \
    --argjson elapsedSeconds "${elapsed_seconds}" \
    --argjson queryMs "${query_ms}" --argjson tuiMs "${tui_ms}" \
    --argjson unmapped "$(jq '.mapping.unmapped' "${doctor_json}")" \
    --argjson reliability "$(jq '.store.reliability' "${status_json}")" \
    --argjson operational "${operational_json}" --argjson baselineRestarts "${baseline_restarts}" \
    --argjson baselineOOM "${baseline_oom_kills}" --argjson agents "${agent_json}" \
    --argjson components "${observer_json}" --argjson nodePressure "${node_pressure}" \
    --argjson canaryMs "${canary_ms}" \
    '{phase:$phase,elapsedSeconds:$elapsedSeconds,workloadContainers:$count,
      mapping:{expected:$target,mapped:$count,unmapped:$unmapped},
      reliability:{state:$reliability.state,expectedNodes:$reliability.expectedNodes,
        freshNodes:$reliability.freshNodes,staleNodes:$reliability.staleNodes,missingNodes:$reliability.missingNodes},
      operational:{unexplainedRestarts:(([$operational.restarts-$baselineRestarts,0]|max)+$operational.replacements),
        oomKills:([$operational.oomKills-$baselineOOM,0]|max)},
      cliLatencyMilliseconds:$queryMs,tuiLatencyMilliseconds:$tuiMs,agents:$agents,
      componentTelemetry:$components,nodeMemoryPressureNodes:$nodePressure,
      canaryLatencyMilliseconds:$canaryMs}' >> "${samples}"
}

api_baseline=$(density_api_server_counters)
steady_started=${SECONDS}
sample_once steady
steady_deadline=$((steady_started + duration))
next_sample=$((steady_started + sample_interval))
while [ "${next_sample}" -le "${steady_deadline}" ]; do
  sleep_for=$((next_sample - SECONDS))
  [ "${sleep_for}" -gt 0 ] && sleep "${sleep_for}"
  sample_once steady
  next_sample=$((next_sample + sample_interval))
done
api_steady_end=$(density_api_server_counters)

churn_started=${SECONDS}
k rollout restart deployment/density-workers -n "${namespace}" >/dev/null
k rollout status deployment/density-workers -n "${namespace}" --timeout="${timeout}"
wait_for_mapping || fail "mapping did not recover after rolling restart"
churn_recovery_seconds=$((SECONDS - churn_started))
density_record_component_operational
density_accept_workload_rollout
density_capture_operational_baseline
sample_once post-churn

if [ "${profile_mode}" = qualification ]; then
  density_measure_worker_node_recovery
  density_record_all_operational
  density_capture_operational_baseline
  reliability_dir=${work_dir}/reliability
  reliability_kubeconfig=${work_dir}/reliability-kubeconfig
  k config view --minify --flatten > "${reliability_kubeconfig}"
  chmod 600 "${reliability_kubeconfig}"
  RELIABILITY_NAMESPACE="${collector_namespace}" \
    RELIABILITY_KUBECONFIG="${reliability_kubeconfig}" \
    RELIABILITY_ARTIFACT_DIR="${reliability_dir}" \
    RELIABILITY_ACKNOWLEDGE=disrupt-and-restore-kube-memlens-components \
    hack/verify-reliability-kind.sh
  cp "${reliability_dir}/reliability-summary.json" "${reliability_summary}"
  wait_for_mapping 120 || fail "mapping did not recover within 120 seconds after reliability injection"
  density_accept_component_rollout
  density_record_workload_operational
  density_capture_operational_baseline
  sample_once post-recovery
fi

final_doctor=${work_dir}/final-doctor.json
if ! retry_to_file 24 5 "${final_doctor}" \
  "${cli}" "${cli_args[@]}" doctor --strict --output json; then
  jq '{checks, mapping}' "${final_doctor}" \
    | tee "${artifact_dir}/final-doctor-failure.json" >&2
  chmod 600 "${artifact_dir}/final-doctor-failure.json"
  fail "final strict doctor check failed"
fi
outcome=completed
write_summary "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if ! python3 hack/scale-profiles/evaluate.py --profile "${profile_path}" \
  --summary "${artifact_dir}/density-soak-summary.json" \
  --output "${artifact_dir}/density-soak-evaluation.json"; then
  chmod 600 "${artifact_dir}/density-soak-evaluation.json" 2>/dev/null || true
  jq -r '.failures[]?' "${artifact_dir}/density-soak-evaluation.json" >&2 || true
  fail "density soak did not meet the selected profile budgets"
fi
chmod 600 "${artifact_dir}/density-soak-evaluation.json"
outcome=passed
write_summary "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "density soak passed; sanitised evidence: ${artifact_dir}"
