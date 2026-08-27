#!/usr/bin/env bash

# Shared live checks for hack/qualify-cluster.sh. The caller supplies k(),
# fail(), cli_args and the qualification state variables.
# shellcheck disable=SC2034,SC2154

wait_for_strict_doctor() {
  local output=$1
  for _ in $(seq 1 36); do
    if "${cli}" "${cli_args[@]}" doctor --strict --output json > "${output}" 2>/dev/null; then
      return 0
    fi
    sleep 5
  done
  return 1
}

assert_live_mount_contract() {
  local daemonset_json=$1

  jq -e '
    .spec.template.spec as $pod |
    $pod.nodeSelector["kubernetes.io/os"] == "linux" and
    $pod.automountServiceAccountToken == false and
    any($pod.volumes[]; .name == "cgroup" and .hostPath.path == "/sys/fs/cgroup" and .hostPath.type == "Directory") and
    any($pod.containers[0].volumeMounts[]; .name == "cgroup" and .mountPath == "/host/sys/fs/cgroup" and .readOnly == true) and
    any($pod.volumes[]; .name == "kubernetes-api-access" and
      any(.projected.sources[]; .serviceAccountToken.expirationSeconds == 3600))
  ' "${daemonset_json}" >/dev/null || fail "agent PodSpec failed the host-access contract"
}

