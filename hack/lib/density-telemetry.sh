#!/usr/bin/env bash

# This library is sourced by soak-live-density.sh, which owns these variables.
# shellcheck disable=SC2154

density_monotonic_ns() {
  python3 -c 'import time; print(time.monotonic_ns())'
}

density_measure_tui_ms() {
  local started finished
  command -v expect >/dev/null 2>&1 || {
    echo null
    return
  }
  started=$(density_monotonic_ns)
  if ! expect hack/measure-tui-latency.exp "${cli}" "${kubeconfig}" "${context}" \
    "${collector_namespace}" 80 24 1s >/dev/null; then
    return 1
  fi
  finished=$(density_monotonic_ns)
  echo $(((finished - started) / 1000000))
}

density_stop_port_forward() {
  if [ -n "${port_forward_pid:-}" ]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
    wait "${port_forward_pid}" 2>/dev/null || true
    port_forward_pid=
  fi
}

density_collect_agent_metrics() {
  local output=$1 port=${SOAK_AGENT_METRICS_LOCAL_PORT:-18082}
  local pod pod_count pod_list ready recognised_required_metrics metrics_available=true
  : > "${output}"
  pod_list=$(k get pods -n "${collector_namespace}" --request-timeout=5s \
    -l app.kubernetes.io/name=kube-memlens-agent -o json | jq -r '.items[].metadata.name') || {
    jq -n '{available:false, reason:"agent Pod discovery failed"}'
    return
  }
  pod_count=$(awk 'NF {count++} END {print count + 0}' <<<"${pod_list}")
  if [ "${pod_count}" -eq 0 ]; then
    jq -n '{available:false, reason:"no agent Pods found"}'
    return
  fi
  while IFS= read -r pod; do
    [ -n "${pod}" ] || continue
    density_stop_port_forward
    kubectl "${kubectl_args[@]}" port-forward -n "${collector_namespace}" \
      "pod/${pod}" "${port}:8082" > "${work_dir}/agent-port-forward.log" 2>&1 &
    port_forward_pid=$!
    ready=false
    for _ in $(seq 1 20); do
      if curl -fsS --connect-timeout 1 --max-time 2 \
        "http://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
        ready=true
        break
      fi
      kill -0 "${port_forward_pid}" >/dev/null 2>&1 || break
      sleep 0.1
    done
    if [ "${ready}" != true ] ||
      ! curl -fsS --connect-timeout 1 --max-time 2 \
        "http://127.0.0.1:${port}/metrics" >> "${output}"; then
      metrics_available=false
    fi
    density_stop_port_forward
    [ "${metrics_available}" = true ] || break
  done <<<"${pod_list}"

  recognised_required_metrics=$(awk '
    $1 == "kubememlens_agent_last_scan_duration_seconds" ||
    $1 == "kubememlens_agent_last_scan_containers{kind=\"found\"}" ||
    $1 == "kubememlens_agent_last_scan_containers{kind=\"mapped\"}" ||
    $1 == "kubememlens_agent_last_scan_containers{kind=\"unmapped\"}" ||
    $1 == "kubememlens_agent_snapshot_posts_total{result=\"failure\"}" ||
    $1 == "kubememlens_agent_scans_total{result=\"failure\"}" {count++}
    END {print count + 0}
  ' "${output}")
  [ "${recognised_required_metrics}" -eq "$((pod_count * 6))" ] || metrics_available=false

  if [ "${metrics_available}" != true ]; then
    jq -n '{available:false, reason:"loopback port-forward failed"}'
    return
  fi

  local found mapped unmapped scan_ms post_failures scan_failures
  found=$(awk '$1 == "kubememlens_agent_last_scan_containers{kind=\"found\"}" {sum += $2} END {print sum + 0}' "${output}")
  mapped=$(awk '$1 == "kubememlens_agent_last_scan_containers{kind=\"mapped\"}" {sum += $2} END {print sum + 0}' "${output}")
  unmapped=$(awk '$1 == "kubememlens_agent_last_scan_containers{kind=\"unmapped\"}" {sum += $2} END {print sum + 0}' "${output}")
  scan_ms=$(awk '$1 == "kubememlens_agent_last_scan_duration_seconds" && $2 > max {max = $2} END {print (max + 0) * 1000}' "${output}")
  post_failures=$(awk '$1 == "kubememlens_agent_snapshot_posts_total{result=\"failure\"}" {sum += $2} END {print sum + 0}' "${output}")
  scan_failures=$(awk '$1 == "kubememlens_agent_scans_total{result=\"failure\"}" {sum += $2} END {print sum + 0}' "${output}")
  jq -n --argjson podCount "${pod_count}" \
    --arg found "${found}" --arg mapped "${mapped}" --arg unmapped "${unmapped}" \
    --arg scanMs "${scan_ms}" --arg postFailures "${post_failures}" --arg scanFailures "${scan_failures}" \
    '{available:true, podCount:$podCount,
      found:($found|tonumber), mapped:($mapped|tonumber), unmapped:($unmapped|tonumber),
      scanMilliseconds:($scanMs|tonumber), postFailures:($postFailures|tonumber),
      scanFailures:($scanFailures|tonumber)}'
}

density_api_server_counters() {
  local metrics=${work_dir}/apiserver-metrics.txt
  if ! k get --raw /metrics > "${metrics}" 2>/dev/null; then
    jq -n '{available:false}'
    return
  fi
  awk '
    /^apiserver_request_total\{/ {
      if ($1 ~ /code="429"/) limited += $2
      if ($1 ~ /code="5[0-9][0-9]"/) errors += $2
    }
    END {printf "{\"available\":true,\"errors\":%.0f,\"rateLimited\":%.0f}\n", errors + 0, limited + 0}
  ' "${metrics}"
}

density_measure_canary_ms() {
  local mib=$1 started finished
  started=$(density_monotonic_ns)
  if ! k exec -n "${namespace}" deployment/density-canary -- \
    dd if=/dev/zero of=/dev/null bs=1M count="${mib}" >/dev/null 2>&1; then
    return 1
  fi
  finished=$(density_monotonic_ns)
  echo $(((finished - started) / 1000000))
}
