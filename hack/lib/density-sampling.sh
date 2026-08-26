#!/usr/bin/env bash

# This library is sourced by soak-live-density.sh, which owns these variables.
# shellcheck disable=SC2034,SC2154

fail_sample() {
  local phase=$1 step=$2 failure_file=${artifact_dir}/sample-failure.json
  sampling_failure_probe=${step}
  jq -n --arg phase "${phase}" --arg step "${step}" \
    --argjson elapsedSeconds "$((SECONDS - steady_started))" \
    '{schemaVersion:1,result:"fail",phase:$phase,elapsedSeconds:$elapsedSeconds,step:$step}' \
    > "${failure_file}"
  chmod 600 "${failure_file}"
  fail "${phase} sample ${step} probe failed"
}

sample_agent_telemetry() {
  local expected=$1 result
  result=$(density_collect_agent_metrics "${work_dir}/agent-metrics.txt") || return 1
  jq -e --argjson expected "${expected}" \
    '.available == true and .podCount == $expected' <<<"${result}" >/dev/null || return 1
  printf '%s' "${result}"
}

sample_node_pressure() {
  k get nodes --request-timeout=5s -o json | jq \
    '[.items[].status.conditions[]? | select(.type == "MemoryPressure" and .status == "True")] | length'
}

sample_once() {
  local phase=$1 start_ns end_ns query_ms tui_ms doctor_json count status_json
  local agent_json expected_agent_count observer_json operational_json node_pressure canary_ms elapsed_seconds
  local sample_probe_log
  elapsed_seconds=$((SECONDS - steady_started))
  doctor_json=${work_dir}/doctor-sample.json
  status_json=${work_dir}/status-sample.json
  sample_probe_log=${work_dir}/sample-probe-errors.log
  touch "${sample_probe_log}"
  chmod 600 "${sample_probe_log}"
  start_ns=$(density_monotonic_ns)
  if ! "${cli}" "${cli_args[@]}" doctor --output json > "${doctor_json}"; then
    fail_sample "${phase}" doctor
  fi
  end_ns=$(density_monotonic_ns)
  query_ms=$(((end_ns - start_ns) / 1000000))
  tui_ms=$(density_measure_tui_ms) || {
    fail_sample "${phase}" tui
  }
  count=$(retry_capture 3 1 mapped_count 2>>"${sample_probe_log}") || {
    fail_sample "${phase}" mapping
  }
  if ! retry_to_file 3 1 "${status_json}" \
    "${cli}" "${cli_args[@]}" status --output json 2>>"${sample_probe_log}"; then
    fail_sample "${phase}" status
  fi
  expected_agent_count=$(jq -er '.store.reliability.expectedNodes | select(. > 0)' "${status_json}") ||
    fail_sample "${phase}" agent_telemetry
  agent_json=$(retry_capture 3 1 sample_agent_telemetry "${expected_agent_count}" \
    2>>"${sample_probe_log}") || {
    fail_sample "${phase}" agent_telemetry
  }
  observer_json='{"available":false}'
  if [ "${kind_observer_available}" = true ]; then
    observer_json=$(retry_capture 3 1 python3 hack/observe_kind_telemetry.py \
      "${observer_args[@]}" 2>>"${sample_probe_log}") || {
      fail_sample "${phase}" component_telemetry
    }
  fi
  operational_json=$(retry_capture 3 1 density_collect_operational_json \
    2>>"${sample_probe_log}") || {
    fail_sample "${phase}" operational
  }
  node_pressure=$(retry_capture 3 1 sample_node_pressure 2>>"${sample_probe_log}") || {
    fail_sample "${phase}" node_pressure
  }
  canary_ms=null
  if [ "${phase}" = steady ]; then
    canary_ms=$(density_measure_canary_ms "${canary_mib}") || {
      fail_sample "${phase}" canary
    }
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
      canaryLatencyMilliseconds:$canaryMs}' >> "${samples}" || {
    fail_sample "${phase}" evidence_encoding
  }
}
