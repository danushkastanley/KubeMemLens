#!/usr/bin/env bash

set -Eeuo pipefail

# Qualify an existing, explicitly selected Kubernetes cluster without creating
# infrastructure or publishing artefacts. The namespace must not already exist.

required_acknowledgement=install-and-remove-kube-memlens
context=${QUALIFY_CONTEXT:-}
namespace=${QUALIFY_NAMESPACE:-}
image_repository=${QUALIFY_IMAGE_REPOSITORY:-}
image_digest=${QUALIFY_IMAGE_DIGEST:-}
probe_image=${QUALIFY_PROBE_IMAGE:-}
artifact_dir=${QUALIFY_ARTIFACT_DIR:-}
release=kube-memlens-qualification
kubeconfig=${KUBECONFIG:-}
work_dir=
cli=
namespace_created=false
release_installed=false
qualification_complete=false
network_policy_result=not-run
install_started_seconds=0
first_explanation_seconds=-1

usage() {
  cat <<'EOF'
Run KubeMemLens qualification against an existing disposable or authorised cluster.

Required environment:
  QUALIFY_CONTEXT             Exact kubeconfig context to use
  QUALIFY_NAMESPACE           New namespace beginning kube-memlens-qualification-
  QUALIFY_IMAGE_REPOSITORY    Release image repository
  QUALIFY_IMAGE_DIGEST        Exact sha256:<64 lowercase hex> image digest
  QUALIFY_PROBE_IMAGE         Digest-pinned image containing /bin/sh and wget
  QUALIFY_ARTIFACT_DIR        New or empty local evidence directory
  QUALIFY_ACKNOWLEDGE         install-and-remove-kube-memlens

The script installs and removes KubeMemLens and its dedicated namespace. It
refuses existing namespaces, releases, and cluster-scoped KubeMemLens RBAC.
It does not create cloud infrastructure, push images, publish results, or run a
high-density workload soak.
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
[ -n "${context}" ] || fail "QUALIFY_CONTEXT is required"
[[ "${namespace}" =~ ^kube-memlens-qualification-[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$ ]] ||
  fail "QUALIFY_NAMESPACE must be a new, lower-case kube-memlens-qualification-* namespace"
[[ "${image_repository}" =~ ^[A-Za-z0-9._/:_-]+$ ]] || fail "QUALIFY_IMAGE_REPOSITORY is invalid"
[[ "${image_digest}" =~ ^sha256:[a-f0-9]{64}$ ]] || fail "QUALIFY_IMAGE_DIGEST must be an exact lowercase sha256 digest"
[[ "${probe_image}" =~ @sha256:[a-f0-9]{64}$ ]] || fail "QUALIFY_PROBE_IMAGE must be digest-pinned"
[ -n "${artifact_dir}" ] || fail "QUALIFY_ARTIFACT_DIR is required"
if [ "${artifact_dir}" = "/" ] || [ "${artifact_dir}" = "." ]; then
  fail "unsafe QUALIFY_ARTIFACT_DIR"
fi
if [ -d "${artifact_dir}" ] && [ -n "$(find "${artifact_dir}" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
  fail "QUALIFY_ARTIFACT_DIR must be empty"
fi

mkdir -p "${artifact_dir}"
artifact_dir=$(cd "${artifact_dir}" && pwd -P)
repo_root=$(pwd -P)
if [ "${artifact_dir}" = "/" ] || [ "${artifact_dir}" = "${repo_root}" ]; then
  fail "QUALIFY_ARTIFACT_DIR resolves to an unsafe directory"
fi
chmod 700 "${artifact_dir}"
for output in qualification-summary.json doctor.json status.json explanation.json environment.json; do
  [ ! -e "${artifact_dir}/${output}" ] || fail "refusing to overwrite ${artifact_dir}/${output}"
done

for command in go helm jq kubectl; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-qualification.XXXXXX")
cli=${work_dir}/kubectl-memlens

kubectl_args=(--context "${context}")
if [ -n "${kubeconfig}" ]; then
  kubectl_args=(--kubeconfig "${kubeconfig}" "${kubectl_args[@]}")
fi

k() {
  kubectl "${kubectl_args[@]}" "$@"
}

delete_owned_cluster_rbac() {
  local resource release_name release_namespace
  for resource in clusterrolebinding/kube-memlens-agent clusterrole/kube-memlens-agent; do
    release_name=$(k get "${resource}" -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}' 2>/dev/null || true)
    release_namespace=$(k get "${resource}" -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-namespace}' 2>/dev/null || true)
    if [ "${release_name}" = "${release}" ] && [ "${release_namespace}" = "${namespace}" ]; then
      k delete "${resource}" >/dev/null 2>&1 || true
    fi
  done
}

helm_cluster_args=(--kube-context "${context}")
if [ -n "${kubeconfig}" ]; then
  helm_cluster_args+=(--kubeconfig "${kubeconfig}")
fi

write_summary() {
  local outcome=$1
  jq -n \
    --arg outcome "${outcome}" \
    --arg completedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg imageDigest "${image_digest}" \
    --arg networkPolicy "${network_policy_result:-not-run}" \
	--argjson installToExplanationSeconds "${first_explanation_seconds}" \
    '{schemaVersion: 1, outcome: $outcome, completedAt: $completedAt,
      image: {repository: "redacted", digest: $imageDigest},
      checks: {networkPolicy: $networkPolicy},
	  measurements: {installToFirstValidExplanationSeconds: $installToExplanationSeconds},
      caveats: ["Cluster identifiers are deliberately omitted", "This is not a high-density soak"]}' \
    > "${artifact_dir}/qualification-summary.json"
  chmod 600 "${artifact_dir}/qualification-summary.json"
}

cleanup() {
  local status=$?
  trap - EXIT
  if [ "${qualification_complete}" != true ]; then
    write_summary failed
  fi
  if [ "${release_installed}" = true ]; then
    helm uninstall "${release}" \
      "${helm_cluster_args[@]}" \
      --namespace "${namespace}" --wait >/dev/null 2>&1 || true
  fi
  if [ "${namespace_created}" = true ]; then
    delete_owned_cluster_rbac
    k delete namespace "${namespace}" --wait=false >/dev/null 2>&1 || true
  fi
  if [[ "${work_dir}" == "${TMPDIR:-/tmp}/kube-memlens-qualification."* ]]; then
    rm -rf -- "${work_dir}"
  fi
  if [ "${status}" -ne 0 ]; then
    echo "qualification failed; sanitised evidence: ${artifact_dir}" >&2
  fi
  exit "${status}"
}
trap cleanup EXIT

k config get-contexts "${context}" -o name | grep -Fxq "${context}" || fail "kubeconfig context not found: ${context}"
k version -o json >/dev/null
if k get namespace "${namespace}" >/dev/null 2>&1; then
  fail "namespace already exists: ${namespace}"
fi
if k get clusterrole kube-memlens-agent >/dev/null 2>&1 ||
  k get clusterrolebinding kube-memlens-agent >/dev/null 2>&1; then
  fail "cluster-scoped kube-memlens-agent RBAC already exists; use a cluster without another installation"
fi
k auth can-i create namespaces | grep -Fxq yes || fail "current identity cannot create namespaces"
k auth can-i create clusterroles.rbac.authorization.k8s.io | grep -Fxq yes ||
  fail "current identity cannot install required cluster RBAC"

nodes_json=${work_dir}/nodes.json
k get nodes -o json > "${nodes_json}"
linux_nodes=$(jq '[.items[] | select(.metadata.labels["kubernetes.io/os"] == "linux") | select((.spec.unschedulable // false) == false)] | length' "${nodes_json}")
[ "${linux_nodes}" -gt 0 ] || fail "cluster has no schedulable Linux nodes"

k create namespace "${namespace}" >/dev/null
namespace_created=true
go build -trimpath -o "${cli}" ./cmd/kubectl-memlens

helm_args=(
  upgrade --install "${release}" ./charts/kube-memlens
  "${helm_cluster_args[@]}"
  --namespace "${namespace}"
  --set-string namespace.name="${namespace}"
  --set-string image.repository="${image_repository}"
  --set-string image.digest="${image_digest}"
  --set image.pullPolicy=IfNotPresent
  --wait --timeout 5m
)
release_installed=true
install_started_seconds=${SECONDS}
helm "${helm_args[@]}"

k rollout status daemonset/kube-memlens-agent -n "${namespace}" --timeout=3m
k rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=3m

daemonset_json=${work_dir}/daemonset.json
k get daemonset kube-memlens-agent -n "${namespace}" -o json > "${daemonset_json}"
desired=$(jq -r '.status.desiredNumberScheduled // 0' "${daemonset_json}")
ready=$(jq -r '.status.numberReady // 0' "${daemonset_json}")
[ "${desired}" -eq "${linux_nodes}" ] ||
  fail "agent targets ${desired}/${linux_nodes} schedulable Linux nodes; configure explicit agent tolerations"
[ "${ready}" -eq "${desired}" ] || fail "only ${ready}/${desired} agent Pods are ready"

pods_json=${work_dir}/pods.json
k get pods -n "${namespace}" -o json > "${pods_json}"
jq -e --arg digest "${image_digest}" '
  [.items[].status.containerStatuses[]?] | length > 0 and
  all(.[]; (.imageID // "") | contains($digest))
' "${pods_json}" >/dev/null || fail "one or more running containers do not report the requested image digest"
jq -e '
  [.items[].spec.containers[]] | all(.[];
    ((.securityContext.privileged // false) == false) and
    (.securityContext.allowPrivilegeEscalation == false) and
    (.securityContext.readOnlyRootFilesystem == true) and
    ((.securityContext.capabilities.drop // []) | index("ALL") != null))
' "${pods_json}" >/dev/null || fail "a workload failed the expected container security-context checks"

cli_args=(--context "${context}" --collector-namespace "${namespace}")
if [ -n "${kubeconfig}" ]; then
  cli_args=(--kubeconfig "${kubeconfig}" "${cli_args[@]}")
fi
doctor_raw=${work_dir}/doctor.json
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

wait_for_strict_doctor "${doctor_raw}" || fail "strict doctor did not pass"
jq -e '.checks | length > 0 and all(.[]; .status == "pass")' "${doctor_raw}" >/dev/null ||
  fail "strict doctor contains a non-passing check"

jq '
  .connection = "redacted" |
  .nodes = ((.nodes // []) | to_entries | map(.value.nodeName = ("node-" + ((.key + 1) | tostring)) | .value))
' "${doctor_raw}" > "${artifact_dir}/doctor.json"
chmod 600 "${artifact_dir}/doctor.json"

"${cli}" "${cli_args[@]}" status --output json \
  | jq '.connection.collector = "redacted" | .connection.description = "redacted"' \
  > "${artifact_dir}/status.json"
"${cli}" "${cli_args[@]}" top pods --all-namespaces --output json > "${work_dir}/top.json"
collector_pod=$(k get pods -n "${namespace}" -l app.kubernetes.io/name=kube-memlens-collector -o jsonpath='{.items[0].metadata.name}')
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
jq '.kubernetes.node = "redacted"' "${explanation_raw}" > "${artifact_dir}/explanation.json"
jq -e '.kubernetes.node == "redacted"' "${artifact_dir}/explanation.json" >/dev/null
chmod 600 "${artifact_dir}/status.json" "${artifact_dir}/explanation.json"

metrics_file=${work_dir}/collector-metrics.txt
k get --raw "/api/v1/namespaces/${namespace}/services/http:kube-memlens-collector:8080/proxy/metrics" \
  > "${metrics_file}"
grep -q '^kubememlens_collector_ingestion_requests_total' "${metrics_file}" || fail "collector metrics are unavailable"

run_probe() {
  local name=$1
  local port=$2
  local expected_phase=$3
  k run "${name}" -n "${namespace}" \
    --image="${probe_image}" --restart=Never \
    --labels=app.kubernetes.io/name=kube-memlens-qualification-probe \
    --command -- /bin/sh -c "wget -T 5 -qO- http://kube-memlens-collector:${port}/healthz" >/dev/null
  local phase=
  for _ in $(seq 1 30); do
    phase=$(k get pod "${name}" -n "${namespace}" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    case "${phase}" in Succeeded|Failed) break ;; esac
    sleep 2
  done
  k logs "${name}" -n "${namespace}" > "${artifact_dir}/${name}.log" 2>&1 || true
  chmod 600 "${artifact_dir}/${name}.log"
  k delete pod "${name}" -n "${namespace}" --wait=true >/dev/null
  [ "${phase}" = "${expected_phase}" ] || fail "probe ${name} ended ${phase:-unknown}, expected ${expected_phase}"
}

sleep 5
network_policy_result=failed
run_probe qualification-read-allowed 8080 Succeeded
run_probe qualification-ingest-denied 8081 Failed
network_policy_result=enforced

provider=$(jq -r '
  [.items[].spec.providerID // ""] |
  if any(.[]; startswith("gce://")) then "gke-or-gce"
  elif any(.[]; startswith("aws://")) then "eks-or-aws"
  elif any(.[]; startswith("azure://")) then "aks-or-azure"
  else "other-or-unreported" end
' "${nodes_json}")
k version -o json | jq \
  --arg provider "${provider}" \
  --slurpfile nodes "${nodes_json}" '
    {schemaVersion: 1, providerFamily: $provider,
     kubernetes: .serverVersion.gitVersion,
     nodeProfiles: ($nodes[0].items
       | group_by([.status.nodeInfo.architecture, .status.nodeInfo.containerRuntimeVersion,
                   .status.nodeInfo.kernelVersion, .status.nodeInfo.osImage, .status.nodeInfo.kubeletVersion])
       | map({count: length, architecture: .[0].status.nodeInfo.architecture,
              runtime: .[0].status.nodeInfo.containerRuntimeVersion,
              kernel: .[0].status.nodeInfo.kernelVersion,
              osImage: .[0].status.nodeInfo.osImage,
              kubelet: .[0].status.nodeInfo.kubeletVersion}))}
  ' > "${artifact_dir}/environment.json"
chmod 600 "${artifact_dir}/environment.json"

helm upgrade "${release}" ./charts/kube-memlens \
  "${helm_cluster_args[@]}" \
  --namespace "${namespace}" --reuse-values --set agent.interval=6s --wait --timeout 5m
helm rollback "${release}" 1 \
  "${helm_cluster_args[@]}" \
  --namespace "${namespace}" --wait --timeout 5m
wait_for_strict_doctor "${work_dir}/doctor-after-rollback.json" || fail "strict doctor did not recover after rollback"

helm uninstall "${release}" \
  "${helm_cluster_args[@]}" \
  --namespace "${namespace}" --wait
release_installed=false
if k get clusterrole kube-memlens-agent >/dev/null 2>&1 ||
  k get clusterrolebinding kube-memlens-agent >/dev/null 2>&1; then
  fail "cluster-scoped RBAC remains after uninstall"
fi
k delete namespace "${namespace}" --wait=true >/dev/null
namespace_created=false

qualification_complete=true
write_summary passed
echo "cluster qualification passed; sanitised evidence: ${artifact_dir}"
