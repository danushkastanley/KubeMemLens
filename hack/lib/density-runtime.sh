#!/usr/bin/env bash

# This library is sourced by soak-live-density.sh, which owns these variables.
# shellcheck disable=SC2034,SC2154

density_create_staged_workload() {
  local containers_json=${work_dir}/containers.json created_pods=0
  jq -n --arg image "${image}" --argjson count "${containers_per_pod}" '
    [range(0; $count) | {
      name: ("worker-" + tostring), image: $image, imagePullPolicy: "IfNotPresent",
      command: ["/bin/sh", "-c", "exec sleep 86400"],
      resources: {requests: {cpu: "1m", memory: "1Mi"}},
      securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true,
        runAsNonRoot: true, runAsUser: 65532, capabilities: {drop: ["ALL"]}}
    }]' > "${containers_json}"
  jq -n --arg namespace "${namespace}" --slurpfile containers "${containers_json}" '
    {apiVersion: "apps/v1", kind: "Deployment", metadata: {name: "density-workers", namespace: $namespace,
      labels: {"app.kubernetes.io/name": "density-workers", "app.kubernetes.io/managed-by": "kube-memlens-density-soak"}},
     spec: {replicas: 0, progressDeadlineSeconds: 1800,
      strategy: {type: "RollingUpdate", rollingUpdate: {maxSurge: 0, maxUnavailable: "10%"}},
      selector: {matchLabels: {"app.kubernetes.io/name": "density-workers"}},
      template: {metadata: {labels: {"app.kubernetes.io/name": "density-workers"}},
        spec: {automountServiceAccountToken: false, terminationGracePeriodSeconds: 0,
          securityContext: {seccompProfile: {type: "RuntimeDefault"}},
          topologySpreadConstraints: [{maxSkew: 1, topologyKey: "kubernetes.io/hostname",
            whenUnsatisfiable: "ScheduleAnyway",
            labelSelector: {matchLabels: {"app.kubernetes.io/name": "density-workers"}}}],
          containers: $containers[0]}}}}' | k apply -f - >/dev/null
  while [ "${created_pods}" -lt "${pod_count}" ]; do
    created_pods=$((created_pods + creation_batch_pods))
    [ "${created_pods}" -le "${pod_count}" ] || created_pods=${pod_count}
    k scale deployment/density-workers -n "${namespace}" --replicas="${created_pods}" >/dev/null
    k rollout status deployment/density-workers -n "${namespace}" --timeout="${timeout}"
    density_assert_startup_stable
  done
}

mapped_count() {
  "${cli}" "${cli_args[@]}" top containers --all-namespaces \
    --field-selector metadata.namespace="${namespace}" \
    --selector app.kubernetes.io/name=density-workers --output json 2>/dev/null | jq 'length'
}

