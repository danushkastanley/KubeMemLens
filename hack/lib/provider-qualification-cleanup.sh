#!/usr/bin/env bash

# Evidence finalisation and bounded cleanup for hack/qualify-cluster.sh.
# shellcheck disable=SC2034,SC2154

owned_cluster_resources=(
  clusterrole/kube-memlens-agent
  clusterrole/kube-memlens-namespace-viewer
  clusterrole/kube-memlens-cluster-viewer
  clusterrole/kube-memlens-metrics-reader
  clusterrole/kube-memlens-collector-node-reader
  clusterrole/kube-memlens-cert-bootstrap
  clusterrolebinding/kube-memlens-agent
  clusterrolebinding/kube-memlens-auth-delegator
  clusterrolebinding/kube-memlens-collector-node-reader
  clusterrolebinding/kube-memlens-cert-bootstrap
  apiservice/v1alpha1.memory.kubememlens.io
)

get_owned_resource_json() {
  local resource=$1
  local error_file=${work_dir}/qualification-cleanup-get.err
  if k get "${resource}" -o json 2>"${error_file}"; then
    return 0
  fi
  if grep -Eiq '(\(NotFound\)| not found)' "${error_file}"; then
    return 1
  fi
  return 2
}

owned_resource_delete_uri() {
  local resource=$1
  case "${resource}" in
    clusterrole/*)
      printf '/apis/rbac.authorization.k8s.io/v1/clusterroles/%s' "${resource#*/}"
      ;;
    clusterrolebinding/*)
      printf '/apis/rbac.authorization.k8s.io/v1/clusterrolebindings/%s' "${resource#*/}"
      ;;
    apiservice/*)
      printf '/apis/apiregistration.k8s.io/v1/apiservices/%s' "${resource#*/}"
      ;;
    *) return 1 ;;
  esac
}

delete_owned_cluster_rbac() {
  local delete_uri resource resource_json resource_uid release_name release_namespace status
  local failed=false
  for resource in "${owned_cluster_resources[@]}"; do
    status=0
    resource_json=$(get_owned_resource_json "${resource}") || status=$?
    if [ "${status}" -eq 1 ]; then
      continue
    fi
    if [ "${status}" -ne 0 ]; then
      failed=true
      continue
    fi
    release_name=$(jq -r '.metadata.annotations["meta.helm.sh/release-name"] // ""' <<<"${resource_json}")
    release_namespace=$(jq -r '.metadata.annotations["meta.helm.sh/release-namespace"] // ""' <<<"${resource_json}")
    resource_uid=$(jq -r '.metadata.uid // ""' <<<"${resource_json}")
    if [ "${release_name}" = "${release}" ] && [ "${release_namespace}" = "${namespace}" ]; then
      if [ -z "${resource_uid}" ]; then
        failed=true
      else
        delete_uri=$(owned_resource_delete_uri "${resource}") || {
          failed=true
          continue
        }
        jq -n --arg uid "${resource_uid}" '
          {apiVersion:"v1",kind:"DeleteOptions",preconditions:{uid:$uid},
           propagationPolicy:"Background"}
        ' | k delete --raw "${delete_uri}" -f - >/dev/null 2>&1 || failed=true
      fi
    fi
  done
  [ "${failed}" = false ]
}

cluster_resources_removed() {
  local resource resource_json status
  for resource in "${owned_cluster_resources[@]}"; do
    status=0
    resource_json=$(get_owned_resource_json "${resource}") || status=$?
    if [ "${status}" -eq 0 ]; then
      echo "qualification cleanup error: cluster-scoped resource remains: ${resource}" >&2
      return 1
    fi
    if [ "${status}" -ne 1 ]; then
      echo "qualification cleanup error: could not verify resource absence: ${resource}" >&2
      return 1
    fi
  done
}

delete_owned_namespace() {
  [ -n "${namespace_uid}" ] || return 1
  jq -n --arg uid "${namespace_uid}" '
    {apiVersion:"v1",kind:"DeleteOptions",preconditions:{uid:$uid},
     propagationPolicy:"Background"}
  ' | k delete --raw "/api/v1/namespaces/${namespace}" -f - >/dev/null 2>&1
}

