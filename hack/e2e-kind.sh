#!/usr/bin/env bash

set -Eeuo pipefail

cluster_name=${E2E_CLUSTER_NAME:-kube-memlens-e2e}
namespace=${E2E_NAMESPACE:-kube-memlens}
node_image=${E2E_NODE_IMAGE:-kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5}
image=${E2E_IMAGE:-kube-memlens:local-e2e}
artifact_dir=${E2E_ARTIFACT_DIR:-}
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-e2e.XXXXXX")
kubeconfig=${work_dir}/kubeconfig
cli=${work_dir}/kubectl-memlens
cluster_created=false

run_linux_fixture_benchmarks() {
  if [ "${E2E_RUN_DENSITY_BENCHMARKS:-false}" != true ]; then
    return
  fi
  node_container="${cluster_name}-control-plane"
  node_machine=$(docker exec "${node_container}" uname -m)
  case "${node_machine}" in
    x86_64) node_arch=amd64 ;;
    aarch64|arm64) node_arch=arm64 ;;
    *)
      echo "unsupported kind node architecture for benchmark: ${node_machine}" >&2
      return 1
      ;;
  esac
  CGO_ENABLED=0 GOOS=linux GOARCH="${node_arch}" go test -c -o "${work_dir}/cgroup-bench.test" ./internal/cgroup
  CGO_ENABLED=0 GOOS=linux GOARCH="${node_arch}" go test -c -o "${work_dir}/kube-bench.test" ./internal/kube
  cgroup_bench_path=/usr/local/bin/kube-memlens-cgroup-bench.test
  kube_bench_path=/usr/local/bin/kube-memlens-mapping-bench.test
  docker cp "${work_dir}/cgroup-bench.test" "${node_container}:${cgroup_bench_path}"
  docker cp "${work_dir}/kube-bench.test" "${node_container}:${kube_bench_path}"
  docker exec "${node_container}" test -x "${cgroup_bench_path}"
  docker exec "${node_container}" test -x "${kube_bench_path}"
  output_dir=${artifact_dir:-${work_dir}}
  mkdir -p "${output_dir}"
  docker exec "${node_container}" "${cgroup_bench_path}" \
    -test.run '^$' -test.bench 'BenchmarkWalker/containers-(5000|10000)$' \
    -test.benchmem -test.benchtime=1x -test.count=3 \
    | tee "${output_dir}/linux-cgroup-density-benchmark.txt"
  docker exec "${node_container}" "${kube_bench_path}" \
    -test.run '^$' -test.bench 'BenchmarkBuildPodIndexAndLookup/pods-(5000|10000)$' \
    -test.benchmem -test.benchtime=1x -test.count=3 \
    | tee "${output_dir}/linux-mapping-density-benchmark.txt"
  docker exec "${node_container}" rm -f "${cgroup_bench_path}" "${kube_bench_path}"
}

run_live_density_smoke() {
  if [ "${E2E_RUN_LIVE_DENSITY_SMOKE:-false}" != true ]; then
    return
  fi
  local workload_image=${E2E_LIVE_DENSITY_IMAGE:-}
  local output_dir=${artifact_dir:-${work_dir}/artifacts}/live-density-smoke
  [[ "${workload_image}" =~ @sha256:[a-f0-9]{64}$ ]] || {
    echo "E2E_LIVE_DENSITY_IMAGE must be digest-pinned" >&2
    return 1
  }
  SOAK_CONTEXT="kind-${cluster_name}" \
    SOAK_COLLECTOR_NAMESPACE="${namespace}" \
    SOAK_NAMESPACE=kube-memlens-soak-e2e \
    SOAK_WORKLOAD_IMAGE="${workload_image}" \
    SOAK_ARTIFACT_DIR="${output_dir}" \
    SOAK_PROFILE=development \
    SOAK_CONTAINERS="${E2E_LIVE_DENSITY_CONTAINERS:-20}" \
    SOAK_CONTAINERS_PER_POD="${E2E_LIVE_DENSITY_CONTAINERS_PER_POD:-10}" \
    SOAK_DURATION_SECONDS="${E2E_LIVE_DENSITY_DURATION_SECONDS:-30}" \
    SOAK_SAMPLE_INTERVAL_SECONDS=5 \
    SOAK_READY_TIMEOUT=5m \
    SOAK_ACKNOWLEDGE=run-and-remove-kube-memlens-density-soak \
    KUBECONFIG="${kubeconfig}" \
    hack/soak-live-density.sh
}