assert_live_security_context() {
  local daemonset_json=$1
  local pods_json=$2

  jq -e '
    .spec.template.spec as $pod |
    $pod.securityContext.runAsNonRoot == true and
    $pod.securityContext.runAsUser == 65532 and
    $pod.securityContext.runAsGroup == 65532 and
    $pod.securityContext.seccompProfile.type == "RuntimeDefault"
  ' "${daemonset_json}" >/dev/null || fail "agent PodSpec failed the Pod-security contract"

  jq -e '
    [.items[].spec.containers[]] | length > 0 and all(.[];
      (.securityContext.privileged // false) == false and
      .securityContext.allowPrivilegeEscalation == false and
      .securityContext.readOnlyRootFilesystem == true and
      any(.securityContext.capabilities.drop[]; . == "ALL"))
  ' "${pods_json}" >/dev/null || fail "a running container failed the security-context contract"
}

assert_live_linux_scheduling() {
  local pods_json=$1
  local nodes_json=$2

  jq -e --slurpfile nodes "${nodes_json}" '
    ($nodes[0].items | map({key: .metadata.name, value: .metadata.labels["kubernetes.io/os"]}) | from_entries) as $os |
    all(.items[]; $os[.spec.nodeName] == "linux")
  ' "${pods_json}" >/dev/null || fail "a KubeMemLens Pod scheduled to a non-Linux node"
}

wait_for_probe_phase() {
  local name=$1
  local expected=$2
  local phase=
  local probe_json=${work_dir}/${name}.json
  for _ in $(seq 1 30); do
    phase=$(k get pod "${name}" -n "${namespace}" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    case "${phase}" in
      Succeeded|Failed) break ;;
    esac
    sleep 2
  done
  [ "${phase}" = "${expected}" ] || fail "NetworkPolicy probe ${name} ended ${phase:-unknown}, expected ${expected}"
  k get pod "${name}" -n "${namespace}" -o json > "${probe_json}"
  if [ "${expected}" = Succeeded ]; then
    jq -e '.status.containerStatuses[0].state.terminated as $term |
      $term.exitCode == 0 and ($term.startedAt | length > 0) and ($term.finishedAt | length > 0)' \
      "${probe_json}" >/dev/null || fail "allowed NetworkPolicy probe did not execute successfully"
  else
    jq -e '.status.containerStatuses[0].state.terminated as $term |
      $term.reason == "Error" and $term.exitCode != 0 and
      ($term.startedAt | length > 0) and ($term.finishedAt | length > 0)' \
      "${probe_json}" >/dev/null || fail "denied NetworkPolicy probe did not execute to a connection failure"
  fi
  jq -e --arg digest "${probe_image##*@}" '
    .status.containerStatuses[0].imageID | contains($digest)
  ' "${probe_json}" >/dev/null || fail "NetworkPolicy probe did not run the digest-pinned image"
}

apply_policy_probe() {
  local name=$1
  local access=$2
  jq -n --arg name "${name}" --arg namespace "${namespace}" --arg image "${probe_image}" \
    --arg access "${access}" --arg node "${policy_node}" '
    {apiVersion:"v1", kind:"Pod", metadata:{name:$name, namespace:$namespace,
      labels:{"app.kubernetes.io/name":"kube-memlens-qualification-policy-client",
              "qualification.kubememlens.io/access":$access}},
     spec:{restartPolicy:"Never", automountServiceAccountToken:false,
       securityContext:{runAsNonRoot:true, runAsUser:65532, runAsGroup:65532,
                        seccompProfile:{type:"RuntimeDefault"}},
       nodeName:$node,
       nodeSelector:{"kubernetes.io/os":"linux"},
       containers:[{name:"probe", image:$image,
         command:["/bin/sh","-c","wget -T 5 -qO- http://qualification-policy-target:8080/"],
         securityContext:{allowPrivilegeEscalation:false, readOnlyRootFilesystem:true,
                          capabilities:{drop:["ALL"]}}}]}}
  ' | k apply -f - >/dev/null
}

assert_live_network_resources() {
  k get networkpolicy kube-memlens-collector -n "${namespace}" -o json |
    jq -e '
      .spec.podSelector.matchLabels["app.kubernetes.io/name"] == "kube-memlens-collector" and
      .spec.policyTypes == ["Ingress"] and
      ([.spec.ingress[].ports[]?.port] | sort) == ["extension","http"]
    ' >/dev/null || fail "installed collector NetworkPolicy differs from the standard profile"
  k get service kube-memlens-collector -n "${namespace}" -o json |
    jq -e '
      (.spec.ports | length) == 1 and .spec.ports[0].port == 443 and
      .spec.ports[0].targetPort == "extension"
    ' >/dev/null || fail "installed collector Service exposes an unexpected port"
}

verify_network_policy_enforcement() {
  assert_live_network_resources
  plaintext_service_exposure_result=closed
  jq -n --arg namespace "${namespace}" --arg image "${probe_image}" '
    {apiVersion:"v1", kind:"Pod", metadata:{name:"qualification-policy-target", namespace:$namespace,
      labels:{"app.kubernetes.io/name":"kube-memlens-qualification-policy-target"}},
     spec:{restartPolicy:"Never", automountServiceAccountToken:false,
       securityContext:{runAsNonRoot:true, runAsUser:65532, runAsGroup:65532, fsGroup:65532,
                        seccompProfile:{type:"RuntimeDefault"}},
       nodeSelector:{"kubernetes.io/os":"linux"},
       containers:[{name:"server", image:$image,
         command:["/bin/sh","-c","echo ok >/www/index.html; exec httpd -f -p 8080 -h /www"],
         ports:[{name:"http",containerPort:8080}],
         securityContext:{allowPrivilegeEscalation:false, readOnlyRootFilesystem:true,
                          capabilities:{drop:["ALL"]}},
         volumeMounts:[{name:"www",mountPath:"/www"}]}],
       volumes:[{name:"www",emptyDir:{}}]}}
  ' | k apply -f - >/dev/null
  jq -n --arg namespace "${namespace}" '
    {apiVersion:"v1", kind:"Service", metadata:{name:"qualification-policy-target",namespace:$namespace},
     spec:{selector:{"app.kubernetes.io/name":"kube-memlens-qualification-policy-target"},
           ports:[{name:"http",port:8080,targetPort:"http"}]}}
  ' | k apply -f - >/dev/null
  jq -n --arg namespace "${namespace}" '
    {apiVersion:"networking.k8s.io/v1",kind:"NetworkPolicy",
     metadata:{name:"qualification-policy-target",namespace:$namespace},
     spec:{podSelector:{matchLabels:{"app.kubernetes.io/name":"kube-memlens-qualification-policy-target"}},
           policyTypes:["Ingress"],ingress:[{from:[{podSelector:{matchLabels:{
             "qualification.kubememlens.io/access":"allowed"}}}],
             ports:[{protocol:"TCP",port:8080}]}]}}
  ' | k apply -f - >/dev/null
  k wait --for=condition=Ready pod/qualification-policy-target -n "${namespace}" --timeout=2m >/dev/null
  policy_node=$(k get pod qualification-policy-target -n "${namespace}" -o jsonpath='{.spec.nodeName}')
  sleep 5
  apply_policy_probe qualification-policy-allowed-before allowed
  wait_for_probe_phase qualification-policy-allowed-before Succeeded
  apply_policy_probe qualification-policy-denied denied
  wait_for_probe_phase qualification-policy-denied Failed
  apply_policy_probe qualification-policy-allowed-after allowed
  wait_for_probe_phase qualification-policy-allowed-after Succeeded
  k delete pod qualification-policy-allowed-before qualification-policy-denied \
    qualification-policy-allowed-after qualification-policy-target \
    -n "${namespace}" --wait=true >/dev/null
  k delete service qualification-policy-target -n "${namespace}" >/dev/null
  k delete networkpolicy qualification-policy-target -n "${namespace}" >/dev/null
}

verify_tui_path() {
  command -v expect >/dev/null 2>&1 || fail "required command not found: expect"
  "${repo_root}/hack/measure-tui-latency.exp" "${cli}" "${kubeconfig}" "${context}" \
    "${namespace}" 80 24 1s >/dev/null
}

verify_component_recovery() {
  local component=$1
  local started=${SECONDS}
  local started_epoch
  local pod old_uid new_uid node previous_capture
  started_epoch=$(date -u +%s)
  case "${component}" in
    agent)
      pod=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-agent -o json |
        jq -r '.items | sort_by(.metadata.name) | .[0].metadata.name')
      old_uid=$(k get pod "${pod}" -n "${namespace}" -o jsonpath='{.metadata.uid}')
      node=$(k get pod "${pod}" -n "${namespace}" -o jsonpath='{.spec.nodeName}')
      wait_for_strict_doctor "${work_dir}/doctor-before-agent-restart.json" ||
        fail "strict doctor was unavailable before agent restart"
      previous_capture=$(jq -r --arg node "${node}" '
        .nodes[] | select(.nodeName == $node) | .capturedAt
      ' "${work_dir}/doctor-before-agent-restart.json")
      [ -n "${previous_capture}" ] || fail "agent restart baseline snapshot was unavailable"
      k delete pod "${pod}" -n "${namespace}" --wait=true --timeout=2m >/dev/null
      k rollout status daemonset/kube-memlens-agent -n "${namespace}" --timeout=3m >/dev/null
      k wait pod -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-agent \
        --field-selector "spec.nodeName=${node}" --for=condition=Ready --timeout=3m >/dev/null
      new_uid=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-agent \
        --field-selector "spec.nodeName=${node}" -o jsonpath='{.items[0].metadata.uid}')
      if [ -z "${new_uid}" ] || [ "${new_uid}" = "${old_uid}" ]; then
        fail "agent restart did not create a replacement Pod on the same node"
      fi
      ;;
    collector)
      pod=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-collector \
        -o jsonpath='{.items[0].metadata.name}')
      old_uid=$(k get pod "${pod}" -n "${namespace}" -o jsonpath='{.metadata.uid}')
      k delete pod "${pod}" -n "${namespace}" --wait=true --timeout=2m >/dev/null
      k rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=3m >/dev/null
      k wait pod -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-collector \
        --for=condition=Ready --timeout=3m >/dev/null
      new_uid=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-collector \
        -o jsonpath='{.items[0].metadata.uid}')
      if [ -z "${new_uid}" ] || [ "${new_uid}" = "${old_uid}" ]; then
        fail "collector restart did not create a replacement Pod"
      fi
      ;;
    *) fail "unknown recovery component: ${component}" ;;
  esac
  wait_for_strict_doctor "${work_dir}/doctor-after-${component}-restart.json" ||
    fail "strict doctor did not recover after ${component} restart"
  case "${component}" in
    agent)
      jq -e --arg node "${node}" --arg previous "${previous_capture}" --argjson started "${started_epoch}" '
        any(.nodes[]; .nodeName == $node and
          .capturedAt != $previous and
          (.capturedAt | sub("\\.[0-9]+Z$";"Z") | fromdateiso8601) >= $started)
      ' "${work_dir}/doctor-after-${component}-restart.json" >/dev/null ||
        fail "agent replacement did not publish a new node snapshot"
      ;;
    collector)
      jq -e --argjson started "${started_epoch}" '
        (.nodes | length > 0) and all(.nodes[];
          (.capturedAt | sub("\\.[0-9]+Z$";"Z") | fromdateiso8601) >= $started)
      ' "${work_dir}/doctor-after-${component}-restart.json" >/dev/null ||
        fail "replacement collector did not rebuild from new snapshots"
      ;;
  esac
  printf '%s' "$((SECONDS - started))"
}