write_summary() {
  local outcome=$1
  jq -n \
    --arg outcome "${outcome}" \
    --arg completedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg imageDigest "${image_digest}" \
    --arg networkPolicy "${network_policy_result:-not-run}" \
    --arg plaintextServiceExposure "${plaintext_service_exposure_result:-not-run}" \
    --argjson installToExplanationSeconds "${first_explanation_seconds}" \
    '{schemaVersion: 1, outcome: $outcome, completedAt: $completedAt,
      image: {repository: "redacted", digest: $imageDigest},
      checks: {networkPolicy: $networkPolicy, plaintextServiceExposure: $plaintextServiceExposure},
      measurements: {installToFirstValidExplanationSeconds: $installToExplanationSeconds},
      caveats: ["Identifiers are deliberately omitted", "This is not a high-density soak"]}' \
    > "${artifact_dir}/qualification-summary.json"
  chmod 600 "${artifact_dir}/qualification-summary.json"
}

write_validated_pending() {
  local outcome=$1
  local failed_check=${2:-}
  local pending_output=${artifact_dir}/provider-qualification.pending.json
  local temporary validation status
  temporary=$(mktemp "${artifact_dir}/.provider-qualification.pending.XXXXXX")
  if ! write_pending_evidence "${outcome}" "${failed_check}" "${temporary}"; then
    rm -f -- "${temporary}"
    return 1
  fi
  status=0
  validation=$(python3 hack/provider-profiles/validate.py --profile "${profile_path}" \
    --evidence "${temporary}" --pending) || status=$?
  if [ "${outcome}" = passed ]; then
    if [ "${status}" -ne 0 ] || ! jq -e '.result == "pass"' <<<"${validation}" >/dev/null; then
      rm -f -- "${temporary}"
      return 1
    fi
  elif [ "${status}" -ne 1 ] || ! jq -e '.result == "fail"' <<<"${validation}" >/dev/null; then
    rm -f -- "${temporary}"
    return 1
  fi
  mv "${temporary}" "${pending_output}"
}

cleanup() {
  local status=$?
  local cleanup_failed=false
  local verify_owned_resources=false
  trap - EXIT
  if [ "${release_installed}" = true ] || [ "${namespace_created}" = true ]; then
    verify_owned_resources=true
  fi
  if [ "${release_installed}" = true ]; then
    if helm uninstall "${release}" "${helm_cluster_args[@]}" \
      --namespace "${namespace}" --wait --timeout 5m >/dev/null 2>&1; then
      release_installed=false
    else
      cleanup_failed=true
      echo "qualification cleanup error: Helm uninstall failed" >&2
    fi
  fi
  if [ "${namespace_created}" = true ]; then
    if ! delete_owned_cluster_rbac; then
      cleanup_failed=true
      echo "qualification cleanup error: owned cluster RBAC deletion failed" >&2
    fi
    if delete_owned_namespace &&
      k wait --for=delete "namespace/${namespace}" --timeout=5m >/dev/null 2>&1; then
      namespace_created=false
    else
      cleanup_failed=true
      echo "qualification cleanup error: namespace deletion did not complete" >&2
    fi
  fi
  if [ "${verify_owned_resources}" = true ] && ! cluster_resources_removed; then
    cleanup_failed=true
  fi
  if [ "${cleanup_failed}" = true ]; then
    current_check=uninstall
  fi
  if [ "${qualification_complete}" != true ]; then
    write_summary failed || echo "qualification warning: failed summary could not be written" >&2
    if [ -f "${artifact_dir}/environment.json" ] &&
      [ -f "${artifact_dir}/provider-inventory.json" ] &&
      [ ! -e "${artifact_dir}/provider-qualification.pending.json" ]; then
      write_validated_pending failed "${current_check}" ||
        echo "qualification warning: failed pending evidence did not validate" >&2
    fi
  fi
  if [[ "${work_dir}" == "${TMPDIR:-/tmp}/kube-memlens-qualification."* ]]; then
    rm -rf -- "${work_dir}"
  fi
  if [ "${cleanup_failed}" = true ]; then
    status=1
  fi
  if [ "${status}" -ne 0 ]; then
    echo "qualification failed; private evidence requires review: ${artifact_dir}" >&2
  fi
  exit "${status}"
}