run_tui_smoke() {
  if [ "${E2E_RUN_TUI_SMOKE:-false}" != true ]; then
    return
  fi
  local output_dir=${artifact_dir:-${work_dir}/artifacts}/tui-smoke
  TUI_E2E_CONTEXT="kind-${cluster_name}" \
    TUI_E2E_KUBECONFIG="${kubeconfig}" \
    TUI_E2E_CLI="${cli}" \
    TUI_E2E_COLLECTOR_NAMESPACE="${namespace}" \
    TUI_E2E_WORKLOAD_IMAGE="${E2E_TUI_WORKLOAD_IMAGE:-public.ecr.aws/docker/library/busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028}" \
    TUI_E2E_ARTIFACT_DIR="${output_dir}" \
    TUI_E2E_ACKNOWLEDGE=run-and-remove-kube-memlens-tui-smoke \
    hack/e2e-tui-kind.sh
}

for command in docker go helm jq kind kubectl; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "required command not found: ${command}" >&2
    exit 1
  fi
done

if kind get clusters | grep -Fxq "${cluster_name}"; then
  echo "refusing to replace existing kind cluster: ${cluster_name}" >&2
  exit 1
fi

collect_diagnostics() {
  if [ "${cluster_created}" != true ]; then
    return
  fi
  if [ -z "${artifact_dir}" ]; then
    artifact_dir=${work_dir}/artifacts
  fi
  mkdir -p "${artifact_dir}"
  kind export logs "${artifact_dir}/kind" --name "${cluster_name}" >/dev/null 2>&1 || true
  KUBECONFIG="${kubeconfig}" kubectl get all -A -o wide > "${artifact_dir}/resources.txt" 2>&1 || true
  KUBECONFIG="${kubeconfig}" kubectl describe pods -n "${namespace}" > "${artifact_dir}/pods.txt" 2>&1 || true
}

cleanup() {
  status=$?
  if [ "${status}" -ne 0 ]; then
    collect_diagnostics
    echo "kind end-to-end test failed; diagnostics: ${artifact_dir:-${work_dir}/artifacts}" >&2
  fi
  if [ "${cluster_created}" = true ]; then
    kind delete cluster --name "${cluster_name}" >/dev/null 2>&1 || true
  fi
  if [ "${status}" -eq 0 ] && [ -z "${E2E_ARTIFACT_DIR:-}" ]; then
    rm -rf "${work_dir}"
  fi
  exit "${status}"
}
trap cleanup EXIT

echo "Building ${image}"
docker build \
  --build-arg VERSION=e2e \
  --build-arg COMMIT="$(git rev-parse --short HEAD 2>/dev/null || printf unknown)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t "${image}" .
go build -trimpath -o "${cli}" ./cmd/kubectl-memlens

echo "Creating ${cluster_name} with ${node_image}"
kind create cluster \
  --name "${cluster_name}" \
  --image "${node_image}" \
  --kubeconfig "${kubeconfig}" \
  --wait 120s
cluster_created=true
kind load docker-image "${image}" --name "${cluster_name}"
run_linux_fixture_benchmarks

helm upgrade --install kube-memlens ./charts/kube-memlens \
  --kubeconfig "${kubeconfig}" \
  --namespace "${namespace}" \
  --create-namespace \
  --set image.repository="${image%:*}" \
  --set image.tag="${image##*:}" \
  --set image.pullPolicy=Never \
  --wait \
  --timeout 3m

KUBECONFIG="${kubeconfig}" kubectl rollout status daemonset/kube-memlens-agent -n "${namespace}" --timeout=2m
KUBECONFIG="${kubeconfig}" kubectl rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=2m

cli_args=(
  --kubeconfig "${kubeconfig}"
  --context "kind-${cluster_name}"
  --collector-namespace "${namespace}"
)

