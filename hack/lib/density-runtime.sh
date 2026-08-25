#!/usr/bin/env bash

# This library is sourced by soak-live-density.sh, which owns these variables.
# shellcheck disable=SC2034,SC2154

density_capture_operational_baseline() {
  local component_json workload_json component_baseline workload_baseline
  component_json=$(k get pods -n "${collector_namespace}" -o json)
  workload_json=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=density-workers -o json)
  component_pod_uids=$(jq -c '[.items[].metadata.uid] | sort' <<<"${component_json}")
  workload_pod_uids=$(jq -c '[.items[].metadata.uid] | sort' <<<"${workload_json}")
  component_baseline=$(jq '
    {restarts: ([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKills: ([.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)}' <<<"${component_json}")
  workload_baseline=$(jq '
    {restarts: ([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKills: ([.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)}' <<<"${workload_json}")
  baseline_workload_restarts=$(jq '.restarts' <<<"${workload_baseline}")
  baseline_workload_oom_kills=$(jq '.oomKills' <<<"${workload_baseline}")
  baseline_component_restarts=$(jq '.restarts' <<<"${component_baseline}")
  baseline_component_oom_kills=$(jq '.oomKills' <<<"${component_baseline}")
  baseline_restarts=$(jq -n --argjson component "${component_baseline}" --argjson workload "${workload_baseline}" \
    '$component.restarts + $workload.restarts')
  baseline_oom_kills=$(jq -n --argjson component "${component_baseline}" --argjson workload "${workload_baseline}" \
    '$component.oomKills + $workload.oomKills')
}

density_collect_operational_json() {
  local component_json workload_json component_replaced workload_replaced
  component_json=$(k get pods -n "${collector_namespace}" -o json)
  workload_json=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=density-workers -o json)
  component_replaced=$(jq --argjson expected "${component_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' <<<"${component_json}")
  workload_replaced=$(jq --argjson expected "${workload_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' <<<"${workload_json}")
  jq -n --argjson components "${component_json}" --argjson workload "${workload_json}" \
    --argjson componentReplaced "${component_replaced}" --argjson workloadReplaced "${workload_replaced}" '
    {restarts: (([$components.items[].status.containerStatuses[]?.restartCount] | add // 0) +
      ([$workload.items[].status.containerStatuses[]?.restartCount] | add // 0)),
     replacements:($componentReplaced + $workloadReplaced),
     oomKills: (([$components.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length) +
       ([$workload.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length))}'
}

density_accumulate_issues() {
  local issues=$1
  disruption_unexplained_restarts=$((disruption_unexplained_restarts + $(jq '.restarts' <<<"${issues}")))
  disruption_oom_kills=$((disruption_oom_kills + $(jq '.oomKills' <<<"${issues}")))
}

density_record_all_operational() {
  local observed issues
  observed=$(density_collect_operational_json)
  issues=$(jq -n --argjson observed "${observed}" --argjson restarts "${baseline_restarts}" \
    --argjson oomKills "${baseline_oom_kills}" '
    {restarts:(([$observed.restarts-$restarts,0]|max)+$observed.replacements),
     oomKills:([$observed.oomKills-$oomKills,0]|max)}')
  density_accumulate_issues "${issues}"
}

density_record_workload_operational() {
  local workload_json replaced issues
  workload_json=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=density-workers -o json)
  replaced=$(jq --argjson expected "${workload_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' <<<"${workload_json}")
  issues=$(jq -n --argjson workload "${workload_json}" --argjson replaced "${replaced}" \
    --argjson restarts "${baseline_workload_restarts}" --argjson oomKills "${baseline_workload_oom_kills}" '
    {restarts:(([(([$workload.items[].status.containerStatuses[]?.restartCount] | add // 0)-$restarts),0]|max)+$replaced),
     oomKills:([(([$workload.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)-$oomKills),0]|max)}')
  density_accumulate_issues "${issues}"
}

density_record_component_operational() {
  local component_json replaced issues
  component_json=$(k get pods -n "${collector_namespace}" -o json)
  replaced=$(jq --argjson expected "${component_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' <<<"${component_json}")
  issues=$(jq -n --argjson components "${component_json}" --argjson replaced "${replaced}" \
    --argjson restarts "${baseline_component_restarts}" --argjson oomKills "${baseline_component_oom_kills}" '
    {restarts:(([(([$components.items[].status.containerStatuses[]?.restartCount] | add // 0)-$restarts),0]|max)+$replaced),
     oomKills:([(([$components.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)-$oomKills),0]|max)}')
  density_accumulate_issues "${issues}"
}

density_accept_workload_rollout() {
  local workload_json issues
  workload_json=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=density-workers -o json)
  issues=$(jq '
    {restarts: ([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKills: ([.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)}' <<<"${workload_json}")
  density_accumulate_issues "${issues}"
}

density_accept_component_rollout() {
  local component_json issues
  component_json=$(k get pods -n "${collector_namespace}" -o json)
  issues=$(jq '
    {restarts: ([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKills: ([.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)}' <<<"${component_json}")
  density_accumulate_issues "${issues}"
}

density_restore_paused_node() {
  if [ -n "${paused_kind_node:-}" ]; then
    if ! docker unpause "${paused_kind_node}" >/dev/null; then
      return 1
    fi
    paused_kind_node=
  fi
}

density_measure_worker_node_recovery() {
  local deadline condition recovery_started worker_node
  paused_kind_node=$(jq -er '[.items[] |
    select((.metadata.labels["node-role.kubernetes.io/control-plane"] // "") == "")][0].metadata.name' \
    "${nodes_json}") || fail "qualification requires a kind worker Node"
  worker_node=${paused_kind_node}
  docker pause "${paused_kind_node}" >/dev/null
  deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    condition=$(k get node "${paused_kind_node}" -o json | jq -r '
      [.status.conditions[] | select(.type == "Ready") | .status][0] // "Unknown"')
    [ "${condition}" = True ] || break
    sleep 2
  done
  [ "${condition}" != True ] || fail "kind worker did not become unavailable within 120 seconds"
  recovery_started=${SECONDS}
  density_restore_paused_node
  k wait --for=condition=Ready "node/${worker_node}" --timeout=120s >/dev/null
  wait_for_mapping 120 || fail "mapping did not recover within 120 seconds after kind worker interruption"
  worker_node_recovery_seconds=$((SECONDS - recovery_started))
}
