#!/usr/bin/env bash

set -Eeuo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
cd "${repo_root}"
# shellcheck source=hack/lib/provider-qualification-live.sh
source "${repo_root}/hack/lib/provider-qualification-live.sh"
# shellcheck source=hack/lib/provider-qualification-evidence.sh
source "${repo_root}/hack/lib/provider-qualification-evidence.sh"
# shellcheck source=hack/lib/provider-qualification-cleanup.sh
source "${repo_root}/hack/lib/provider-qualification-cleanup.sh"

# Qualify an existing, explicitly selected Kubernetes cluster without creating
# infrastructure or publishing artefacts. The namespace must not already exist.

required_acknowledgement=install-test-and-remove-kube-memlens
required_node_acknowledgement=provider-action-approved
context=${QUALIFY_CONTEXT:-}
namespace=${QUALIFY_NAMESPACE:-}
profile_path=${QUALIFY_PROFILE:-}
image_repository=${QUALIFY_IMAGE_REPOSITORY:-}
image_digest=${QUALIFY_IMAGE_DIGEST:-}
chart_archive=${QUALIFY_CHART_ARCHIVE:-}
chart_digest=${QUALIFY_CHART_DIGEST:-}
source_commit=${QUALIFY_SOURCE_COMMIT:-}
qualification_tool_commit=
probe_image=${QUALIFY_PROBE_IMAGE:-}
artifact_dir=${QUALIFY_ARTIFACT_DIR:-}
node_replacement_timeout=${QUALIFY_NODE_REPLACEMENT_TIMEOUT_SECONDS:-1800}
release=kube-memlens-qualification
kubeconfig=${KUBECONFIG:-}
work_dir=
cli=
namespace_created=false
namespace_uid=
release_installed=false
qualification_complete=false
current_check=prerequisites
profile_id=unknown
profile_digest=sha256:$(printf '0%.0s' $(seq 1 64))
chart_version=0.0.0
values_path=
values_digest=sha256:$(printf '0%.0s' $(seq 1 64))
provider_receipt_digest=sha256:$(printf '0%.0s' $(seq 1 64))
evidence_manifest_digest=
provider_name=unknown
node_image=unknown
cni_name=unknown
network_policy_result=not-run
plaintext_service_exposure_result=not-run
install_started_seconds=0
first_explanation_seconds=-1
agent_recovery_seconds=-1
collector_recovery_seconds=-1
node_recovery_seconds=-1
qualification_linux_selector=kubernetes.io/os=linux

usage() {
  cat <<'EOF'
Run KubeMemLens qualification against an existing disposable or authorised cluster.

Required environment:
  QUALIFY_CONTEXT             Exact kubeconfig context to use
  QUALIFY_NAMESPACE           New namespace beginning kube-memlens-qualification-
  QUALIFY_PROFILE             Checked-in provider profile JSON
  QUALIFY_IMAGE_REPOSITORY    Release image repository
  QUALIFY_IMAGE_DIGEST        Exact sha256:<64 lowercase hex> image digest
  QUALIFY_CHART_ARCHIVE       Exact packaged release-candidate chart
  QUALIFY_CHART_DIGEST        SHA-256 digest of that chart package
  QUALIFY_SOURCE_COMMIT       Exact 40-character release-candidate source commit
  QUALIFY_PROBE_IMAGE         Digest-pinned image containing /bin/sh and wget
  QUALIFY_ARTIFACT_DIR        New or empty local evidence directory
  QUALIFY_ACKNOWLEDGE         install-test-and-remove-kube-memlens
  QUALIFY_NODE_REPLACEMENT_ACKNOWLEDGE
                              provider-action-approved

The script installs and removes KubeMemLens and its dedicated namespace. It
refuses existing namespaces, releases, and chart-owned KubeMemLens RBAC.
It waits for an operator-triggered provider node replacement but never creates,
replaces or deletes cloud infrastructure itself.
EOF
}

fail() {
  echo "qualification error: $*" >&2
  exit 1
}

