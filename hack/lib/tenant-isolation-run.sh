#!/usr/bin/env bash

# Adversarial checks that extend the existing disposable tenant-read fixtures.
# The caller owns their lifecycle and supplies kctl, raw_get, fail and paths.
# shellcheck disable=SC2034,SC2154

isolation_release_resources_created=false
isolation_run_started=false
isolation_summary_written=false

tenant_isolation_cleanup() {
  local status=0
  tenant_isolation_restore_controls || status=1
  tenant_isolation_scan_retained_evidence || status=1
  if [ "${isolation_release_resources_created}" = true ]; then
    kctl delete clusterrolebinding kube-memlens-isolation-metrics-reader --ignore-not-found >/dev/null 2>&1 || status=1
    kctl delete rolebinding kube-memlens-isolation-service-proxy -n "${release_namespace}" --ignore-not-found >/dev/null 2>&1 || status=1
    kctl delete role kube-memlens-isolation-service-proxy -n "${release_namespace}" --ignore-not-found >/dev/null 2>&1 || status=1
  fi
  rm -f "${work_dir}/tenant-reader.curl.conf" "${work_dir}/tenant-reader-ca.crt"
  return "${status}"
}

tenant_isolation_create_adversary() {
  isolation_release_resources_created=true
  kctl apply -f - >/dev/null <<YAML
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-memlens-isolation-adversary
  namespace: ${namespace_a}
automountServiceAccountToken: true
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-memlens-isolation-metrics-reader
  namespace: ${namespace_a}
automountServiceAccountToken: false
---
apiVersion: v1
kind: Pod
metadata:
  name: kube-memlens-isolation-adversary
  namespace: ${namespace_a}
spec:
  serviceAccountName: kube-memlens-isolation-adversary
  automountServiceAccountToken: true
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: probe
      image: ${workload_image}
      command: ["/bin/sh", "-c", "exec sleep 600"]
      securityContext:
        runAsUser: 65532
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: {drop: ["ALL"]}
      resources:
        requests: {cpu: 1m, memory: 2Mi}
        limits: {memory: 16Mi}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kube-memlens-isolation-service-proxy
  namespace: ${release_namespace}
rules:
  - apiGroups: [""]
    resources: ["services/proxy"]
    resourceNames: ["kube-memlens-collector"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-memlens-isolation-service-proxy
  namespace: ${release_namespace}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kube-memlens-isolation-service-proxy
subjects:
  - kind: ServiceAccount
    name: ${tenant_reader_service_account}
    namespace: ${namespace_a}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-memlens-isolation-metrics-reader
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-memlens-metrics-reader
subjects:
  - kind: ServiceAccount
    name: kube-memlens-isolation-metrics-reader
    namespace: ${namespace_a}
YAML
  kctl wait --for=condition=Ready pod/kube-memlens-isolation-adversary -n "${namespace_a}" --timeout=60s >/dev/null
}

tenant_isolation_verify_build_identity() {
  local expected_commit=${ISOLATION_EXPECTED_COMMIT:-}
  local expected_image=${ISOLATION_EXPECTED_IMAGE_REFERENCE:-}
  local expected_runtime=${ISOLATION_EXPECTED_RUNTIME_IMAGE_ID:-}
  local expected_local_image=${ISOLATION_EXPECTED_LOCAL_IMAGE_ID:-}
  local expected_chart=${ISOLATION_EXPECTED_CHART_SOURCE_SHA256:-}
  [ -n "${expected_commit}" ] && [ -n "${expected_image}" ] && [ -n "${expected_runtime}" ] &&
    [ -n "${expected_local_image}" ] && [ -n "${expected_chart}" ] || fail "expected build identity is incomplete"
  [ "$(git rev-parse HEAD)" = "${expected_commit}" ] || fail "repository commit differs from expected build commit"
  [ -z "$(git status --porcelain --untracked-files=all)" ] || fail "repository must be clean for retained evidence"

  local collector_pod agent_pod references runtime_ids collector_version agent_version actual_chart
  collector_pod=$(kctl get pods -n "${release_namespace}" -l app.kubernetes.io/name=kube-memlens-collector -o jsonpath='{.items[0].metadata.name}')
  agent_pod=$(kctl get pods -n "${release_namespace}" -l app.kubernetes.io/name=kube-memlens-agent -o jsonpath='{.items[0].metadata.name}')
  references=$(kctl get pods -n "${release_namespace}" -o json | jq -r '.items[] | select(.metadata.labels["app.kubernetes.io/name"] | startswith("kube-memlens-")) | .spec.containers[].image' | sort -u)
  [ "${references}" = "${expected_image}" ] || fail "deployed image reference differs from expected image"
  runtime_ids=$(kctl get pods -n "${release_namespace}" -o json | jq -r '.items[] | select(.metadata.labels["app.kubernetes.io/name"] | startswith("kube-memlens-")) | .status.containerStatuses[].imageID' | sort -u)
  [ "${runtime_ids}" = "${expected_runtime}" ] || fail "runtime image ID differs from expected image"
  collector_version=$(kctl exec "pod/${collector_pod}" -n "${release_namespace}" -- /memlens-collector --version)
  agent_version=$(kctl exec "pod/${agent_pod}" -n "${release_namespace}" -- /memlens-agent --version)
  [[ "${collector_version}" == *"commit=${expected_commit}"* ]] || fail "collector binary commit is not the expected source"
  [[ "${agent_version}" == *"commit=${expected_commit}"* ]] || fail "agent binary commit is not the expected source"

  actual_chart=$(git ls-files charts/kube-memlens | sort | xargs shasum -a 256 | shasum -a 256 | awk '{print $1}')
  [ "${actual_chart}" = "${expected_chart}" ] || fail "chart source hash differs from expected chart"
  helm get metadata "${isolation_release}" --kubeconfig "${kubeconfig}" --kube-context "${context}" -n "${release_namespace}" -o json > "${work_dir}/isolation-helm-metadata.json"
  helm get manifest "${isolation_release}" --kubeconfig "${kubeconfig}" --kube-context "${context}" -n "${release_namespace}" |
    shasum -a 256 | awk '{print $1}' > "${work_dir}/isolation-manifest-hash.txt"
  jq -n --arg commit "${expected_commit}" --arg imageReference "${expected_image}" \
    --arg runtimeImageID "${expected_runtime}" --arg localImageID "${expected_local_image}" \
    --arg chartSourceSHA256 "${expected_chart}" --arg installedManifestSHA256 "$(cat "${work_dir}/isolation-manifest-hash.txt")" \
    --slurpfile helm "${work_dir}/isolation-helm-metadata.json" \
    '{sourceCommit:$commit,repositoryClean:true,imageReference:$imageReference,runtimeImageID:$runtimeImageID,
      localImageID:$localImageID,chartSourceSHA256:$chartSourceSHA256,installedManifestSHA256:$installedManifestSHA256,
      chart:{name:$helm[0].chart,version:$helm[0].version,appVersion:$helm[0].appVersion,revision:$helm[0].revision}}'
}

