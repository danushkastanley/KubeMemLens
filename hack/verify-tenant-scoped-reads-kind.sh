#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${root}"
# shellcheck source=hack/lib/tenant-read-identity.sh
source hack/lib/tenant-read-identity.sh
# shellcheck source=hack/lib/tenant-read-evidence.sh
source hack/lib/tenant-read-evidence.sh
# shellcheck source=hack/lib/tenant-isolation-controls.sh
source hack/lib/tenant-isolation-controls.sh
# shellcheck source=hack/lib/tenant-isolation-http.sh
source hack/lib/tenant-isolation-http.sh
# shellcheck source=hack/lib/tenant-isolation-run.sh
source hack/lib/tenant-isolation-run.sh
kubeconfig=${TENANT_READ_KUBECONFIG:-}
context=${TENANT_READ_CONTEXT:-}
release_namespace=${TENANT_READ_NAMESPACE:-kube-memlens}
cli=${TENANT_READ_CLI:-}
artifact_dir=${TENANT_READ_ARTIFACT_DIR:-}
acknowledgement=${TENANT_READ_ACKNOWLEDGE:-}
phase=${TENANT_READ_PHASE:-install}
namespace_a=kube-memlens-tenant-a
namespace_b=kube-memlens-tenant-b
user_a=kube-memlens-tenant-a-user
user_b=kube-memlens-tenant-b-user
cluster_user=kube-memlens-cluster-operator
tenant_reader_service_account=kube-memlens-tenant-reader
cluster_reader_service_account=kube-memlens-tenant-cluster-reader
cluster_binding=kube-memlens-tenant-test-cluster-viewer
pod_a=tenant-a-sample
pod_b=tenant-b-sample
missing_pod=tenant-b-does-not-exist
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-tenant-reads.XXXXXX")
api=/apis/memory.kubememlens.io/v1alpha1
workload_image=busybox:1.37.0@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
watch_pid=

fail() {
  echo "tenant-scoped read verification failed: $*" >&2
  exit 1
}

for command in jq kubectl perl sort; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done
[ -n "${kubeconfig}" ] || fail "TENANT_READ_KUBECONFIG is required"
[ -f "${kubeconfig}" ] || fail "TENANT_READ_KUBECONFIG does not exist"
[ -n "${context}" ] || fail "TENANT_READ_CONTEXT is required"
case "${context}" in
  kind-*) ;;
  *) fail "TENANT_READ_CONTEXT must be a kind context" ;;
esac
[ "${acknowledgement}" = run-and-clean-tenant-read-verification ] ||
  fail "set TENANT_READ_ACKNOWLEDGE=run-and-clean-tenant-read-verification"
[ -n "${cli}" ] || fail "TENANT_READ_CLI is required"
[ -x "${cli}" ] || fail "TENANT_READ_CLI must be executable"
case "${phase}" in
  install|upgrade|rollback) ;;
  *) fail "TENANT_READ_PHASE must be install, upgrade, or rollback" ;;
esac

kctl() {
  kubectl --kubeconfig "${kubeconfig}" --context "${context}" --cache-dir "${work_dir}/discovery-cache" "$@"
}

for resource in \
  "namespace/${namespace_a}" \
  "namespace/${namespace_b}" \
  "clusterrolebinding/${cluster_binding}"
do
  ! kctl get "${resource}" >/dev/null 2>&1 || fail "refusing to replace existing ${resource}"
done

