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
fake_node_created=false
fake_node=${release}-reliability-replacement

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
binding_subjects=$("${kc[@]}" get clusterrolebinding "${collector_binding}" -o json | jq -c '.subjects')
agent_node_selector=$("${kc[@]}" get daemonset "${agent_daemonset}" -n "${namespace}" -o json | jq -c '.spec.template.spec.nodeSelector')

restore_binding() {
  if [ "${binding_mutated}" = true ]; then
    patch=$(jq -cn --argjson subjects "${binding_subjects}" '{subjects:$subjects}')
    if ! "${kc[@]}" patch clusterrolebinding "${collector_binding}" --type=merge -p="${patch}" >/dev/null; then
      return 1
    fi
    binding_mutated=false
  fi
}

restore_agent() {
  if [ "${agent_blocked}" = true ]; then
    patch=$(jq -cn --argjson value "${agent_node_selector}" '[{op:"replace",path:"/spec/template/spec/nodeSelector",value:$value}]')
    if ! "${kc[@]}" patch daemonset "${agent_daemonset}" -n "${namespace}" --type=json -p="${patch}" >/dev/null; then
      return 1
    fi
    agent_blocked=false
  fi
}

remove_fake_agent_pods() {
  while IFS= read -r pod; do
    [ -z "${pod}" ] || "${kc[@]}" delete pod "${pod}" -n "${namespace}" --wait=true >/dev/null
  done < <("${kc[@]}" get pods -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-agent -o json | jq -r \
    --arg node "${fake_node}" '.items[] | select(any(.spec.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[]?.matchFields[]?.values[]?; . == $node)) | .metadata.name')
}

cleanup() {
  status=$?
  set +e
  restore_binding || { echo "failed to restore collector delegated-authorisation binding" >&2; status=1; }
  restore_agent || { echo "failed to restore agent DaemonSet node selector" >&2; status=1; }
  if [ "${fake_node_created}" = true ]; then
    "${kc[@]}" delete node "${fake_node}" --ignore-not-found --wait=true >/dev/null || {
      echo "failed to remove reliability test Node" >&2
      status=1
    }
  fi
  remove_fake_agent_pods || { echo "failed to remove reliability test agent Pod" >&2; status=1; }
  trap - EXIT
  exit "${status}"
}
trap cleanup EXIT

cluster_status() {
  "${kc[@]}" get --raw "${api_path}"
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

wait_for_collector_ready() {
  "${kc[@]}" rollout status deployment/"${collector_deployment}" -n "${namespace}" --timeout=90s >/dev/null
  "${kc[@]}" wait --for=condition=Available apiservice/v1alpha1.memory.kubememlens.io --timeout=90s >/dev/null
}

baseline=$(cluster_status)
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

pod=$(collector_pod)
restart_before=$("${kc[@]}" get pod "${pod}" -n "${namespace}" -o jsonpath='{.status.containerStatuses[0].restartCount}')
"${kc[@]}" patch clusterrolebinding "${collector_binding}" --type=merge -p='{"subjects":[]}' >/dev/null
binding_mutated=true
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
"${kc[@]}" patch daemonset "${agent_daemonset}" -n "${namespace}" --type=json -p="${patch}" >/dev/null
agent_blocked=true
"${kc[@]}" wait --for=delete pod -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-agent --timeout=90s >/dev/null
agent_stale_seconds=$(wait_for_state stale 75)
stale=$(cluster_status)
jq -e --argjson baseline "${baseline_containers}" '
  .store.reliability.state == "stale" and
  .store.staleContainers == $baseline and
  .store.totalContainers == $baseline
' <<<"${stale}" >/dev/null

old_collector=$(collector_pod)
"${kc[@]}" delete pod "${old_collector}" -n "${namespace}" --wait=true --timeout=30s >/dev/null
wait_for_collector_ready
rebuilding_seconds=$(wait_for_state rebuilding 30)
rebuilding=$(cluster_status)
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
restore_agent
"${kc[@]}" rollout status daemonset/"${agent_daemonset}" -n "${namespace}" --timeout=90s >/dev/null
wait_for_state ready 90 >/dev/null
agent_recovery_seconds=$((SECONDS - agent_recovery_started))

partial_rollout_started=${SECONDS}
"${kc[@]}" create -f - >/dev/null <<EOF
apiVersion: v1
kind: Node
metadata:
  name: ${fake_node}
  labels:
    kubernetes.io/os: linux
EOF
fake_node_created=true
"${kc[@]}" patch node "${fake_node}" --subresource=status --type=merge \
  -p='{"status":{"conditions":[{"type":"Ready","status":"True","reason":"ReliabilityTest","message":"synthetic selected Node"}]}}' >/dev/null
"${kc[@]}" patch node "${fake_node}" --type=merge -p='{"spec":{"taints":[]}}' >/dev/null
wait_for_state degraded 45 >/dev/null
partial_rollout_seconds=$((SECONDS - partial_rollout_started))
jq -e --argjson expected "$((baseline_expected_nodes + 1))" \
  '.store.reliability.missingNodes == 1 and .store.reliability.expectedNodes == $expected' \
  <<<"$(cluster_status)" >/dev/null
node_recovery_started=${SECONDS}
"${kc[@]}" delete node "${fake_node}" --wait=true --timeout=30s >/dev/null
fake_node_created=false
remove_fake_agent_pods
wait_for_state ready 45 >/dev/null
node_recovery_seconds=$((SECONDS - node_recovery_started))

pod=$(collector_pod)
final_recovery_started=${SECONDS}
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

jq -n \
  --arg baselineGeneration "${baseline_generation}" \
  --arg recoveredGeneration "$(cluster_status | jq -r '.store.reliability.generation')" \
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

cluster_status | jq '{store: {totalContainers: .store.totalContainers, staleContainers: .store.staleContainers,
  reliability: .store.reliability}}' > "${output_dir}/final-cluster-status.json"
echo "reliability failure-injection test passed"