tenant_isolation_verify_metrics_and_capture() {
  local config=${work_dir}/metrics-reader.kubeconfig
  tenant_read_make_service_account_kubeconfig "${config}" "${namespace_a}" kube-memlens-isolation-metrics-reader
  [ "$(kubectl --kubeconfig "${config}" --context "${context}" auth can-i get metrics.memory.kubememlens.io)" = yes ] || fail "metrics reader cannot get metrics"
  [ "$(kubectl --kubeconfig "${config}" --context "${context}" auth can-i list pods.memory.kubememlens.io -n "${namespace_a}")" = no ] || fail "metrics reader can read tenant Pods"
  kubectl --kubeconfig "${config}" --context "${context}" get --raw "${api}/metrics/current" > "${work_dir}/isolation-authorised-metrics.json"
  local pod_uid container_id
  pod_uid=$(kctl get pod "${pod_b}" -n "${namespace_b}" -o jsonpath='{.metadata.uid}')
  container_id=$(kctl get pod "${pod_b}" -n "${namespace_b}" -o jsonpath='{.status.containerStatuses[0].containerID}')
  container_id=${container_id#*://}
  for file in "${work_dir}/isolation-authorised-metrics.json" "${work_dir}/incident.json"; do
    [ -f "${file}" ] || fail "privacy evidence file is missing"
    for sentinel in "${pod_uid}" "${container_id}" /kubepods; do
      [ -z "${sentinel}" ] || ! grep -Fq "${sentinel}" "${file}" || fail "privacy evidence contains a prohibited runtime value"
    done
  done
  jq -e '[.. | objects | .podUID?, .containerID?, .cgroupPath? | select(. != null and . != "")] | length == 0' \
    "${work_dir}/incident.json" >/dev/null || fail "redacted capture retains a runtime identifier"
  jq -e '[.. | objects | .labels? | select(. != null and length > 0)] | length == 0' \
    "${work_dir}/incident.json" >/dev/null || fail "redacted capture retains a label map"
}

tenant_isolation_scan_retained_evidence() {
  [ -n "${artifact_dir:-}" ] && [ -d "${artifact_dir}" ] || return 0
  ! grep -ERq 'BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY|BEGIN CERTIFICATE|Bearer |containerd://|/kubepods|"token"|"podUID"|"containerID"|"cgroupPath"' "${artifact_dir}"
}

tenant_isolation_direct_checks() {
  local phase=$1
  local collector_ip=$2
  local service_host=kube-memlens-collector.${release_namespace}.svc
  local extension_path=${api}/namespaces/${namespace_b}/pods
  local service_code extension_code health_code legacy_read_code metrics_code legacy_write_code ingest_code
  service_code=$(tenant_isolation_adversary_request "https://${service_host}:443${extension_path}" "${work_dir}/isolation-${phase}-service.txt")
  extension_code=$(tenant_isolation_adversary_request "https://${collector_ip}:8443${extension_path}" "${work_dir}/isolation-${phase}-extension.txt")
  health_code=$(tenant_isolation_adversary_request "http://${collector_ip}:8080/healthz" "${work_dir}/isolation-${phase}-health.txt")
  legacy_read_code=$(tenant_isolation_adversary_request "http://${collector_ip}:8080/api/v1/pods" "${work_dir}/isolation-${phase}-legacy-read.txt")
  metrics_code=$(tenant_isolation_adversary_request "http://${collector_ip}:8080/metrics" "${work_dir}/isolation-${phase}-metrics.txt")
  legacy_write_code=$(tenant_isolation_adversary_request "http://${collector_ip}:8080/api/v1/snapshots" "${work_dir}/isolation-${phase}-legacy-write.txt" POST)
  ingest_code=$(tenant_isolation_adversary_request "http://${collector_ip}:8081/healthz" "${work_dir}/isolation-${phase}-ingest.txt")
  [ "${service_code}" = 401 ] || fail "direct Service request returned ${service_code}, want 401"
  [ "${extension_code}" = 401 ] || fail "direct extension request returned ${extension_code}, want 401"
  [ "${health_code}" = 200 ] || fail "health-only listener returned ${health_code}, want 200"
  [ "${legacy_read_code}" = 404 ] || fail "legacy read listener returned ${legacy_read_code}, want 404"
  [ "${metrics_code}" = 404 ] || fail "legacy metrics listener returned ${metrics_code}, want 404"
  [ "${legacy_write_code}" = 404 ] || fail "legacy write listener returned ${legacy_write_code}, want 404"
  [ "${ingest_code}" = 000 ] || fail "plaintext ingestion listener returned HTTP ${ingest_code}"
  local response
  for response in "${work_dir}/isolation-${phase}-"*.txt; do
    ! grep -Fq "${pod_b}" "${response}" || fail "direct denial exposed tenant B data"
  done
  jq -n --arg service "${service_code}" --arg extension "${extension_code}" --arg health "${health_code}" \
    --arg legacyRead "${legacy_read_code}" --arg metrics "${metrics_code}" --arg legacyWrite "${legacy_write_code}" --arg ingest "${ingest_code}" \
    '{service:($service|tonumber),extension:($extension|tonumber),health:($health|tonumber),
      legacyRead:($legacyRead|tonumber),metrics:($metrics|tonumber),legacyWrite:($legacyWrite|tonumber),
      plaintextIngestionReachable:($ingest != "000")}'
}