cleanup() {
  status=$?
  if [ -n "${watch_pid}" ]; then
    kill "${watch_pid}" >/dev/null 2>&1 || true
    wait "${watch_pid}" >/dev/null 2>&1 || true
  fi
  if [ "${isolation_run_started}" = true ]; then
    tenant_isolation_cleanup || { status=1; isolation_summary_written=false; }
  fi
  if [ "${TENANT_READ_RUN_ISOLATION:-false}" = true ] &&
    [ "${isolation_summary_written}" != true ] && [ -n "${artifact_dir}" ]; then
    mkdir -p "${artifact_dir}"
    jq -n '{schemaVersion:1,outcome:"failed",privacy:{rawResponsesRetained:false,credentialsRetained:false,runtimeIdentifiersIncluded:false}}' \
      > "${artifact_dir}/tenant-isolation-summary.json"
    chmod 0600 "${artifact_dir}/tenant-isolation-summary.json"
  fi
  kctl delete clusterrolebinding "${cluster_binding}" --ignore-not-found >/dev/null 2>&1 || true
  kctl delete namespace "${namespace_a}" "${namespace_b}" --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || true
  rm -f "${work_dir}/tenant-a.kubeconfig" "${work_dir}/cluster-operator.kubeconfig"
  if [ "${status}" -eq 0 ] || [ "${TENANT_READ_RUN_ISOLATION:-false}" = true ]; then
    rm -rf "${work_dir}"
  else
    echo "tenant-scoped read diagnostics: ${work_dir}" >&2
  fi
  exit "${status}"
}
trap cleanup EXIT

for role in kube-memlens-namespace-viewer kube-memlens-cluster-viewer kube-memlens-metrics-reader; do
  kctl get clusterrole "${role}" >/dev/null || fail "chart ClusterRole is missing: ${role}"
done

kctl apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace_a}
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${namespace_b}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-memlens-namespace-viewer
  namespace: ${namespace_a}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-memlens-namespace-viewer
subjects:
  - kind: User
    name: ${user_a}
    apiGroup: rbac.authorization.k8s.io
  - kind: ServiceAccount
    name: ${tenant_reader_service_account}
    namespace: ${namespace_a}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-memlens-namespace-viewer
  namespace: ${namespace_b}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-memlens-namespace-viewer
subjects:
  - kind: User
    name: ${user_b}
    apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${cluster_binding}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-memlens-cluster-viewer
subjects:
  - kind: User
    name: ${cluster_user}
    apiGroup: rbac.authorization.k8s.io
  - kind: ServiceAccount
    name: ${cluster_reader_service_account}
    namespace: ${namespace_a}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${tenant_reader_service_account}
  namespace: ${namespace_a}
automountServiceAccountToken: false
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${cluster_reader_service_account}
  namespace: ${namespace_a}
automountServiceAccountToken: false
---
apiVersion: v1
kind: Pod
metadata:
  name: ${pod_a}
  namespace: ${namespace_a}
  labels:
    app.kubernetes.io/name: kube-memlens-tenant-read-fixture
spec:
  automountServiceAccountToken: false
  containers:
    - name: fixture
      image: ${workload_image}
      command: ["/bin/sh", "-c", "sleep 3600"]
      resources:
        requests:
          memory: 8Mi
        limits:
          memory: 32Mi
---
apiVersion: v1
kind: Pod
metadata:
  name: ${pod_b}
  namespace: ${namespace_b}
  labels:
    app.kubernetes.io/name: kube-memlens-tenant-read-fixture
spec:
  automountServiceAccountToken: false
  containers:
    - name: fixture
      image: ${workload_image}
      command: ["/bin/sh", "-c", "sleep 3600"]
      resources:
        requests:
          memory: 8Mi
        limits:
          memory: 32Mi
YAML

kctl wait --for=condition=Ready "pod/${pod_a}" -n "${namespace_a}" --timeout=90s >/dev/null
kctl wait --for=condition=Ready "pod/${pod_b}" -n "${namespace_b}" --timeout=90s >/dev/null

[ "$(kctl auth can-i --as="${user_a}" list pods.memory.kubememlens.io -n "${namespace_a}")" = yes ] ||
  fail "tenant A user cannot list its KubeMemLens Pods"
[ "$(kctl auth can-i --as="${user_a}" list pods.memory.kubememlens.io -n "${namespace_b}")" = no ] ||
  fail "tenant A user can list tenant B KubeMemLens Pods"