wait_for_node_replacement() {
  local before=${work_dir}/eligible-node-uids-before.txt
  local after=${work_dir}/eligible-node-uids-after.txt
  local recovery_started
  local started_epoch
  local deadline=$((SECONDS + node_replacement_timeout))
  local added removed replacement_node replacement_uid
  k get nodes -l "${qualification_linux_selector}" -o json |
    jq -r '.items[].metadata.uid' | sort > "${before}"
  echo "qualification is waiting for one provider-approved Linux node replacement" >&2
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    k get nodes -l "${qualification_linux_selector}" -o json |
      jq -r '.items[].metadata.uid' | sort > "${after}"
    removed=$(comm -23 "${before}" "${after}" | wc -l | tr -d ' ')
    added=$(comm -13 "${before}" "${after}" | wc -l | tr -d ' ')
    if [ "${removed}" -eq 1 ] && [ "${added}" -eq 1 ]; then
      recovery_started=${SECONDS}
      started_epoch=$(date -u +%s)
      replacement_uid=$(comm -13 "${before}" "${after}")
      replacement_node=$(k get nodes -o json | jq -r --arg uid "${replacement_uid}" '
        .items[] | select(.metadata.uid == $uid) | .metadata.name')
      k rollout status daemonset/kube-memlens-agent -n "${namespace}" --timeout=5m >/dev/null
      for _ in $(seq 1 36); do
        if wait_for_strict_doctor "${work_dir}/doctor-after-node-replacement.json" &&
          jq -e --arg node "${replacement_node}" --argjson started "${started_epoch}" '
            any(.nodes[]; .nodeName == $node and .stale == false and
              (.capturedAt | sub("\\.[0-9]+Z$";"Z") | fromdateiso8601) > $started)
          ' "${work_dir}/doctor-after-node-replacement.json" >/dev/null; then
          printf '%s' "$((SECONDS - recovery_started))"
          return 0
        fi
        sleep 5
      done
      fail "replacement node did not publish a post-detection snapshot"
    fi
    sleep 10
  done
  fail "no Linux node replacement completed within ${node_replacement_timeout}s"
}