tenant_isolation_assert_least_privilege() {
  local collector=system:serviceaccount:${release_namespace}:kube-memlens-collector
  local adversary=system:serviceaccount:${namespace_a}:kube-memlens-isolation-adversary
  local check
  for check in \
    "${collector}|list|pods||" \
    "${collector}|get|nodes||" \
    "${collector}|list|secrets|${release_namespace}|" \
    "${collector}|list|pods.memory.kubememlens.io|${namespace_a}|" \
    "${collector}|get|metrics.memory.kubememlens.io||" \
    "${adversary}|list|pods.memory.kubememlens.io|${namespace_a}|" \
    "${adversary}|create|nodesnapshots.memory.kubememlens.io||"
  do
    IFS='|' read -r identity verb resource target_namespace _ <<<"${check}"
    local args=(auth can-i --as="${identity}" "${verb}" "${resource}")
    if [ -n "${target_namespace}" ]; then args+=(-n "${target_namespace}"); fi
    [ "$(kctl "${args[@]}")" = no ] || fail "least-privilege check allowed ${verb} ${resource}"
  done
  [ "$(kctl auth can-i --as="${collector}" create subjectaccessreviews.authorization.k8s.io)" = yes ] ||
    fail "collector cannot perform delegated authorisation"
}

tenant_isolation_assert_agent_loopback() {
  local index=0 pod_ip code
  while IFS= read -r pod_ip; do
    [ -n "${pod_ip}" ] || continue
    index=$((index + 1))
    code=$(tenant_isolation_adversary_request "http://${pod_ip}:8082/metrics" "${work_dir}/isolation-agent-metrics-${index}.txt")
    [ "${code}" = 000 ] || fail "agent Pod metrics returned HTTP ${code} to a tenant workload"
  done < <(kctl get pods -n "${release_namespace}" -l app.kubernetes.io/name=kube-memlens-agent \
    -o jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}')
  [ "${index}" -gt 0 ] || fail "no agent Pod IP was available for loopback verification"
}