[ "$(kctl auth can-i --as="${cluster_user}" list pods.memory.kubememlens.io --all-namespaces)" = yes ] ||
  fail "cluster operator cannot list KubeMemLens Pods"
[ "$(kctl auth can-i --as="${cluster_user}" get metrics.memory.kubememlens.io --all-namespaces)" = no ] ||
  fail "cluster operator received the separate metrics permission"

raw_get() {
  local user=$1
  local path=$2
  local output=$3
  kctl --request-timeout=5s --as="${user}" get --raw "${path}" > "${output}"
}

expect_forbidden() {
  local user=$1
  local path=$2
  local output=$3
  if kctl --request-timeout=5s --as="${user}" get --raw "${path}" > "${output}" 2>&1; then
    fail "forbidden request succeeded: ${path}"
  fi
  grep -Eiq 'forbidden|code[^0-9]*403|status[^0-9]*403' "${output}" ||
    fail "request did not return a forbidden result: ${path}"
}

wait_for_fixture_data() {
  local output=${work_dir}/cluster-pods.json
  for _ in $(seq 1 36); do
    if raw_get "${cluster_user}" "${api}/pods" "${output}" 2>/dev/null &&
      grep -Fq "${namespace_a}" "${output}" && grep -Fq "${namespace_b}" "${output}"; then
      return
    fi
    sleep 5
  done
  fail "collector did not report both tenant fixtures"
}
wait_for_fixture_data

raw_get "${user_a}" "${api}/namespaces/${namespace_a}/pods" "${work_dir}/tenant-a-pods.json"
grep -Fq "${pod_a}" "${work_dir}/tenant-a-pods.json" || fail "tenant A Pod is missing"
! grep -Fq "${namespace_b}" "${work_dir}/tenant-a-pods.json" || fail "tenant A response contains tenant B"
raw_get "${user_b}" "${api}/namespaces/${namespace_b}/pods" "${work_dir}/tenant-b-pods.json"
grep -Fq "${pod_b}" "${work_dir}/tenant-b-pods.json" || fail "tenant B Pod is missing"
! grep -Fq "${namespace_a}" "${work_dir}/tenant-b-pods.json" || fail "tenant B response contains tenant A"

for resource in containers workloads; do
  raw_get "${user_a}" "${api}/namespaces/${namespace_a}/${resource}" "${work_dir}/tenant-a-${resource}.json"
  ! grep -Fq "${namespace_b}" "${work_dir}/tenant-a-${resource}.json" ||
    fail "tenant A ${resource} response contains tenant B"
done
raw_get "${cluster_user}" "${api}/nodes" "${work_dir}/cluster-nodes.json"
raw_get "${cluster_user}" "${api}/clusterstatus/current" "${work_dir}/cluster-status.json"
expect_forbidden "${user_a}" "${api}/nodes" "${work_dir}/tenant-a-nodes-denied.txt"
expect_forbidden "${user_a}" "${api}/clusterstatus/current" "${work_dir}/tenant-a-status-denied.txt"
expect_forbidden "${cluster_user}" "${api}/metrics/current" "${work_dir}/cluster-metrics-denied.txt"
expect_forbidden "${user_a}" "${api}/pods" "${work_dir}/tenant-a-cluster-list-denied.txt"

expect_forbidden "${user_a}" "${api}/namespaces/${namespace_b}/pods/${pod_b}" "${work_dir}/existing-denied.txt"
expect_forbidden "${user_a}" "${api}/namespaces/${namespace_b}/pods/${missing_pod}" "${work_dir}/missing-denied.txt"
sed -e "s/${pod_b}/<target>/g" -e "s/${missing_pod}/<target>/g" "${work_dir}/existing-denied.txt" > "${work_dir}/existing-normalised.txt"
sed -e "s/${pod_b}/<target>/g" -e "s/${missing_pod}/<target>/g" "${work_dir}/missing-denied.txt" > "${work_dir}/missing-normalised.txt"
cmp -s "${work_dir}/existing-normalised.txt" "${work_dir}/missing-normalised.txt" ||
  fail "existing and missing out-of-scope object denials differ"
