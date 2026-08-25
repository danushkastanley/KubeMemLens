#!/usr/bin/env bash

# Reversible control mutations for the disposable kind isolation verifier. The
# caller supplies kctl, fail, work_dir, release_namespace and isolation_release.
# shellcheck disable=SC2154

isolation_network_policy_removed=false
isolation_authorizer_binding_removed=false
isolation_network_policy_hash=
isolation_authorizer_binding_hash=

tenant_isolation_get_control() {
  local resource=$1
  shift
  case "${resource}" in
    networkpolicy/*) kctl get "${resource}" -n "${release_namespace}" "$@" ;;
    *) kctl get "${resource}" "$@" ;;
  esac
}

tenant_isolation_sanitise_object() {
  local resource=$1
  local output=$2
  tenant_isolation_get_control "${resource}" -o json |
    jq 'if has("spec") then
      {apiVersion,kind,metadata:{name:.metadata.name,namespace:.metadata.namespace,
        labels:.metadata.labels,annotations:.metadata.annotations},spec}
    else
      {apiVersion,kind,metadata:{name:.metadata.name,namespace:.metadata.namespace,
        labels:.metadata.labels,annotations:.metadata.annotations},roleRef,subjects}
    end' > "${output}"
}

tenant_isolation_object_hash() {
  local resource=$1
  tenant_isolation_get_control "${resource}" -o json | jq -S 'if has("spec") then .spec else {roleRef,subjects} end' |
    shasum -a 256 | awk '{print $1}'
}

tenant_isolation_assert_helm_owner() {
  local resource=$1
  local owner namespace
  owner=$(tenant_isolation_get_control "${resource}" -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}')
  namespace=$(tenant_isolation_get_control "${resource}" -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-namespace}')
  [ "${owner}" = "${isolation_release}" ] || fail "unexpected Helm owner for ${resource}"
  [ "${namespace}" = "${release_namespace}" ] || fail "unexpected Helm namespace for ${resource}"
}

tenant_isolation_prepare_controls() {
  local network=networkpolicy/kube-memlens-collector
  local binding=clusterrolebinding/kube-memlens-auth-delegator
  tenant_isolation_assert_helm_owner "${network}"
  tenant_isolation_assert_helm_owner "${binding}"
  tenant_isolation_sanitise_object "${network}" "${work_dir}/network-policy.restore.json"
  tenant_isolation_sanitise_object "${binding}" "${work_dir}/authorizer-binding.restore.json"
  isolation_network_policy_hash=$(tenant_isolation_object_hash "${network}")
  isolation_authorizer_binding_hash=$(tenant_isolation_object_hash "${binding}")
}

tenant_isolation_remove_network_policy() {
  isolation_network_policy_removed=true
  kctl delete networkpolicy kube-memlens-collector -n "${release_namespace}" --wait=true >/dev/null
  ! kctl get networkpolicy kube-memlens-collector -n "${release_namespace}" >/dev/null 2>&1 ||
    fail "collector NetworkPolicy remained after controlled deletion"
}

tenant_isolation_restore_network_policy() {
  if [ "${isolation_network_policy_removed}" != true ]; then
    return
  fi
  for _ in $(seq 1 20); do
    local deleting
    deleting=$(kctl get networkpolicy kube-memlens-collector -n "${release_namespace}" \
      -o jsonpath='{.metadata.deletionTimestamp}' 2>/dev/null || true)
    if [ -n "${deleting}" ]; then
      sleep 0.25
      continue
    fi
    if ! kctl get networkpolicy kube-memlens-collector -n "${release_namespace}" >/dev/null 2>&1; then
      kctl create -f "${work_dir}/network-policy.restore.json" >/dev/null 2>&1 || {
        sleep 0.25
        continue
      }
    fi
    if [ "$(tenant_isolation_object_hash networkpolicy/kube-memlens-collector)" = "${isolation_network_policy_hash}" ]; then
      isolation_network_policy_removed=false
      return
    fi
    return 1
  done
  return 1
}

tenant_isolation_remove_authorizer_binding() {
  isolation_authorizer_binding_removed=true
  kctl delete clusterrolebinding kube-memlens-auth-delegator --wait=true >/dev/null
  ! kctl get clusterrolebinding kube-memlens-auth-delegator >/dev/null 2>&1 ||
    fail "authorizer binding remained after controlled deletion"
}

tenant_isolation_restore_authorizer_binding() {
  if [ "${isolation_authorizer_binding_removed}" != true ]; then
    return
  fi
  for _ in $(seq 1 20); do
    local deleting
    deleting=$(kctl get clusterrolebinding kube-memlens-auth-delegator \
      -o jsonpath='{.metadata.deletionTimestamp}' 2>/dev/null || true)
    if [ -n "${deleting}" ]; then
      sleep 0.25
      continue
    fi
    if ! kctl get clusterrolebinding kube-memlens-auth-delegator >/dev/null 2>&1; then
      kctl create -f "${work_dir}/authorizer-binding.restore.json" >/dev/null 2>&1 || {
        sleep 0.25
        continue
      }
    fi
    if [ "$(tenant_isolation_object_hash clusterrolebinding/kube-memlens-auth-delegator)" = "${isolation_authorizer_binding_hash}" ]; then
      isolation_authorizer_binding_removed=false
      return
    fi
    return 1
  done
  return 1
}

tenant_isolation_restore_controls() {
  local status=0
  tenant_isolation_restore_authorizer_binding || status=1
  tenant_isolation_restore_network_policy || status=1
  return "${status}"
}