mapped_workload_pod_uids() {
  local page_file=${work_dir}/mapped-workload-pods.json encoded path
  local uids='[]' page_uids
  local continue_token=
  : > "${page_file}"
  chmod 600 "${page_file}"
  while :; do
    path="/apis/memory.kubememlens.io/v1alpha1/namespaces/${namespace}/pods?summary=true&limit=500"
    if [ -n "${continue_token}" ]; then
      encoded=$(jq -nr --arg value "${continue_token}" '$value | @uri')
      path="${path}&continue=${encoded}"
    fi
    k get --raw "${path}" > "${page_file}" || return 1
    page_uids=$(jq -ce '
      [.items[].snapshot |
        select(.context.labels["app.kubernetes.io/name"] == "density-workers")] as $pods |
      if all($pods[]; .freshness == "fresh")
      then [$pods[].podUID] | sort else empty end
    ' "${page_file}") || return 1
    uids=$(jq -cn --argjson existing "${uids}" --argjson page "${page_uids}" \
      '$existing + $page | unique | sort')
    continue_token=$(jq -er '.metadata.continue // ""' "${page_file}") || return 1
    [ -n "${continue_token}" ] || break
  done
  printf '%s' "${uids}"
}

wait_for_mapping() {
  local timeout_seconds=${1:-120} deadline count=0 status doctor state complete unmapped
  local mapped_uids mapped_uids_ready
  deadline=$((SECONDS + timeout_seconds))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    count=$(mapped_count || true)
    status=$("${cli}" "${cli_args[@]}" status --output json 2>/dev/null || true)
    doctor=$("${cli}" "${cli_args[@]}" doctor --output json 2>/dev/null || true)
    state=$(jq -r '.store.reliability.state // empty' <<<"${status}" 2>/dev/null || true)
    complete=$(jq -r '
      .store.reliability.expectedNodes == .store.reliability.freshNodes and
      .store.reliability.staleNodes == 0 and .store.reliability.missingNodes == 0
    ' <<<"${status}" 2>/dev/null || true)
    unmapped=$(jq -r '.mapping.unmapped // empty' <<<"${doctor}" 2>/dev/null || true)
    mapped_uids_ready=true
    if [ "${required_workload_pod_uids}" != '[]' ]; then
      mapped_uids=$(mapped_workload_pod_uids || true)
      [ "${mapped_uids:-}" = "${required_workload_pod_uids}" ] || mapped_uids_ready=false
    fi
    if [ "${count:-0}" -eq "${containers}" ] && [ "${state}" = ready ] && [ "${complete}" = true ] &&
      [ "${unmapped:-1}" -eq 0 ] && [ "${mapped_uids_ready}" = true ]; then
      return 0
    fi
    sleep 10
  done
  return 1
}

density_prepare_workload_batch() {
  local selection_file=$1 pods_file=${work_dir}/workload-churn-before.json
  k get pods -n "${namespace}" -l app.kubernetes.io/name=density-workers -o json > "${pods_file}"
  : > "${selection_file}"
  chmod 600 "${selection_file}"
  jq -e --argjson expected "${workload_pod_uids}" --argjson podCount "${pod_count}" \
    --argjson batch "${creation_batch_pods}" --argjson containersPerPod "${containers_per_pod}" \
    --argjson target "${containers}" --argjson baselineRestarts "${baseline_workload_restarts}" \
    --argjson baselineOOMKills "${baseline_workload_oom_kills}" '
    .items as $pods |
    ($pods | map(.metadata.uid) | sort) as $uids |
    if (($pods | length) == $podCount and ($uids | unique | length) == $podCount and
      ($pods | map(.metadata.name) | unique | length) == $podCount and $uids == $expected and
      ([$pods[].status.containerStatuses[]?] | length) == $target and
      ([$pods[].status.containerStatuses[]?.restartCount] | add // 0) == $baselineRestarts and
      ([$pods[].status.containerStatuses[]? |
        select(.lastState.terminated.reason == "OOMKilled")] | length) == $baselineOOMKills and
      all($pods[];
        .metadata.deletionTimestamp == null and .status.phase == "Running" and
        (.status.containerStatuses | type) == "array" and
        (.status.containerStatuses | length) == $containersPerPod and
        all(.status.containerStatuses[]; .ready == true and (.state.running | type) == "object")))
    then ($pods | sort_by(.metadata.name) | .[:$batch] | map({name:.metadata.name,uid:.metadata.uid}))
    else error("workload is not at the exact ready baseline") end |
    if length == $batch then . else error("workload churn batch is incomplete") end
  ' "${pods_file}" > "${selection_file}" || fail "could not prepare the exact workload replacement batch"
  workload_replacement_expected_pods=${creation_batch_pods}
  workload_replacement_resident_containers_before=${containers}
}

density_delete_workload_batch() {
  local selection_file=$1 names_file=${work_dir}/workload-churn-names.txt
  local output_file=${work_dir}/workload-churn-delete.txt selected_count name
  local names=()
  : > "${names_file}"
  : > "${output_file}"
  chmod 600 "${names_file}" "${output_file}"
  jq -er '.[] | .name' "${selection_file}" > "${names_file}" ||
    fail "could not read the workload replacement batch"
  while IFS= read -r name; do
    [ -n "${name}" ] || fail "workload replacement batch contained an empty resource"
    names+=("${name}")
  done < "${names_file}"
  selected_count=${#names[@]}
  [ "${selected_count}" -eq "${creation_batch_pods}" ] ||
    fail "workload replacement batch count changed before deletion"
  k delete pod -n "${namespace}" --wait=false "${names[@]}" > "${output_file}" 2>&1 ||
    fail "could not delete the workload replacement batch"
}

density_workload_recovery_state() {
  local pods_file=$1 selection_file=$2
  jq -c --slurpfile selected "${selection_file}" --argjson baseline "${workload_pod_uids}" \
    --argjson podCount "${pod_count}" --argjson batch "${creation_batch_pods}" \
    --argjson containersPerPod "${containers_per_pod}" --argjson target "${containers}" '
    .items as $pods |
    ($selected[0] | map(.uid) | sort) as $selectedUIDs |
    ($pods | map(.metadata.uid) | sort) as $currentUIDs |
    ($baseline - $currentUIDs | unique | sort) as $removedUIDs |
    ($currentUIDs - $baseline | unique | sort) as $addedUIDs |
    {currentUIDs:$currentUIDs,addedUIDs:$addedUIDs,
     removedPods:($removedUIDs | length),addedPods:($addedUIDs | length),
     unexpectedRemovedPods:(($removedUIDs - $selectedUIDs) | length),
     residentContainers:([$pods[].status.containerStatuses[]?] | length),
     complete:(($pods | length) == $podCount and ($currentUIDs | unique | length) == $podCount and
       ($pods | map(.metadata.name) | unique | length) == $podCount and
       $removedUIDs == $selectedUIDs and ($addedUIDs | length) == $batch and
       ([$pods[].status.containerStatuses[]?] | length) == $target and
       all($pods[];
         .metadata.deletionTimestamp == null and .status.phase == "Running" and
         (.status.containerStatuses | type) == "array" and
         (.status.containerStatuses | length) == $containersPerPod and
         all(.status.containerStatuses[]; .ready == true and (.state.running | type) == "object")))}
  ' "${pods_file}"
}

density_wait_for_workload_batch_recovery() {
  local selection_file=$1 deadline=$2 pods_file=${work_dir}/workload-churn-current.json
  local state observed_new_uids='[]' observed_new_count
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if k get pods -n "${namespace}" -l app.kubernetes.io/name=density-workers -o json > "${pods_file}"; then
      state=$(density_workload_recovery_state "${pods_file}" "${selection_file}") ||
        fail "workload replacement state was invalid"
      observed_new_uids=$(jq -cn --argjson previous "${observed_new_uids}" \
        --argjson current "$(jq -c '.addedUIDs' <<<"${state}")" '$previous + $current | unique | sort')
      observed_new_count=$(jq 'length' <<<"${observed_new_uids}")
      [ "$(jq '.unexpectedRemovedPods' <<<"${state}")" -eq 0 ] ||
        fail "an unselected workload Pod was replaced"
      [ "${observed_new_count}" -le "${creation_batch_pods}" ] ||
        fail "more workload Pods were replaced than the selected batch"
      if [ "$(jq -r '.complete' <<<"${state}")" = true ]; then
        accepted_workload_pod_uids=$(jq -c '.currentUIDs' <<<"${state}")
        workload_replacement_observed_pods=$(jq '.addedPods' <<<"${state}")
        workload_replacement_resident_containers_after=$(jq '.residentContainers' <<<"${state}")
        return 0
      fi
    fi
    sleep 2
  done
  return 1
}

density_capture_startup_baseline() {
  local component_file=${work_dir}/startup-component-baseline.json
  k get pods -n "${collector_namespace}" --request-timeout=5s -o json > "${component_file}"
  startup_component_pod_uids=$(jq -c '[.items[].metadata.uid] | sort' "${component_file}")
  startup_component_restarts=$(jq '[.items[].status.containerStatuses[]?.restartCount] | add // 0' \
    "${component_file}")
  startup_component_oom_kills=$(jq '[.items[].status.containerStatuses[]? |
    select(.lastState.terminated.reason == "OOMKilled")] | length' "${component_file}")
}

density_assert_startup_stable() {
  local component_file=${work_dir}/startup-component.json workload_file=${work_dir}/startup-workload.json
  local component_replaced workload_replaced restarts oom_kills node_pressure current_uids
  k get pods -n "${collector_namespace}" --request-timeout=5s -o json > "${component_file}"
  k get pods -n "${namespace}" --request-timeout=5s \
    -l app.kubernetes.io/name=density-workers -o json > "${workload_file}"
  component_replaced=$(jq --argjson expected "${startup_component_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' "${component_file}")
  current_uids=$(jq -c '[.items[].metadata.uid] | sort' "${workload_file}")
  workload_replaced=$(jq -n --argjson expected "${startup_workload_pod_uids}" --argjson current "${current_uids}" \
    'if ($expected - $current | length) == 0 then 0 else 1 end')
  restarts=$(jq -n --slurpfile components "${component_file}" --slurpfile workload "${workload_file}" \
    --argjson baseline "${startup_component_restarts}" \
    --argjson componentReplaced "${component_replaced}" --argjson workloadReplaced "${workload_replaced}" '
    ([(([$components[0].items[].status.containerStatuses[]?.restartCount] | add // 0)-$baseline),0]|max) +
    ([$workload[0].items[].status.containerStatuses[]?.restartCount] | add // 0) +
    $componentReplaced + $workloadReplaced')
  oom_kills=$(jq -n --slurpfile components "${component_file}" --slurpfile workload "${workload_file}" \
    --argjson baseline "${startup_component_oom_kills}" '
    ([(([$components[0].items[].status.containerStatuses[]? |
      select(.lastState.terminated.reason == "OOMKilled")] | length)-$baseline),0]|max) +
    ([$workload[0].items[].status.containerStatuses[]? |
      select(.lastState.terminated.reason == "OOMKilled")] | length)')
  node_pressure=$(k get nodes --request-timeout=5s -o json | jq '
    [.items[].status.conditions[]? | select(.type == "MemoryPressure" and .status == "True")] | length')
  [ "${restarts}" -eq 0 ] || fail "a workload or KubeMemLens container restarted during staged creation"
  [ "${oom_kills}" -eq 0 ] || fail "a workload or KubeMemLens container was OOM-killed during staged creation"
  [ "${node_pressure}" -eq 0 ] || fail "a Node reported MemoryPressure during staged creation"
  startup_workload_pod_uids=${current_uids}
}

density_capture_operational_baseline() {
  local component_file=${work_dir}/operational-component-baseline.json
  local workload_file=${work_dir}/operational-workload-baseline.json component_baseline workload_baseline
  k get pods -n "${collector_namespace}" --request-timeout=5s -o json > "${component_file}"
  k get pods -n "${namespace}" --request-timeout=5s \
    -l app.kubernetes.io/name=density-workers -o json > "${workload_file}"
  component_pod_uids=$(jq -c '[.items[].metadata.uid] | sort' "${component_file}")
  workload_pod_uids=$(jq -c '[.items[].metadata.uid] | sort' "${workload_file}")
  if [ "${accepted_workload_pod_uids}" != '[]' ] &&
    [ "${workload_pod_uids}" != "${accepted_workload_pod_uids}" ]; then
    fail "workload Pod set changed before the operational baseline was refreshed"
  fi
  component_baseline=$(jq '
    {restarts: ([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKills: ([.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)}' "${component_file}")
  workload_baseline=$(jq '
    {restarts: ([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKills: ([.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)}' "${workload_file}")
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
  local component_file=${work_dir}/operational-component.json workload_file=${work_dir}/operational-workload.json
  local component_replaced workload_replaced
  k get pods -n "${collector_namespace}" --request-timeout=5s -o json > "${component_file}"
  k get pods -n "${namespace}" --request-timeout=5s \
    -l app.kubernetes.io/name=density-workers -o json > "${workload_file}"
  component_replaced=$(jq --argjson expected "${component_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' "${component_file}")
  workload_replaced=$(jq --argjson expected "${workload_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' "${workload_file}")
  jq -n --slurpfile components "${component_file}" --slurpfile workload "${workload_file}" \
    --argjson componentReplaced "${component_replaced}" --argjson workloadReplaced "${workload_replaced}" '
    {restarts: (([$components[0].items[].status.containerStatuses[]?.restartCount] | add // 0) +
      ([$workload[0].items[].status.containerStatuses[]?.restartCount] | add // 0)),
     replacements:($componentReplaced + $workloadReplaced),
     oomKills: (([$components[0].items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length) +
       ([$workload[0].items[].status.containerStatuses[]? |
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
  local workload_file=${work_dir}/disruption-workload.json replaced issues
  k get pods -n "${namespace}" -l app.kubernetes.io/name=density-workers -o json > "${workload_file}"
  replaced=$(jq --argjson expected "${workload_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' "${workload_file}")
  issues=$(jq -n --slurpfile workload "${workload_file}" --argjson replaced "${replaced}" \
    --argjson restarts "${baseline_workload_restarts}" --argjson oomKills "${baseline_workload_oom_kills}" '
    {restarts:(([(([$workload[0].items[].status.containerStatuses[]?.restartCount] | add // 0)-$restarts),0]|max)+$replaced),
     oomKills:([(([$workload[0].items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)-$oomKills),0]|max)}')
  density_accumulate_issues "${issues}"
}

density_record_component_operational() {
  local component_file=${work_dir}/disruption-component.json replaced issues
  k get pods -n "${collector_namespace}" -o json > "${component_file}"
  replaced=$(jq --argjson expected "${component_pod_uids}" \
    'if ([.items[].metadata.uid] | sort) == $expected then 0 else 1 end' "${component_file}")
  issues=$(jq -n --slurpfile components "${component_file}" --argjson replaced "${replaced}" \
    --argjson restarts "${baseline_component_restarts}" --argjson oomKills "${baseline_component_oom_kills}" '
    {restarts:(([(([$components[0].items[].status.containerStatuses[]?.restartCount] | add // 0)-$restarts),0]|max)+$replaced),
     oomKills:([(([$components[0].items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)-$oomKills),0]|max)}')
  density_accumulate_issues "${issues}"
}

density_accept_workload_batch() {
  local workload_file=${work_dir}/accepted-workload-batch.json current_uids issues
  k get pods -n "${namespace}" -l app.kubernetes.io/name=density-workers -o json > "${workload_file}"
  current_uids=$(jq -c '[.items[].metadata.uid] | sort' "${workload_file}")
  [ "${current_uids}" = "${accepted_workload_pod_uids}" ] ||
    fail "workload Pod set changed after the accepted replacement batch"
  issues=$(jq '
    {restarts:([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKills:([.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)}' "${workload_file}")
  density_accumulate_issues "${issues}"
}

density_accept_component_rollout() {
  local component_file=${work_dir}/accepted-component-rollout.json issues
  k get pods -n "${collector_namespace}" -o json > "${component_file}"
  issues=$(jq '
    {restarts: ([.items[].status.containerStatuses[]?.restartCount] | add // 0),
     oomKills: ([.items[].status.containerStatuses[]? |
       select(.lastState.terminated.reason == "OOMKilled")] | length)}' "${component_file}")
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

density_select_kind_worker() {
  jq -er '[.items[] | select(
    ((.metadata.labels // {}) | has("node-role.kubernetes.io/control-plane") | not) and
    ((.metadata.labels // {}) | has("node-role.kubernetes.io/master") | not)
  )][0].metadata.name' "$1"
}

density_measure_worker_node_recovery() {
  local deadline condition recovery_started recovery_deadline remaining worker_node
  local unavailable_observed=false
  paused_kind_node=$(density_select_kind_worker "${nodes_json}") ||
    fail "qualification requires a kind worker Node"
  worker_node=${paused_kind_node}
  docker pause "${paused_kind_node}" >/dev/null || fail "could not pause the selected kind worker"
  deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if condition=$(k get node "${paused_kind_node}" --request-timeout=3s -o json 2>/dev/null | jq -r '
      [.status.conditions[] | select(.type == "Ready") | .status][0] // "Unknown"'); then
      if [ "${condition}" != True ]; then
        unavailable_observed=true
        break
      fi
    fi
    sleep 2
  done
  [ "${unavailable_observed}" = true ] || fail "kind worker did not become unavailable within 120 seconds"
  recovery_started=${SECONDS}
  recovery_deadline=$((recovery_started + 120))
  density_restore_paused_node || fail "could not restore the selected kind worker"
  remaining=$((recovery_deadline - SECONDS))
  [ "${remaining}" -gt 0 ] || fail "kind worker restoration exhausted the recovery budget"
  k wait --for=condition=Ready "node/${worker_node}" --timeout="${remaining}s" >/dev/null ||
    fail "kind worker did not become ready within 120 seconds"
  remaining=$((recovery_deadline - SECONDS))
  [ "${remaining}" -gt 0 ] || fail "kind worker readiness exhausted the mapping recovery budget"
  wait_for_mapping "${remaining}" ||
    fail "mapping did not recover within 120 seconds after kind worker interruption"
  worker_node_recovery_seconds=$((SECONDS - recovery_started))
}
