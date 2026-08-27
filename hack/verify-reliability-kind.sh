#!/usr/bin/env bash

set -Eeuo pipefail

namespace=${RELIABILITY_NAMESPACE:-kube-memlens}
release=${RELIABILITY_RELEASE:-kube-memlens}
kubeconfig=${RELIABILITY_KUBECONFIG:-${KUBECONFIG:-}}
artifact_dir=${RELIABILITY_ARTIFACT_DIR:-}
acknowledgement=${RELIABILITY_ACKNOWLEDGE:-}
collector_binding=${release}-auth-delegator
collector_deployment=${release}-collector
agent_daemonset=${release}-agent
api_path=/apis/memory.kubememlens.io/v1alpha1/clusterstatus/current
agent_blocked=false
binding_mutated=false
fake_node_touched=false
fake_node=${release}-reliability-replacement
fake_node_owner="$$-${RANDOM}-${RANDOM}"

[ "${acknowledgement}" = disrupt-and-restore-kube-memlens-components ] || {
  echo "set RELIABILITY_ACKNOWLEDGE=disrupt-and-restore-kube-memlens-components" >&2
  exit 1
}
[ -n "${kubeconfig}" ] || {
  echo "RELIABILITY_KUBECONFIG or KUBECONFIG is required" >&2
  exit 1
}
for command in jq kubectl; do
  command -v "${command}" >/dev/null 2>&1 || {
    echo "required command not found: ${command}" >&2
    exit 1
  }
done

