#!/usr/bin/env bash

# Loaded by the disposable tenant-read verifier. Its caller supplies kctl and
# fail so credentials stay inside that verifier's protected temporary directory.

tenant_read_make_service_account_kubeconfig() {
  local output=$1
  local namespace=$2
  local service_account=$3
  local token
  token=$(kctl create token "${service_account}" -n "${namespace}" --duration=10m)
  kctl config view --raw --minify --flatten -o json |
    jq --arg token "${token}" --arg identity "${namespace}:${service_account}" '
      .users = [{name:$identity,user:{token:$token}}] |
      .contexts[0].context.user = $identity
    ' > "${output}"
  chmod 0600 "${output}"
  token=
}

tenant_read_assert_service_account_identity() {
  local config=$1
  local context=$2
  local namespace=$3
  local service_account=$4
  local label=$5
  local actual
  actual=$(kubectl --kubeconfig "${config}" --context "${context}" auth whoami -o json |
    jq -r '.status.userInfo.username')
  [ "${actual}" = "system:serviceaccount:${namespace}:${service_account}" ] ||
    fail "${label} CLI kubeconfig has the wrong effective identity"
}