[ "${QUALIFY_ACKNOWLEDGE:-}" = "${required_acknowledgement}" ] || {
  usage >&2
  fail "set QUALIFY_ACKNOWLEDGE=${required_acknowledgement} after reviewing the target"
}
[ "${QUALIFY_NODE_REPLACEMENT_ACKNOWLEDGE:-}" = "${required_node_acknowledgement}" ] ||
  fail "the provider node replacement requires explicit approval"
[ -n "${context}" ] || fail "QUALIFY_CONTEXT is required"
[[ "${namespace}" =~ ^kube-memlens-qualification-[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$ ]] ||
  fail "QUALIFY_NAMESPACE must be a new, lower-case kube-memlens-qualification-* namespace"
[ -f "${profile_path}" ] || fail "QUALIFY_PROFILE must be a checked-in profile file"
profile_path=$(cd "$(dirname "${profile_path}")" && pwd -P)/$(basename "${profile_path}")
case "${profile_path}" in
  "${repo_root}/hack/provider-profiles/"*.json) ;;
  *) fail "QUALIFY_PROFILE must name a top-level checked-in provider profile" ;;
esac
[[ "${image_repository}" =~ ^[A-Za-z0-9._/:_-]+$ ]] || fail "QUALIFY_IMAGE_REPOSITORY is invalid"
[[ "${image_digest}" =~ ^sha256:[a-f0-9]{64}$ ]] || fail "QUALIFY_IMAGE_DIGEST must be an exact lowercase sha256 digest"
[ -f "${chart_archive}" ] || fail "QUALIFY_CHART_ARCHIVE must be an immutable chart package"
[[ "${chart_digest}" =~ ^sha256:[a-f0-9]{64}$ ]] || fail "QUALIFY_CHART_DIGEST must be an exact lowercase sha256 digest"
[[ "${source_commit}" =~ ^[a-f0-9]{40}$ ]] || fail "QUALIFY_SOURCE_COMMIT must be an exact commit"
git cat-file -e "${source_commit}^{commit}" >/dev/null 2>&1 || fail "QUALIFY_SOURCE_COMMIT does not exist"
[[ "${probe_image}" =~ @sha256:[a-f0-9]{64}$ ]] || fail "QUALIFY_PROBE_IMAGE must be digest-pinned"
probe_contract=${repo_root}/hack/provider-probe-image.json
[ -f "${probe_contract}" ] || fail "approved provider probe contract is missing"
[ "$(jq -r '.image' "${probe_contract}")" = "${probe_image}" ] ||
  fail "QUALIFY_PROBE_IMAGE must match the checked-in approved probe image"
[[ "${node_replacement_timeout}" =~ ^[0-9]+$ ]] || fail "node replacement timeout must be an integer"
[ "${node_replacement_timeout}" -ge 300 ] || fail "node replacement timeout must be at least 300 seconds"
[ -n "${artifact_dir}" ] || fail "QUALIFY_ARTIFACT_DIR is required"
if [ "${artifact_dir}" = "/" ] || [ "${artifact_dir}" = "." ]; then
  fail "unsafe QUALIFY_ARTIFACT_DIR"
fi
if [ -d "${artifact_dir}" ] && [ -n "$(find "${artifact_dir}" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
  fail "QUALIFY_ARTIFACT_DIR must be empty"
fi

mkdir -p "${artifact_dir}"
artifact_dir=$(cd "${artifact_dir}" && pwd -P)
if [ "${artifact_dir}" = "/" ] || [ "${artifact_dir}" = "${repo_root}" ]; then
  fail "QUALIFY_ARTIFACT_DIR resolves to an unsafe directory"
fi
chmod 700 "${artifact_dir}"
for output in qualification-summary.json provider-qualification.pending.json provider-inventory.json \
  evidence-manifest.json doctor.json status.json environment.json recovery.json lifecycle.json; do
  [ ! -e "${artifact_dir}/${output}" ] || fail "refusing to overwrite ${artifact_dir}/${output}"
done

for command in expect go helm jq kubectl python3; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done

profile_validation=$(python3 hack/provider-profiles/validate.py --profile "${profile_path}") ||
  fail "provider profile validation failed"
profile_id=$(jq -r '.profile.id' <<<"${profile_validation}")
profile_digest=$(jq -r '.profile.digest' <<<"${profile_validation}")
[ "$(jq -r '.expectedOutcome' "${profile_path}")" = pass ] ||
  fail "use the unsupported-profile runner for this profile"
case "${profile_id}" in
  gke-*) qualification_linux_selector="cloud.google.com/gke-nodepool=${QUALIFY_GKE_NODE_POOL:-}" ;;
  eks-*) qualification_linux_selector="eks.amazonaws.com/nodegroup=${QUALIFY_EKS_NODEGROUP:-}" ;;
  aks-*) qualification_linux_selector="kubernetes.azure.com/agentpool=${QUALIFY_AKS_NODE_POOL:-}" ;;