expect_forbidden "${user_a}" "${api}/namespaces/${namespace_b}/pods/${pod_b}/history" "${work_dir}/history-denied.txt"

if [ "${phase}" != install ]; then
  jq -n \
    --arg phase "${phase}" \
    --arg serverVersion "$(kctl version -o json | jq -r '.serverVersion.gitVersion')" \
    '{phase:$phase,serverVersion:$serverVersion,authorisation:{namespaceIsolation:true,directIdentifierDenied:true,clusterOverrideExplicit:true},performance:[]}' \
    > "${work_dir}/tenant-scoped-read-evidence.json"
  if [ -n "${artifact_dir}" ]; then
    mkdir -p "${artifact_dir}"
    cp "${work_dir}/tenant-scoped-read-evidence.json" "${artifact_dir}/tenant-scoped-read-evidence.json"
  fi
  cat "${work_dir}/tenant-scoped-read-evidence.json"
  echo "tenant-scoped read ${phase} verification passed"
  exit 0
fi

tenant_a_config=${work_dir}/tenant-a.kubeconfig
cluster_config=${work_dir}/cluster-operator.kubeconfig
tenant_read_make_service_account_kubeconfig "${tenant_a_config}" "${namespace_a}" "${tenant_reader_service_account}"
tenant_read_make_service_account_kubeconfig "${cluster_config}" "${namespace_a}" "${cluster_reader_service_account}"

tenant_read_assert_service_account_identity "${tenant_a_config}" "${context}" "${namespace_a}" "${tenant_reader_service_account}" tenant
tenant_read_assert_service_account_identity "${cluster_config}" "${context}" "${namespace_a}" "${cluster_reader_service_account}" cluster
[ "$(kubectl --kubeconfig "${tenant_a_config}" --context "${context}" auth can-i list pods.memory.kubememlens.io -n "${namespace_a}")" = yes ] ||
  fail "tenant CLI identity cannot list its KubeMemLens Pods"
[ "$(kubectl --kubeconfig "${tenant_a_config}" --context "${context}" auth can-i list pods.memory.kubememlens.io -n "${namespace_b}")" = no ] ||
  fail "tenant CLI identity can list another namespace"

cli_a=("${cli}" --kubeconfig "${tenant_a_config}" --context "${context}")
cli_cluster=("${cli}" --kubeconfig "${cluster_config}" --context "${context}")
"${cli_a[@]}" top pods -n "${namespace_a}" --output json > "${work_dir}/cli-tenant-a-pods.json"
! grep -Fq "${namespace_b}" "${work_dir}/cli-tenant-a-pods.json" || fail "CLI tenant Pod view contains tenant B"
"${cli_a[@]}" top namespaces -n "${namespace_a}" --output json > "${work_dir}/cli-tenant-a-namespace.json"
"${cli_a[@]}" compare "pod/${pod_a}" "pod/${pod_a}" -n "${namespace_a}" > "${work_dir}/cli-compare.txt"
"${cli_a[@]}" recommend pod "${pod_a}" -n "${namespace_a}" --output json > "${work_dir}/cli-recommend.json"

history_ready=false
for _ in $(seq 1 24); do
  if "${cli_a[@]}" history pod "${pod_a}" -n "${namespace_a}" > "${work_dir}/cli-history.txt" 2>&1; then
    history_ready=true
    break
  fi
  sleep 5
done
[ "${history_ready}" = true ] || fail "tenant-scoped history did not become available"
"${cli_a[@]}" capture --namespace "${namespace_a}" --pod "${pod_a}" --include-history \
  --output "${work_dir}/incident.json" > "${work_dir}/cli-capture.txt"
! grep -Fq "${namespace_b}" "${work_dir}/incident.json" || fail "tenant capture contains tenant B"