wait_for_doctor() {
  doctor_ok=false
  for _ in $(seq 1 24); do
    if "${cli}" "${cli_args[@]}" doctor --strict > "${work_dir}/doctor.txt" 2>&1; then
      doctor_ok=true
      break
    fi
    sleep 5
  done
  if [ "${doctor_ok}" != true ]; then
    cat "${work_dir}/doctor.txt" >&2
    return 1
  fi
  cat "${work_dir}/doctor.txt"
}

wait_for_doctor
"${cli}" "${cli_args[@]}" status --output json > "${work_dir}/status.json"
grep -q '"status": "ok"' "${work_dir}/status.json"
"${cli}" "${cli_args[@]}" top pods --all-namespaces > "${work_dir}/top.txt"
grep -q '^NAMESPACE' "${work_dir}/top.txt"
"${cli}" "${cli_args[@]}" top workloads --all-namespaces > "${work_dir}/workloads.txt"
grep -q 'Deployment.*kube-memlens-collector' "${work_dir}/workloads.txt"
"${cli}" "${cli_args[@]}" top pods --all-namespaces \
  --selector app.kubernetes.io/name=kube-memlens-collector \
  --field-selector metadata.namespace="${namespace}" \
  --sort-by name --no-headers > "${work_dir}/selected-top.txt"
"${cli}" "${cli_args[@]}" top pods --all-namespaces --output json > "${work_dir}/top.json"
grep -q '"kind": "Pod"' "${work_dir}/top.json"
collector_pod=$(KUBECONFIG="${kubeconfig}" kubectl get pods -n "${namespace}" \
  -l app.kubernetes.io/name=kube-memlens-collector \
  -o jsonpath='{.items[0].metadata.name}')
grep -q "${collector_pod}" "${work_dir}/selected-top.txt"
agent_pod=$(KUBECONFIG="${kubeconfig}" kubectl get pods -n "${namespace}" \
  -l app.kubernetes.io/name=kube-memlens-agent \
  -o jsonpath='{.items[0].metadata.name}')
"${cli}" "${cli_args[@]}" compare "pod/${collector_pod}" "pod/${agent_pod}" -n "${namespace}" > "${work_dir}/compare-live.txt"
grep -q '^Live comparison:' "${work_dir}/compare-live.txt"
evidence_window_ok=false
for _ in $(seq 1 12); do
  "${cli}" "${cli_args[@]}" explain pod "${collector_pod}" -n "${namespace}" --output json > "${work_dir}/explain.json"
  if jq -e '.schemaVersion == 1 and (.finding.severity | length > 0) and
    (.finding.confidence | length > 0) and (.finding.caveats | length > 0) and
    (.finding.evidenceWindow.observationStart != null) and
    .finding.evidenceWindow.counterDeltaKnown == true' "${work_dir}/explain.json" >/dev/null; then
    evidence_window_ok=true
    break
  fi
  sleep 2
done
[ "${evidence_window_ok}" = true ] || {
  echo "Pod explanation did not acquire a counter-delta evidence window" >&2
  exit 1
}
"${cli}" "${cli_args[@]}" explain pod "${collector_pod}" -n "${namespace}" > "${work_dir}/explain.txt"
grep -q '^Pod:' "${work_dir}/explain.txt"
grep -q '^Severity:' "${work_dir}/explain.txt"
grep -q '^Observation window:' "${work_dir}/explain.txt"
grep -Eq '^Counter window: .+ to .+' "${work_dir}/explain.txt"
for sensitive_field in containerID cgroupPath labels podUID; do
  if grep -q "\"${sensitive_field}\"" "${work_dir}/explain.json"; then
    echo "machine explanation contains sensitive field: ${sensitive_field}" >&2
    exit 1
  fi
done
"${cli}" "${cli_args[@]}" explain workload deployment/kube-memlens-collector -n "${namespace}" > "${work_dir}/explain-workload.txt"
grep -q '^Workload: Deployment/' "${work_dir}/explain-workload.txt"
"${cli}" "${cli_args[@]}" recommend pod "${collector_pod}" -n "${namespace}" --output json > "${work_dir}/recommend.json"
grep -q '"automaticMutation": false' "${work_dir}/recommend.json"
"${cli}" "${cli_args[@]}" history pod "${collector_pod}" -n "${namespace}" > "${work_dir}/history.txt"
grep -q '^Pod:' "${work_dir}/history.txt"
grep -q '^TIME' "${work_dir}/history.txt"
"${cli}" "${cli_args[@]}" history pod "${collector_pod}" -n "${namespace}" --since 15m > "${work_dir}/history-since.txt"
grep -q '^TIME' "${work_dir}/history-since.txt"
"${cli}" "${cli_args[@]}" capture --namespace "${namespace}" --pod "${collector_pod}" --include-history --output "${work_dir}/incident.json" > "${work_dir}/capture.txt"
grep -q '"redacted": true' "${work_dir}/incident.json"
if grep -q '/kubepods' "${work_dir}/incident.json"; then
  echo "redacted incident contains a cgroup path" >&2
  exit 1