esac
values_path="${repo_root}/hack/provider-values/${profile_id}.yaml"
[ -f "${values_path}" ] || fail "provider profile has no checked-in values file"
python3 hack/verify_chart_archive.py "${chart_archive}" charts/kube-memlens >/dev/null ||
  fail "chart archive differs from the checked-out source commit"
git diff --quiet "${source_commit}" -- charts/kube-memlens ||
  fail "checked-out chart differs from the release-candidate source commit"
actual_chart_digest=sha256:$(python3 -c 'import hashlib,sys; h=hashlib.sha256(); f=open(sys.argv[1], "rb"); [h.update(chunk) for chunk in iter(lambda: f.read(65536), b"")]; print(h.hexdigest())' "${chart_archive}")
[ "${actual_chart_digest}" = "${chart_digest}" ] || fail "chart archive digest does not match"
chart_version=$(helm show chart "${chart_archive}" | awk '$1 == "version:" {print $2; exit}')
[[ "${chart_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.+][0-9A-Za-z.-]+)?$ ]] ||
  fail "chart package version is invalid"
qualification_tool_commit=$(git rev-parse HEAD)
[[ "${qualification_tool_commit}" =~ ^[a-f0-9]{40}$ ]] || fail "qualification tool commit is invalid"
profile_relative=${profile_path#"${repo_root}/"}
values_relative=${values_path#"${repo_root}/"}
probe_contract_relative=${probe_contract#"${repo_root}/"}
git ls-files --error-unmatch "${profile_relative}" >/dev/null 2>&1 ||
  fail "provider profile is not tracked"
git ls-files --error-unmatch "${values_relative}" >/dev/null 2>&1 ||
  fail "provider values are not tracked"
git ls-files --error-unmatch "${probe_contract_relative}" >/dev/null 2>&1 ||
  fail "provider probe contract is not tracked"
git show "${source_commit}:${profile_relative}" | cmp -s - "${profile_path}" ||
  fail "provider profile differs from the source commit"
git show "${source_commit}:${values_relative}" | cmp -s - "${values_path}" ||
  fail "provider values differ from the source commit"
git show "${source_commit}:${probe_contract_relative}" | cmp -s - "${probe_contract}" ||
  fail "provider probe contract differs from the source commit"
values_digest=sha256:$(python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())' "${values_path}")
repository_changes=$(git status --porcelain --untracked-files=all)
[ -z "${repository_changes}" ] || fail "repository must be clean before qualification"

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-qualification.XXXXXX")
cli=${work_dir}/kubectl-memlens

kubectl_args=(--context "${context}")
if [ -n "${kubeconfig}" ]; then
  kubectl_args=(--kubeconfig "${kubeconfig}" "${kubectl_args[@]}")
fi

k() {
  kubectl "${kubectl_args[@]}" "$@"
}

helm_cluster_args=(--kube-context "${context}")
if [ -n "${kubeconfig}" ]; then
  helm_cluster_args+=(--kubeconfig "${kubeconfig}")
fi

trap cleanup EXIT

k config get-contexts "${context}" -o name | grep -Fxq "${context}" || fail "kubeconfig context not found: ${context}"
collect_provider_receipt || fail "provider-owned inventory did not match the selected profile"
[ "$(jq -r '.qualificationToolCommit' "${artifact_dir}/provider-inventory.json")" = \
  "${qualification_tool_commit}" ] || fail "provider receipt is not bound to the qualification tool commit"
provider_name=$(jq -r '.provider' "${artifact_dir}/provider-inventory.json")
node_image=$(jq -r '.nodeImage' "${artifact_dir}/provider-inventory.json")
cni_name=$(jq -r '.cniName' "${artifact_dir}/provider-inventory.json")
provider_control_plane=$(jq -r '.controlPlaneVersion' "${artifact_dir}/provider-inventory.json")
provider_receipt_digest=$(jq -r '.receiptDigest' "${artifact_dir}/provider-inventory.json")
k version -o json > "${work_dir}/version.json"
if k get namespace "${namespace}" >/dev/null 2>&1; then
  fail "namespace already exists: ${namespace}"
fi
for resource in "${owned_resources[@]}"; do
  status=0
  get_owned_resource_json "${resource}" >/dev/null || status=$?
  if [ "${status}" -eq 0 ]; then
    fail "chart-owned KubeMemLens resource already exists: ${resource}"
  fi
  [ "${status}" -eq 1 ] || fail "could not verify KubeMemLens resource absence: ${resource}"
done
k auth can-i create namespaces | grep -Fxq yes || fail "current identity cannot create namespaces"
k auth can-i create clusterroles.rbac.authorization.k8s.io | grep -Fxq yes ||
  fail "current identity cannot install required cluster RBAC"
for verb in "${required_kube_system_rolebinding_verbs[@]}"; do
  k auth can-i "${verb}" rolebindings.rbac.authorization.k8s.io --namespace kube-system |
    grep -Fxq yes || fail "current identity cannot ${verb} required kube-system RBAC"
done

nodes_json=${work_dir}/nodes.json
k get nodes -o json > "${nodes_json}"
qualified_nodes_json=${work_dir}/qualified-nodes.json
k get nodes -l "${qualification_linux_selector}" -o json > "${qualified_nodes_json}"
linux_nodes=$(jq '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux")] | length' \
  "${qualified_nodes_json}")
all_linux_nodes=$(jq '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux")] | length' \
  "${nodes_json}")
windows_nodes=$(jq '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "windows")] | length' "${nodes_json}")
[ "${linux_nodes}" -gt 0 ] || fail "cluster has no Linux nodes; deep mode is unsupported"
[ "${linux_nodes}" -eq "${all_linux_nodes}" ] ||
  fail "qualification cluster contains Linux nodes outside the selected provider pool"
jq -e '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux") |
  [.status.nodeInfo.architecture,.status.nodeInfo.containerRuntimeVersion,.status.nodeInfo.kernelVersion,
   .status.nodeInfo.osImage,.status.nodeInfo.kubeletVersion]] | unique | length == 1' \
  "${qualified_nodes_json}" >/dev/null || fail "Linux nodes do not form one exact qualification profile"
IFS=$'\t' read -r architecture runtime_version kernel_version os_image kubelet_version < <(jq -r '
  [.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux")][0].status.nodeInfo |
  [.architecture,.containerRuntimeVersion,.kernelVersion,.osImage,.kubeletVersion] | @tsv' "${qualified_nodes_json}")
kubernetes_version=$(jq -r '.serverVersion.gitVersion' "${work_dir}/version.json")
live_control_plane=${kubernetes_version#v}
provider_control_plane=${provider_control_plane#v}
case "${profile_id}" in
  eks-al2023-containerd-amd64)
    [[ "${live_control_plane}" == "${provider_control_plane}."* ]] ||
      fail "live Kubernetes version does not match the EKS provider inventory"
    ;;
  *)
    [ "${live_control_plane}" = "${provider_control_plane}" ] ||
      fail "live Kubernetes version does not match provider inventory"
    ;;
esac
write_environment_evidence unreported false

current_check=helmInstall
namespace_json=$(k create namespace "${namespace}" -o json)
namespace_created=true
namespace_uid=$(jq -er '.metadata.uid | select(type == "string" and length > 0)' <<<"${namespace_json}") ||
  fail "created namespace did not expose a UID"
go build -trimpath \
  -ldflags "-X github.com/danushkastanley/kube-memlens/internal/buildinfo.Commit=${source_commit}" \
  -o "${cli}" ./cmd/kubectl-memlens

helm_args=(
  upgrade --install "${release}" "${chart_archive}"
  "${helm_cluster_args[@]}"
  --namespace "${namespace}"
  --values "${values_path}"
  --set-string namespace.name="${namespace}"
  --set-string image.repository="${image_repository}"
  --set-string image.digest="${image_digest}"
  --set image.pullPolicy=IfNotPresent
  --wait --timeout 5m
)
release_installed=true
install_started_seconds=${SECONDS}
helm "${helm_args[@]}"

current_check=readiness
k rollout status daemonset/kube-memlens-agent -n "${namespace}" --timeout=3m
k rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=3m

daemonset_json=${work_dir}/daemonset.json
k get daemonset kube-memlens-agent -n "${namespace}" -o json > "${daemonset_json}"
desired=$(jq -r '.status.desiredNumberScheduled // 0' "${daemonset_json}")
ready=$(jq -r '.status.numberReady // 0' "${daemonset_json}")
[ "${desired}" -eq "${linux_nodes}" ] ||
  fail "agent targets ${desired}/${linux_nodes} Linux nodes; configure only the required tolerations"
[ "${ready}" -eq "${desired}" ] || fail "only ${ready}/${desired} agent Pods are ready"

pods_json=${work_dir}/pods.json
current_check=mounts
k get pods -n "${namespace}" -o json > "${pods_json}"
assert_live_mount_contract "${daemonset_json}"
current_check=securityContext
assert_live_security_context "${daemonset_json}" "${pods_json}"
current_check=mixedOSScheduling
assert_live_linux_scheduling "${pods_json}" "${nodes_json}"
jq -e --arg digest "${image_digest}" '
  [.items[].status.containerStatuses[]?] | length > 0 and
  all(.[]; (.imageID // "") | contains($digest))
' "${pods_json}" >/dev/null || fail "one or more running containers do not report the requested image digest"
agent_pod=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-agent \
  -o jsonpath='{.items[0].metadata.name}')
collector_pod=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-collector \
  -o jsonpath='{.items[0].metadata.name}')
k exec "${agent_pod}" -n "${namespace}" -- /memlens-agent --version |
  grep -Fq "commit=${source_commit}" || fail "agent image does not report the source commit"
k exec "${collector_pod}" -n "${namespace}" -- /memlens-collector --version |
  grep -Fq "commit=${source_commit}" || fail "collector image does not report the source commit"

cli_args=(--context "${context}" --collector-namespace "${namespace}")
if [ -n "${kubeconfig}" ]; then
  cli_args=(--kubeconfig "${kubeconfig}" "${cli_args[@]}")
fi
doctor_raw=${work_dir}/doctor.json
current_check=collection
wait_for_strict_doctor "${doctor_raw}" || fail "strict doctor did not pass"
jq -e '.checks | length > 0 and all(.[]; .status == "pass")' "${doctor_raw}" >/dev/null ||
  fail "strict doctor contains a non-passing check"
cgroup_version=$(jq -r '[.nodes[].environment.cgroupVersion] | unique |
  if length == 1 then .[0] else "mixed" end' "${doctor_raw}")
write_environment_evidence "${cgroup_version}" false

jq -f hack/provider-profiles/sanitise_doctor.jq \
  "${doctor_raw}" > "${artifact_dir}/doctor.json"
chmod 600 "${artifact_dir}/doctor.json"

current_check=api
"${cli}" "${cli_args[@]}" status --output json \
  | jq '.connection.collector = "redacted" | .connection.description = "redacted"' \
  > "${artifact_dir}/status.json"
"${cli}" "${cli_args[@]}" top pods --all-namespaces --output json > "${work_dir}/top.json"
explanation_raw=${work_dir}/explanation.json
explanation_ok=false
for _ in $(seq 1 36); do
  if "${cli}" "${cli_args[@]}" explain pod "${collector_pod}" -n "${namespace}" --output json \
    > "${explanation_raw}" 2>/dev/null; then
    explanation_ok=true
    break
  fi
  sleep 5
done
[ "${explanation_ok}" = true ] || fail "collector Pod did not become available for explanation"
jq -e '.schemaVersion == 1 and (.finding.severity | length > 0) and
  (.finding.confidence | length > 0) and (.finding.caveats | length > 0) and
  (.finding.evidenceWindow | type == "object")' "${explanation_raw}" >/dev/null ||
  fail "machine explanation is missing required diagnosis metadata"
for sensitive_field in containerID cgroupPath labels podUID; do
  if grep -q "\"${sensitive_field}\"" "${explanation_raw}"; then
    fail "machine explanation contains sensitive field: ${sensitive_field}"
  fi
done
first_explanation_seconds=$((SECONDS - install_started_seconds))
chmod 600 "${artifact_dir}/status.json"

metrics_file=${work_dir}/collector-metrics.txt
k get --raw "/apis/memory.kubememlens.io/v1alpha1/metrics/current" \
  | jq -r '.content' > "${metrics_file}"
grep -q '^kubememlens_collector_ingestion_requests_total' "${metrics_file}" || fail "collector metrics are unavailable"

current_check=tui
verify_tui_path
current_check=networkPolicy
verify_network_policy_enforcement
network_policy_result=enforced
write_environment_evidence "${cgroup_version}" true
python3 hack/provider-profiles/validate_environment.py --profile "${profile_path}" \
  --environment "${artifact_dir}/environment.json" >/dev/null ||
  fail "live environment does not match the selected provider profile"
current_check=agentRestart
agent_recovery_seconds=$(verify_component_recovery agent)
current_check=collectorRestart
collector_recovery_seconds=$(verify_component_recovery collector)
current_check=nodeReplacement
node_recovery_seconds=$(wait_for_node_replacement)
k get nodes -o json > "${work_dir}/nodes-after-replacement.json"
after_linux_nodes=$(jq '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux")] | length' \
  "${work_dir}/nodes-after-replacement.json")
after_windows_nodes=$(jq '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "windows")] | length' \
  "${work_dir}/nodes-after-replacement.json")
after_qualified_nodes=${work_dir}/qualified-nodes-after-replacement.json
k get nodes -l "${qualification_linux_selector}" -o json > "${after_qualified_nodes}"
after_qualified_linux_nodes=$(jq \
  '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux")] | length' \
  "${after_qualified_nodes}")
if [ "${after_linux_nodes}" -ne "${linux_nodes}" ] ||
  [ "${after_qualified_linux_nodes}" -ne "${linux_nodes}" ] ||
  [ "${after_windows_nodes}" -ne "${windows_nodes}" ]; then
  fail "node replacement changed the qualified Linux/Windows pool shape"
fi
jq -e '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux") |
  [.status.nodeInfo.architecture,.status.nodeInfo.containerRuntimeVersion,.status.nodeInfo.kernelVersion,
   .status.nodeInfo.osImage,.status.nodeInfo.kubeletVersion]] | unique | length == 1' \
  "${after_qualified_nodes}" >/dev/null ||
  fail "replacement Linux nodes do not form one exact qualification profile"
after_profile=$(jq -r '
  [.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux")][0].status.nodeInfo |
  [.architecture,.containerRuntimeVersion,.kernelVersion,.osImage,.kubeletVersion] | @tsv' \
  "${after_qualified_nodes}")
expected_profile=$(printf '%s\t%s\t%s\t%s\t%s' \
  "${architecture}" "${runtime_version}" "${kernel_version}" "${os_image}" "${kubelet_version}")
[ "${after_profile}" = "${expected_profile}" ] ||
  fail "node replacement changed the qualified runtime profile"
collect_provider_receipt || fail "provider inventory refresh failed after node replacement"
refreshed_provider_name=$(jq -r '.provider' "${artifact_dir}/provider-inventory.json")
refreshed_node_image=$(jq -r '.nodeImage' "${artifact_dir}/provider-inventory.json")
refreshed_cni_name=$(jq -r '.cniName' "${artifact_dir}/provider-inventory.json")
refreshed_control_plane=$(jq -r '.controlPlaneVersion' "${artifact_dir}/provider-inventory.json")
if [ "${refreshed_provider_name}" != "${provider_name}" ] ||
  [ "${refreshed_node_image}" != "${node_image}" ] ||
  [ "${refreshed_cni_name}" != "${cni_name}" ]; then
  fail "provider inventory changed after node replacement"
fi
refreshed_control_plane=${refreshed_control_plane#v}
case "${profile_id}" in
  eks-al2023-containerd-amd64)
    [[ "${live_control_plane}" == "${refreshed_control_plane}."* ]] ||
      fail "refreshed EKS inventory no longer matches the live control plane"
    ;;
  *)
    [ "${live_control_plane}" = "${refreshed_control_plane}" ] ||
      fail "refreshed provider inventory no longer matches the live control plane"
    ;;
esac
provider_receipt_digest=$(jq -r '.receiptDigest' "${artifact_dir}/provider-inventory.json")

jq -n --argjson installToFirstValidExplanationSeconds "${first_explanation_seconds}" \
  --argjson agentRestartRecoverySeconds "${agent_recovery_seconds}" \
  --argjson collectorRestartRecoverySeconds "${collector_recovery_seconds}" \
  --argjson nodeReplacementRecoverySeconds "${node_recovery_seconds}" '
  {schemaVersion:1,installToFirstValidExplanationSeconds:$installToFirstValidExplanationSeconds,
   agentRestartRecoverySeconds:$agentRestartRecoverySeconds,
   collectorRestartRecoverySeconds:$collectorRestartRecoverySeconds,
   nodeReplacementRecoverySeconds:$nodeReplacementRecoverySeconds}
' > "${artifact_dir}/recovery.json"
chmod 600 "${artifact_dir}/recovery.json"

current_check=upgrade
helm upgrade "${release}" "${chart_archive}" \
  "${helm_cluster_args[@]}" \
  --namespace "${namespace}" --reuse-values --set agent.interval=6s --wait --timeout 5m
helm rollback "${release}" 1 \
  "${helm_cluster_args[@]}" \
  --namespace "${namespace}" --wait --timeout 5m
wait_for_strict_doctor "${work_dir}/doctor-after-rollback.json" || fail "strict doctor did not recover after rollback"
helm history "${release}" "${helm_cluster_args[@]}" --namespace "${namespace}" -o json \
  > "${work_dir}/helm-history.json"
jq -e --arg chartVersion "${chart_version}" '
  length == 3 and ([.[].revision | tonumber] == [1,2,3]) and
  .[-1].status == "deployed" and (.[-1].chart | endswith("-" + $chartVersion))
' "${work_dir}/helm-history.json" >/dev/null || fail "Helm upgrade and rollback history is invalid"

current_check=uninstall
helm uninstall "${release}" \
  "${helm_cluster_args[@]}" \
  --namespace "${namespace}" --wait
release_installed=false
owned_resources_removed || fail "chart-owned resources remain after uninstall"
delete_owned_namespace || fail "UID-preconditioned namespace deletion failed"
k wait --for=delete "namespace/${namespace}" --timeout=5m >/dev/null ||
  fail "namespace deletion did not complete"
namespace_created=false

current_check=prerequisites
write_lifecycle_evidence
write_summary passed
for evidence_file in qualification-summary.json environment.json provider-inventory.json \
  doctor.json status.json recovery.json lifecycle.json; do
  python3 hack/provider-profiles/validate_privacy.py "${artifact_dir}/${evidence_file}" ||
    fail "retained evidence failed the privacy contract: ${evidence_file}"
done
probe_image_digest=${probe_image##*@}
manifest_report=$(python3 hack/provider-profiles/evidence_manifest.py create \
  --bundle "${artifact_dir}" --probe-image-digest "${probe_image_digest}") ||
  fail "evidence manifest creation failed"
evidence_manifest_digest=$(jq -r '.manifestDigest' <<<"${manifest_report}")
write_validated_pending passed ||
  fail "pending provider evidence contract failed"
qualification_complete=true
echo "provider qualification run passed; manual evidence review is required: ${artifact_dir}"