tenant_isolation_scan_agent_logs() {
  local pod_uid container_id pod log index=0
  pod_uid=$(kctl get pod "${pod_b}" -n "${namespace_b}" -o jsonpath='{.metadata.uid}')
  container_id=$(kctl get pod "${pod_b}" -n "${namespace_b}" -o jsonpath='{.status.containerStatuses[0].containerID}')
  while IFS= read -r pod; do
    [ -n "${pod}" ] || continue
    index=$((index + 1))
    log=${work_dir}/isolation-agent-${index}.log
    kctl logs "${pod}" -n "${release_namespace}" --since=10m > "${log}"
    for sentinel in "${pod_uid}" "${container_id}" /kubepods containerd:// podUID cgroupPath; do
      [ -z "${sentinel}" ] || ! grep -Fq "${sentinel}" "${log}" || fail "agent log contains a runtime identifier"
    done
  done < <(kctl get pods -n "${release_namespace}" -l app.kubernetes.io/name=kube-memlens-agent -o name)
  [ "${index}" -gt 0 ] || fail "no agent log was available for privacy verification"
  pod_uid=
  container_id=
}

tenant_isolation_collector_stats() {
  local output=$1
  local pod node restarts summary
  pod=$(kctl get pods -n "${release_namespace}" -l app.kubernetes.io/name=kube-memlens-collector -o jsonpath='{.items[0].metadata.name}')
  node=$(kctl get pod "${pod}" -n "${release_namespace}" -o jsonpath='{.spec.nodeName}')
  restarts=$(kctl get pod "${pod}" -n "${release_namespace}" -o jsonpath='{.status.containerStatuses[0].restartCount}')
  summary=${work_dir}/isolation-kubelet-summary.json
  kctl get --raw "/api/v1/nodes/${node}/proxy/stats/summary" > "${summary}"
  jq --arg namespace "${release_namespace}" --arg pod "${pod}" --argjson restarts "${restarts}" '
    [.pods[] | select(.podRef.namespace==$namespace and .podRef.name==$pod) | .containers[] |
      select(.name=="collector") | {workingSetBytes:(.memory.workingSetBytes // 0),restarts:$restarts}][0]
  ' "${summary}" > "${output}"
  jq -e '.workingSetBytes > 0 and .restarts >= 0' "${output}" >/dev/null || fail "collector resource stats are unavailable"
}

tenant_isolation_run() {
  isolation_run_started=true
  for command in base64 curl shasum; do command -v "${command}" >/dev/null 2>&1 || fail "required isolation command not found: ${command}"; done
  isolation_release=${ISOLATION_RELEASE_NAME:-kube-memlens}
  local build_identity
  build_identity=$(tenant_isolation_verify_build_identity)
  tenant_isolation_prepare_controls
  tenant_isolation_create_adversary
  tenant_isolation_prepare_http "${tenant_a_config}"
  tenant_isolation_assert_least_privilege
  tenant_isolation_assert_agent_loopback
  [ "$(kubectl --kubeconfig "${tenant_a_config}" --context "${context}" auth can-i get services/kube-memlens-collector --subresource=proxy -n "${release_namespace}")" = yes ] ||
    fail "tenant service-proxy test permission is unavailable"

  local collector_ip
  collector_ip=$(kctl get pods -n "${release_namespace}" -l app.kubernetes.io/name=kube-memlens-collector -o jsonpath='{.items[0].status.podIP}')
  [ -n "${collector_ip}" ] || fail "collector Pod IP is unavailable"
  local direct_before direct_without_policy
  direct_before=$(tenant_isolation_direct_checks before "${collector_ip}")

  local adversary_write_code
  adversary_write_code=$(tenant_isolation_adversary_request \
    "https://kubernetes.default.svc${api}/nodesnapshots" "${work_dir}/isolation-adversary-write.txt" POST)
  [ "${adversary_write_code}" = 403 ] || fail "adversary token snapshot request returned ${adversary_write_code}, want 403"

  local service_proxy_path service_proxy_result service_proxy_code service_proxy_time
  service_proxy_path=/api/v1/namespaces/${release_namespace}/services/https:kube-memlens-collector:443/proxy${api}/namespaces/${namespace_b}/pods
  service_proxy_result=$(tenant_isolation_curl "${service_proxy_path}" "${work_dir}/isolation-service-proxy.json")
  read -r service_proxy_code service_proxy_time <<<"${service_proxy_result}"
  case "${service_proxy_code}" in 401|403|500|503) ;; *) fail "Service proxy returned ${service_proxy_code}" ;; esac
  ! grep -Fq "${pod_b}" "${work_dir}/isolation-service-proxy.json" || fail "Service proxy exposed tenant B data"

  tenant_isolation_remove_network_policy
  direct_without_policy=$(tenant_isolation_direct_checks without-policy "${collector_ip}")
  [ "$(jq -S . <<<"${direct_before}")" = "$(jq -S . <<<"${direct_without_policy}")" ] ||
    fail "direct authentication results changed without NetworkPolicy"
  local own_result own_code
  own_result=$(tenant_isolation_curl "${api}/namespaces/${namespace_a}/pods" "${work_dir}/isolation-own-without-policy.json")
  read -r own_code _ <<<"${own_result}"
  [ "${own_code}" = 200 ] || fail "authorised read failed without NetworkPolicy"
  tenant_isolation_restore_network_policy || fail "NetworkPolicy restoration failed"

  tenant_isolation_remove_authorizer_binding
  local no_authorizer_result no_authorizer_code
  no_authorizer_result=$(tenant_isolation_curl "${api}/namespaces/${namespace_a}/pods" "${work_dir}/isolation-no-authorizer.json")
  read -r no_authorizer_code _ <<<"${no_authorizer_result}"
  [ "${no_authorizer_code}" != 200 ] || fail "read succeeded without delegated-authorizer permission"
  tenant_isolation_restore_authorizer_binding || fail "authorizer binding restoration failed"
  for _ in $(seq 1 20); do
    own_result=$(tenant_isolation_curl "${api}/namespaces/${namespace_a}/pods" "${work_dir}/isolation-authorizer-recovered.json")
    read -r own_code _ <<<"${own_result}"
    [ "${own_code}" = 200 ] && break
    sleep 0.5
  done
  [ "${own_code}" = 200 ] || fail "authorised read did not recover after binding restoration"

  local timing history_timing abuse before_stats after_stats
  timing=$(tenant_isolation_interleaved_denials \
    "${api}/namespaces/${namespace_b}/pods/${pod_b}" \
    "${api}/namespaces/${namespace_b}/pods/${missing_pod}" "${pod_b}" "${missing_pod}")
  history_timing=$(tenant_isolation_interleaved_denials \
    "${api}/namespaces/${namespace_b}/pods/${pod_b}/history" \
    "${api}/namespaces/${namespace_b}/pods/${missing_pod}/history" "${pod_b}" "${missing_pod}")
  tenant_isolation_collector_stats "${work_dir}/isolation-before-stats.json"
  before_stats=$(cat "${work_dir}/isolation-before-stats.json")
  abuse=$(tenant_isolation_concurrent_reads "${api}/namespaces/${namespace_a}/pods")
  sleep 2
  tenant_isolation_collector_stats "${work_dir}/isolation-after-stats.json"
  after_stats=$(cat "${work_dir}/isolation-after-stats.json")
  [ "$(jq -r '.restarts' <<<"${after_stats}")" = "$(jq -r '.restarts' <<<"${before_stats}")" ] || fail "collector restarted during abuse"
  local memory_budget=${ISOLATION_COLLECTOR_MEMORY_BUDGET_BYTES:-134217728}
  [ "$(jq -r '.workingSetBytes' <<<"${after_stats}")" -lt "${memory_budget}" ] || fail "collector exceeded its memory budget"
  kctl wait --for=condition=Available apiservice/v1alpha1.memory.kubememlens.io --timeout=30s >/dev/null
  kctl rollout status deployment/kube-memlens-collector -n "${release_namespace}" --timeout=30s >/dev/null

  kctl logs deployment/kube-memlens-collector -n "${release_namespace}" --since=10m > "${work_dir}/isolation-collector.log"
  for sentinel in "${pod_b}" forged-tenant credential-sentinel /kubepods; do
    ! grep -Fq "${sentinel}" "${work_dir}/isolation-collector.log" || fail "collector log contains a sensitive sentinel"
  done
  tenant_isolation_scan_agent_logs
  tenant_isolation_verify_metrics_and_capture
  if [ -f "${work_dir}/incident.json" ]; then
    ! grep -Fq "${namespace_b}" "${work_dir}/incident.json" || fail "tenant capture contains tenant B"
  fi

  local summary=${work_dir}/tenant-isolation-summary.json
  local network_hash server_version repository_commit
  network_hash=${isolation_network_policy_hash}
  server_version=$(kctl version -o json | jq -r '.serverVersion.gitVersion')
  repository_commit=$(git rev-parse HEAD)
  jq -n --argjson direct "${direct_before}" --arg serviceProxyCode "${service_proxy_code}" \
    --arg serviceProxySeconds "${service_proxy_time}" --arg noAuthorizerCode "${no_authorizer_code}" \
    --arg adversaryWriteCode "${adversary_write_code}" --arg networkPolicySpecHash "${network_hash}" \
    --arg kubernetesVersion "${server_version}" --arg repositoryCommit "${repository_commit}" \
    --argjson buildIdentity "${build_identity}" --argjson timing "${timing}" --argjson historyTiming "${history_timing}" \
    --argjson abuse "${abuse}" --argjson before "${before_stats}" --argjson after "${after_stats}" \
    '{schemaVersion:1,outcome:"passed",kubernetesVersion:$kubernetesVersion,repositoryCommit:$repositoryCommit,buildIdentity:$buildIdentity,
      checks:{directBoundary:$direct,adversarySnapshotStatus:($adversaryWriteCode|tonumber),agentMetricsLoopbackOnly:true,
      networkPolicyRemovalEquivalent:true,networkPolicySpecHash:$networkPolicySpecHash,
      serviceProxy:{status:($serviceProxyCode|tonumber),seconds:($serviceProxySeconds|tonumber)},
      delegatedAuthorizerRemoval:{status:($noAuthorizerCode|tonumber),recovered:true},leastPrivilege:true,
      denialEquivalence:{pod:$timing,history:$historyTiming},concurrentAbuse:$abuse},resources:{before:$before,after:$after},
      privacy:{rawResponsesRetained:false,credentialsRetained:false,runtimeIdentifiersIncluded:false},
      caveats:["kind does not prove NetworkPolicy enforcement; it proves authentication is independent of policy presence"]}' > "${summary}"
  chmod 0600 "${summary}"
  if [ -n "${artifact_dir}" ]; then
    mkdir -p "${artifact_dir}"
    cp "${summary}" "${artifact_dir}/tenant-isolation-summary.json"
    chmod 0600 "${artifact_dir}/tenant-isolation-summary.json"
  fi
  cat "${summary}"
  isolation_summary_written=true
}