fi
"${cli}" replay "${work_dir}/incident.json" --pod "${namespace}/${collector_pod}" > "${work_dir}/replay.txt"
grep -q '^Diagnosis:' "${work_dir}/replay.txt"
grep -q '^Severity:' "${work_dir}/replay.txt"
grep -q '^Confidence:' "${work_dir}/replay.txt"
grep -q '^Observation window:' "${work_dir}/replay.txt"
grep -q '^Counter window:' "${work_dir}/replay.txt"
grep -q '^Caveats:' "${work_dir}/replay.txt"
"${cli}" "${cli_args[@]}" capture --namespace "${namespace}" --pod "${collector_pod}" --output "${work_dir}/incident-after.json" > "${work_dir}/capture-after.txt"
"${cli}" compare --before "${work_dir}/incident.json" --after "${work_dir}/incident-after.json" --pod "${namespace}/${collector_pod}" > "${work_dir}/compare-incidents.txt"
grep -q '^Incident comparison:' "${work_dir}/compare-incidents.txt"
"${cli}" compare --before "${work_dir}/incident.json" --after "${work_dir}/incident-after.json" --workload "${namespace}/deployment/kube-memlens-collector" > "${work_dir}/compare-workload-incidents.txt"
grep -q '^Workload incident comparison:' "${work_dir}/compare-workload-incidents.txt"

KUBECONFIG="${kubeconfig}" kubectl get --raw \
  "/api/v1/namespaces/${namespace}/services/http:kube-memlens-collector:8080/proxy/metrics" \
  > "${work_dir}/collector-metrics.txt"
grep -q '^kubememlens_collector_ingestion_requests_total' "${work_dir}/collector-metrics.txt"
grep -q 'kind="history_points"' "${work_dir}/collector-metrics.txt"
KUBECONFIG="${kubeconfig}" kubectl get --raw \
  "/api/v1/namespaces/${namespace}/pods/${agent_pod}:8082/proxy/metrics" \
  > "${work_dir}/agent-metrics.txt"
grep -q '^kubememlens_agent_scans_total' "${work_dir}/agent-metrics.txt"
run_tui_smoke
run_live_density_smoke

helm upgrade kube-memlens ./charts/kube-memlens \
  --kubeconfig "${kubeconfig}" \
  --namespace "${namespace}" \
  --reuse-values \
  --set agent.interval=6s \
  --wait \
  --timeout 3m
KUBECONFIG="${kubeconfig}" kubectl rollout status daemonset/kube-memlens-agent -n "${namespace}" --timeout=2m
KUBECONFIG="${kubeconfig}" kubectl rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=2m

helm rollback kube-memlens 1 \
  --kubeconfig "${kubeconfig}" \
  --namespace "${namespace}" \
  --wait \
  --timeout 3m
KUBECONFIG="${kubeconfig}" kubectl rollout status daemonset/kube-memlens-agent -n "${namespace}" --timeout=2m
KUBECONFIG="${kubeconfig}" kubectl rollout status deployment/kube-memlens-collector -n "${namespace}" --timeout=2m
wait_for_doctor

helm uninstall kube-memlens --kubeconfig "${kubeconfig}" --namespace "${namespace}" --wait
if KUBECONFIG="${kubeconfig}" kubectl get clusterrole kube-memlens-agent >/dev/null 2>&1; then
  echo "cluster role remains after uninstall" >&2
  exit 1
fi
if KUBECONFIG="${kubeconfig}" kubectl get clusterrolebinding kube-memlens-agent >/dev/null 2>&1; then
  echo "cluster role binding remains after uninstall" >&2
  exit 1
fi

echo "kind end-to-end test passed"