"${cli_a[@]}" top pods -n "${namespace_a}" --watch --watch-interval=500ms > "${work_dir}/cli-watch.txt" 2>&1 &
watch_pid=$!
sleep 1
kctl delete rolebinding kube-memlens-namespace-viewer -n "${namespace_a}" --wait=true >/dev/null
for _ in $(seq 1 20); do
  kill -0 "${watch_pid}" >/dev/null 2>&1 || break
  sleep 0.5
done
if kill -0 "${watch_pid}" >/dev/null 2>&1; then
  fail "namespace polling continued after access was revoked"
fi
set +e
wait "${watch_pid}"
watch_status=$?
set -e
watch_pid=
[ "${watch_status}" -ne 0 ] || fail "revoked namespace polling exited successfully"
grep -Eiq 'forbidden|permission|not authorised|not authorized' "${work_dir}/cli-watch.txt" ||
  fail "revoked namespace polling did not preserve the forbidden state"
kctl create rolebinding kube-memlens-namespace-viewer -n "${namespace_a}" \
  --clusterrole kube-memlens-namespace-viewer --user "${user_a}" \
  --serviceaccount "${namespace_a}:${tenant_reader_service_account}" >/dev/null
raw_get "${user_a}" "${api}/namespaces/${namespace_a}/pods" "${work_dir}/tenant-a-after-regrant.json"

"${cli_cluster[@]}" top pods --all-namespaces --output json > "${work_dir}/cli-cluster-pods.json"
grep -Fq "${namespace_a}" "${work_dir}/cli-cluster-pods.json" || fail "cluster view is missing tenant A"
grep -Fq "${namespace_b}" "${work_dir}/cli-cluster-pods.json" || fail "cluster view is missing tenant B"

if "${cli_a[@]}" top pods --all-namespaces > "${work_dir}/cli-all-denied.txt" 2>&1; then
  fail "namespace viewer CLI all-namespace request succeeded"
fi
grep -Eiq 'forbidden|permission|not authorised|not authorized' "${work_dir}/cli-all-denied.txt" ||
  fail "CLI did not preserve the forbidden state"
if "${cli_a[@]}" recommend pod "${pod_b}" -n "${namespace_b}" > "${work_dir}/cli-recommend-denied.txt" 2>&1; then
  fail "cross-namespace recommendation succeeded"
fi
if "${cli_a[@]}" capture --namespace "${namespace_b}" --pod "${pod_b}" \
  --output "${work_dir}/forbidden-incident.json" > "${work_dir}/cli-capture-denied.txt" 2>&1; then
  fail "cross-namespace capture succeeded"
fi

namespace_perf=$(tenant_read_measure_requests "${work_dir}" "${user_a}" "${api}/namespaces/${namespace_a}/pods" namespace)
cluster_perf=$(tenant_read_measure_requests "${work_dir}" "${cluster_user}" "${api}/pods" cluster)
if [ "${TENANT_READ_RUN_ISOLATION:-false}" = true ]; then
  tenant_isolation_run
fi
jq -n \
  --arg phase "${phase}" \
  --arg serverVersion "$(kctl version -o json | jq -r '.serverVersion.gitVersion')" \
  --argjson namespace "${namespace_perf}" \
  --argjson cluster "${cluster_perf}" \
  '{phase:$phase,serverVersion:$serverVersion,authorisation:{namespaceIsolation:true,directIdentifierDenied:true,clusterOverrideExplicit:true},performance:[$namespace,$cluster]}' \
  > "${work_dir}/tenant-scoped-read-evidence.json"

if kctl logs deployment/kube-memlens-collector -n "${release_namespace}" --since=10m 2>/dev/null |
  grep -Fq "${pod_b}"; then
  fail "collector security log contains a denied object name"
fi

if [ -n "${artifact_dir}" ]; then
  mkdir -p "${artifact_dir}"
  cp "${work_dir}/tenant-scoped-read-evidence.json" "${artifact_dir}/tenant-scoped-read-evidence.json"
fi
cat "${work_dir}/tenant-scoped-read-evidence.json"
echo "tenant-scoped read verification passed"