kc=(kubectl --kubeconfig "${kubeconfig}")
output_dir=${artifact_dir:-$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-reliability.XXXXXX")}
mkdir -p "${output_dir}"
chmod 700 "${output_dir}"
phase=preflight
phase_file=${output_dir}/reliability-phase.json
failure_summary=${output_dir}/reliability-failure.json
write_phase() {
  phase=$1
  jq -n --arg phase "${phase}" '{phase:$phase}' > "${phase_file}"
  chmod 600 "${phase_file}"
}

restore_binding() {
  local attempt current_subjects patch
  if [ "${binding_mutated}" = true ]; then
    patch=$(jq -cn --argjson subjects "${binding_subjects}" '{subjects:$subjects}')
    for attempt in 1 2 3; do
      if "${kc[@]}" patch clusterrolebinding "${collector_binding}" --request-timeout=5s \
        --type=merge -p="${patch}" >/dev/null &&
        current_subjects=$("${kc[@]}" get clusterrolebinding "${collector_binding}" \
          --request-timeout=5s -o json | jq -c '.subjects') &&
        [ "${current_subjects}" = "${binding_subjects}" ]; then
        binding_mutated=false
        return 0
      fi
      [ "${attempt}" -eq 3 ] || sleep 1
    done
    return 1
  fi
}

restore_agent() {
  local attempt current_selector patch
  if [ "${agent_blocked}" = true ]; then
    patch=$(jq -cn --argjson value "${agent_node_selector}" '[{op:"replace",path:"/spec/template/spec/nodeSelector",value:$value}]')
    for attempt in 1 2 3; do
      if "${kc[@]}" patch daemonset "${agent_daemonset}" -n "${namespace}" \
        --request-timeout=5s --type=json -p="${patch}" >/dev/null &&
        current_selector=$("${kc[@]}" get daemonset "${agent_daemonset}" -n "${namespace}" \
          --request-timeout=5s -o json | jq -c '.spec.template.spec.nodeSelector') &&
        [ "${current_selector}" = "${agent_node_selector}" ]; then
        agent_blocked=false
        return 0
      fi
      [ "${attempt}" -eq 3 ] || sleep 1
    done
    return 1
  fi
}

remove_fake_agent_pods() {
  local pod pod_list
  pod_list=$("${kc[@]}" get pods -n "${namespace}" --request-timeout=5s \
    -l app.kubernetes.io/name=kube-memlens-agent -o json | jq -r \
    --arg node "${fake_node}" '.items[] | select(any(.spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[]?.matchFields[]?.values[]?; . == $node)) | .metadata.name') || return 1
  while IFS= read -r pod; do
    [ -n "${pod}" ] || continue
    "${kc[@]}" delete pod "${pod}" -n "${namespace}" --request-timeout=10s \
      --wait=true --timeout=30s >/dev/null || return 1
  done <<<"${pod_list}"
}

remove_fake_node() {
  local node
  node=$("${kc[@]}" get node "${fake_node}" --request-timeout=5s --ignore-not-found -o json) || return 1
  [ -n "${node}" ] || return 0
  jq -e --arg owner "${fake_node_owner}" \
    '.metadata.labels["kubememlens.io/reliability-owner"] == $owner' <<<"${node}" >/dev/null || {
    echo "refusing to remove an unowned reliability test Node" >&2
    return 1
  }
  "${kc[@]}" delete node "${fake_node}" --request-timeout=10s \
    --wait=true --timeout=30s >/dev/null
}

cleanup() {
  status=$?
  trigger_phase=${phase}
  failure_phase=${phase}
  set +e
  restore_binding || {
    echo "failed to restore collector delegated-authorisation binding" >&2
    status=1
    failure_phase=cleanup
  }
  restore_agent || {
    echo "failed to restore agent DaemonSet node selector" >&2
    status=1
    failure_phase=cleanup
  }
  if [ "${fake_node_touched}" = true ]; then
    fake_node_removed=true
    remove_fake_node || {
      echo "failed to remove reliability test Node" >&2
      status=1
      failure_phase=cleanup
      fake_node_removed=false
    }
    if [ "${fake_node_removed}" = true ]; then
      remove_fake_agent_pods || {
        echo "failed to remove reliability test agent Pod" >&2
        status=1
        failure_phase=cleanup
      }
    fi
  fi
  if [ "${status}" -ne 0 ]; then
    jq -n --arg phase "${failure_phase}" --arg triggerPhase "${trigger_phase}" \
      '{schemaVersion:1,result:"fail",phase:$phase,triggerPhase:$triggerPhase}' > "${failure_summary}"
    chmod 600 "${failure_summary}"
  fi
  trap - EXIT
  exit "${status}"
}
trap cleanup EXIT
write_phase "${phase}"
binding_subjects=$("${kc[@]}" get clusterrolebinding "${collector_binding}" --request-timeout=5s -o json | jq -c '.subjects')
agent_node_selector=$("${kc[@]}" get daemonset "${agent_daemonset}" -n "${namespace}" \
  --request-timeout=5s -o json | jq -c '.spec.template.spec.nodeSelector')

cluster_status() {
  "${kc[@]}" get --request-timeout=3s --raw "${api_path}"
}

cluster_status_retry() {
  local deadline=${1:-0} attempt output
  attempt=1
  while [ "${attempt}" -le 10 ]; do
    if output=$(cluster_status 2>/dev/null) && jq -e '
      (.store.totalContainers | type) == "number" and
      (.store.reliability.state | type) == "string"
    ' <<<"${output}" >/dev/null; then
      if [ "${deadline}" -eq 0 ] || [ "${SECONDS}" -le "${deadline}" ]; then
        printf '%s' "${output}"
        return 0
      fi
    fi
    if [ "${deadline}" -gt 0 ] && [ "${SECONDS}" -ge "${deadline}" ]; then
      break
    fi
    [ "${attempt}" -eq 10 ] || sleep 1
    attempt=$((attempt + 1))
  done
  echo "collector status remained unavailable after bounded retries" >&2
  return 1
}

stable_ready_baseline() {
  local deadline=$((SECONDS + 45)) output signature previous_signature='' stable_samples=0
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    output=$(cluster_status_retry "${deadline}")
    signature=$(jq -er '[.store.reliability.state, .store.totalContainers, .store.reliability.expectedNodes] | @tsv' <<<"${output}")
    if [ "${signature}" = "${previous_signature}" ] && [[ "${signature}" == ready$'\t'* ]]; then
      stable_samples=$((stable_samples + 1))
    else
      stable_samples=1
    fi
    if [ "${stable_samples}" -ge 3 ]; then
      printf '%s' "${output}"
      return 0
    fi
    previous_signature=${signature}
    sleep 3
  done
  echo "collector baseline did not settle across one agent scan interval" >&2
  return 1
}

collector_pod() {
  "${kc[@]}" get pod -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-collector \
    -o jsonpath='{.items[0].metadata.name}'
}

wait_for_state() {
  local expected=$1 timeout=${2:-90} start now state
  start=$(date +%s)
  while true; do
    state=$(cluster_status 2>/dev/null | jq -r '.store.reliability.state // empty' || true)
    if [ "${state}" = "${expected}" ]; then
      now=$(date +%s)
      echo $((now - start))
      return
    fi
    now=$(date +%s)
    if [ $((now - start)) -ge "${timeout}" ]; then
      echo "collector did not reach state ${expected}; last state=${state:-unavailable}" >&2
      return 1
    fi
    sleep 2
  done
}

wait_for_stale_retention() {
  local expected=$1 deadline=$2 output
  while true; do
    output=
    output=$(cluster_status 2>/dev/null) || true
    if [ "${SECONDS}" -le "${deadline}" ] && [ -n "${output}" ] && jq -e --argjson expected "${expected}" '
      .store.reliability.state == "stale" and
      .store.staleContainers == $expected and
      .store.totalContainers == $expected
    ' <<<"${output}" >/dev/null; then
      return
    fi
    if [ "${SECONDS}" -ge "${deadline}" ]; then
      echo "collector did not retain the complete baseline as stale evidence" >&2
      return 1
    fi
    sleep 2
  done
}

wait_for_collector_ready() {
  "${kc[@]}" rollout status deployment/"${collector_deployment}" -n "${namespace}" --timeout=90s >/dev/null
  "${kc[@]}" wait --for=condition=Available apiservice/v1alpha1.memory.kubememlens.io --timeout=90s >/dev/null
}

write_phase baseline
baseline=$(stable_ready_baseline)
baseline_state=$(jq -r '.store.reliability.state' <<<"${baseline}")
[ "${baseline_state}" = ready ] || {
  echo "collector baseline state is ${baseline_state}, want ready" >&2
  exit 1
}
baseline_generation=$(jq -r '.store.reliability.generation' <<<"${baseline}")
baseline_history_reset=$(jq -r '.store.reliability.history.resetAt' <<<"${baseline}")
baseline_containers=$(jq -r '.store.totalContainers' <<<"${baseline}")
baseline_expected_nodes=$(jq -r '.store.reliability.expectedNodes' <<<"${baseline}")
[ "${baseline_containers}" -gt 0 ] || {
  echo "collector baseline has no evidence" >&2
  exit 1
}
[ "${baseline_expected_nodes}" -gt 0 ] || {
  echo "collector baseline has no expected Nodes" >&2
  exit 1
}
existing_fake_node=$("${kc[@]}" get node "${fake_node}" --ignore-not-found -o name)
[ -z "${existing_fake_node}" ] || {
  echo "reliability test Node already exists: ${fake_node}" >&2
  exit 1
}

pod=$(collector_pod)
restart_before=$("${kc[@]}" get pod "${pod}" -n "${namespace}" -o jsonpath='{.status.containerStatuses[0].restartCount}')
write_phase api_outage
binding_mutated=true
"${kc[@]}" patch clusterrolebinding "${collector_binding}" --type=merge -p='{"subjects":[]}' >/dev/null
for _ in $(seq 1 20); do
  ready=$("${kc[@]}" get pod "${pod}" -n "${namespace}" -o jsonpath='{.status.containerStatuses[0].ready}' 2>/dev/null || true)
  [ "${ready}" = false ] && break
  sleep 1
done
[ "${ready:-}" = false ] || {
  echo "collector readiness did not fail after delegated authorisation removal" >&2
  exit 1
}
sleep 3
restart_during_outage=$("${kc[@]}" get pod "${pod}" -n "${namespace}" -o jsonpath='{.status.containerStatuses[0].restartCount}')
[ "${restart_during_outage}" = "${restart_before}" ] || {
  echo "collector liveness restarted the process during an authorisation outage" >&2
  exit 1
}
api_recovery_started=${SECONDS}
restore_binding
wait_for_state ready 90 >/dev/null
api_recovery_seconds=$((SECONDS - api_recovery_started))

blocked_selector=$(jq -c '. + {"kubememlens.io/reliability-blocked":"true"}' <<<"${agent_node_selector}")
patch=$(jq -cn --argjson value "${blocked_selector}" '[{op:"replace",path:"/spec/template/spec/nodeSelector",value:$value}]')
write_phase agent_outage
agent_blocked=true
"${kc[@]}" patch daemonset "${agent_daemonset}" -n "${namespace}" --type=json -p="${patch}" >/dev/null
"${kc[@]}" wait --for=delete pod -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-agent --timeout=90s >/dev/null
agent_outage_started=${SECONDS}
agent_outage_deadline=$((agent_outage_started + 75))
write_phase agent_outage_boundary
outage_boundary=$(cluster_status_retry "${agent_outage_deadline}")
outage_containers=$(jq -er '.store.totalContainers' <<<"${outage_boundary}")
minimum_outage_containers=$((baseline_containers - baseline_expected_nodes))
if [ "${outage_containers}" -le 0 ] ||
  [ "${outage_containers}" -lt "${minimum_outage_containers}" ] ||
  [ "${outage_containers}" -gt "${baseline_containers}" ]; then
  echo "collector outage boundary has invalid retained-container count: baseline=${baseline_containers} expectedNodes=${baseline_expected_nodes} outage=${outage_containers} minimum=${minimum_outage_containers}" >&2
  exit 1
fi
write_phase agent_outage_stale
[ "${SECONDS}" -lt "${agent_outage_deadline}" ] || {
  echo "collector outage boundary exhausted the stale-evidence budget" >&2
  exit 1
}
wait_for_stale_retention "${outage_containers}" "${agent_outage_deadline}"
[ "${SECONDS}" -le "${agent_outage_deadline}" ] || {
  echo "collector stale evidence exceeded the outage budget" >&2
  exit 1
}
agent_stale_seconds=$((SECONDS - agent_outage_started))

old_collector=$(collector_pod)
write_phase collector_restart
"${kc[@]}" delete pod "${old_collector}" -n "${namespace}" --wait=true --timeout=30s >/dev/null
wait_for_collector_ready
rebuilding_seconds=$(wait_for_state rebuilding 30)
rebuilding=$(cluster_status_retry)
new_generation=$(jq -r '.store.reliability.generation' <<<"${rebuilding}")
new_history_reset=$(jq -r '.store.reliability.history.resetAt' <<<"${rebuilding}")
[ "${new_generation}" != "${baseline_generation}" ] || {
  echo "collector generation did not change after restart" >&2
  exit 1
}
[ "${new_history_reset}" != "${baseline_history_reset}" ] || {
  echo "history reset marker did not change after restart" >&2
  exit 1
}
jq -e '.store.totalContainers == 0 and .store.historyPoints == 0' <<<"${rebuilding}" >/dev/null

agent_recovery_started=${SECONDS}
write_phase agent_recovery
restore_agent
"${kc[@]}" rollout status daemonset/"${agent_daemonset}" -n "${namespace}" --timeout=90s >/dev/null
wait_for_state ready 90 >/dev/null
agent_recovery_seconds=$((SECONDS - agent_recovery_started))

partial_rollout_started=${SECONDS}
write_phase partial_rollout
fake_node_touched=true
"${kc[@]}" create -f - >/dev/null <<EOF
apiVersion: v1
kind: Node
metadata:
  name: ${fake_node}
  labels:
    kubernetes.io/os: linux
    kubememlens.io/reliability-owner: "${fake_node_owner}"
EOF
"${kc[@]}" patch node "${fake_node}" --subresource=status --type=merge \
  -p='{"status":{"conditions":[{"type":"Ready","status":"True","reason":"ReliabilityTest","message":"synthetic selected Node"}]}}' >/dev/null
"${kc[@]}" patch node "${fake_node}" --type=merge -p='{"spec":{"taints":[]}}' >/dev/null
wait_for_state degraded 45 >/dev/null
partial_rollout_seconds=$((SECONDS - partial_rollout_started))
jq -e --argjson expected "$((baseline_expected_nodes + 1))" \
  '.store.reliability.missingNodes == 1 and .store.reliability.expectedNodes == $expected' \
  <<<"$(cluster_status_retry)" >/dev/null
node_recovery_started=${SECONDS}
write_phase node_recovery
remove_fake_node
remove_fake_agent_pods
fake_node_touched=false
wait_for_state ready 45 >/dev/null
node_recovery_seconds=$((SECONDS - node_recovery_started))

pod=$(collector_pod)
final_recovery_started=${SECONDS}
write_phase final_collector_restart
shutdown_start=$(date +%s)
"${kc[@]}" delete pod "${pod}" -n "${namespace}" --wait=true --timeout=30s >/dev/null
shutdown_seconds=$(($(date +%s) - shutdown_start))
[ "${shutdown_seconds}" -lt 30 ] || {
  echo "collector shutdown took ${shutdown_seconds}s, termination window is 30s" >&2
  exit 1
}
wait_for_collector_ready
wait_for_state ready 90 >/dev/null
final_recovery_seconds=$((SECONDS - final_recovery_started))
recovered_generation=$(cluster_status_retry | jq -er '.store.reliability.generation')

jq -n \
  --arg baselineGeneration "${baseline_generation}" \
  --arg recoveredGeneration "${recovered_generation}" \
  --argjson apiRecoverySeconds "${api_recovery_seconds}" \
  --argjson agentStaleSeconds "${agent_stale_seconds}" \
  --argjson rebuildingObservedSeconds "${rebuilding_seconds}" \
  --argjson agentRecoverySeconds "${agent_recovery_seconds}" \
  --argjson partialRolloutSeconds "${partial_rollout_seconds}" \
  --argjson nodeRecoverySeconds "${node_recovery_seconds}" \
  --argjson shutdownSeconds "${shutdown_seconds}" \
  --argjson finalRecoverySeconds "${final_recovery_seconds}" \
  '{result:"pass", baselineGeneration:$baselineGeneration, recoveredGeneration:$recoveredGeneration,
    apiRecoverySeconds:$apiRecoverySeconds, agentStaleSeconds:$agentStaleSeconds,
    rebuildingObservedSeconds:$rebuildingObservedSeconds, agentRecoverySeconds:$agentRecoverySeconds,
    partialRolloutSeconds:$partialRolloutSeconds,
    nodeRecoverySeconds:$nodeRecoverySeconds, shutdownSeconds:$shutdownSeconds,
    finalRecoverySeconds:$finalRecoverySeconds}' | tee "${output_dir}/reliability-summary.json"

cluster_status_retry | jq '{store: {totalContainers: .store.totalContainers, staleContainers: .store.staleContainers,
  reliability: .store.reliability}}' > "${output_dir}/final-cluster-status.json"
write_phase completed
echo "reliability failure-injection test passed"
