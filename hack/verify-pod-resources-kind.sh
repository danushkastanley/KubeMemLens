#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${root}"
kubeconfig=${POD_RESOURCE_KUBECONFIG:?POD_RESOURCE_KUBECONFIG is required}
context=${POD_RESOURCE_CONTEXT:?POD_RESOURCE_CONTEXT is required}
cli=${POD_RESOURCE_CLI:?POD_RESOURCE_CLI is required}
collector_namespace=${POD_RESOURCE_COLLECTOR_NAMESPACE:-kube-memlens}
artifact_dir=${POD_RESOURCE_ARTIFACT_DIR:?POD_RESOURCE_ARTIFACT_DIR is required}
namespace=kube-memlens-resource-e2e
pod=memory-budget
legacy_commit=c13457818c598e8b66bd2c6048a0af5a4eb3666e
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-resources.XXXXXX")
created=false
restore_images=false
collector_image=
agent_image=

case "${context}" in kind-*) ;; *) echo 'resource verification requires a kind context' >&2; exit 1 ;; esac
test "${POD_RESOURCE_ACKNOWLEDGE:-}" = run-and-remove-pod-resource-fixtures || {
  echo 'set POD_RESOURCE_ACKNOWLEDGE=run-and-remove-pod-resource-fixtures' >&2
  exit 1
}
for command in git go kubectl jq tar docker kind; do command -v "${command}" >/dev/null; done
test -x "${cli}"
mkdir -p "${artifact_dir}"
chmod 700 "${artifact_dir}"

kctl() { kubectl --kubeconfig "${kubeconfig}" --context "${context}" "$@"; }
cli_args=(--connect-mode kubernetes-api --kubeconfig "${kubeconfig}" --context "${context}" --collector-namespace "${collector_namespace}")

cleanup() {
  local result=$?
  if [ "${restore_images}" = true ]; then
    kctl set image deployment/kube-memlens-collector -n "${collector_namespace}" "collector=${collector_image}" >/dev/null || result=1
    kctl set image daemonset/kube-memlens-agent -n "${collector_namespace}" "agent=${agent_image}" >/dev/null || result=1
    kctl rollout status deployment/kube-memlens-collector -n "${collector_namespace}" --timeout=120s >/dev/null || result=1
    kctl rollout status daemonset/kube-memlens-agent -n "${collector_namespace}" --timeout=120s >/dev/null || result=1
  fi
  if [ "${created}" = true ]; then
    kctl delete namespace "${namespace}" --wait=true --timeout=90s >/dev/null || result=1
  fi
  rm -rf "${work_dir}"
  exit "${result}"
}
trap cleanup EXIT
trap 'echo "Pod resource verification failed at line ${LINENO}" >&2' ERR
"${cli}" "${cli_args[@]}" --help >/dev/null
if kctl get namespace "${namespace}" >/dev/null 2>&1; then
  echo 'refusing to replace existing resource fixture namespace' >&2
  exit 1
fi

# Compile the exact previous client to exercise its strict decoder, rather than
# assuming a JSON-key check is equivalent to compatibility with a real reader.
if ! git cat-file -e "${legacy_commit}^{commit}" 2>/dev/null; then
  git fetch --no-tags --depth=1 origin "${legacy_commit}"
fi
mkdir "${work_dir}/legacy"
git archive "${legacy_commit}" | tar -x -C "${work_dir}/legacy"
(cd "${work_dir}/legacy" && go build -trimpath -o "${work_dir}/legacy-cli" ./cmd/kubectl-memlens)

kctl create namespace "${namespace}" >/dev/null
created=true
kctl apply -n "${namespace}" -f - >/dev/null <<'YAML'
apiVersion: v1
kind: Pod
metadata:
  name: memory-budget
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    runAsGroup: 1000
    fsGroup: 1000
    seccompProfile:
      type: RuntimeDefault
  resources:
    requests:
      memory: 128Mi
    limits:
      memory: 256Mi
  containers:
    - name: app
      image: public.ecr.aws/docker/library/busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
      command: ["sh", "-c", "exec sleep 86400"]
      resources:
        requests:
          memory: 32Mi
        limits:
          memory: 64Mi
      resizePolicy:
        - resourceName: memory
          restartPolicy: NotRequired
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
    - name: shared
      image: public.ecr.aws/docker/library/busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
      command: ["sh", "-c", "exec sleep 86400"]
      resources:
        requests:
          memory: 32Mi
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities:
          drop: ["ALL"]
      volumeMounts:
        - name: cache
          mountPath: /cache
  volumes:
    - name: cache
      emptyDir:
        medium: Memory
        sizeLimit: 64Mi
YAML
kctl wait --for=condition=Ready "pod/${pod}" -n "${namespace}" --timeout=120s >/dev/null
original_uid=$(kctl get pod "${pod}" -n "${namespace}" -o jsonpath='{.metadata.uid}')
original_restarts=$(kctl get pod "${pod}" -n "${namespace}" -o json | jq '[.status.containerStatuses[].restartCount] | add')

wait_for_explanation() {
  local predicate=$1
  for _ in $(seq 1 60); do
    if "${cli}" "${cli_args[@]}" explain pod "${pod}" -n "${namespace}" -o json > "${work_dir}/explanation.json" 2> "${work_dir}/read-error.txt" &&
      jq -e "${predicate}" "${work_dir}/explanation.json" >/dev/null; then
      return
    fi
    sleep 2
  done
  echo "resource explanation did not meet: ${predicate}" >&2
  jq '{schemaVersion,kubernetes:{resources:.kubernetes.resources,effectiveResources:.kubernetes.effectiveResources,memoryRequestBytes:.kubernetes.memoryRequestBytes},containers:[.containers[]? | {resources}]}' "${work_dir}/explanation.json" >&2 || true
  return 1
}

wait_for_explanation '.schemaVersion == 2 and .kubernetes.effectiveResources.request.bytes == 134217728 and .kubernetes.effectiveResources.limit.bytes == 268435456 and .kubernetes.memoryRequestBytes == 67108864'
"${cli}" "${cli_args[@]}" capture -n "${namespace}" --pod "${pod}" -o "${work_dir}/before.json" >/dev/null

kctl patch pod "${pod}" -n "${namespace}" --subresource=resize --type=strategic \
  -p '{"spec":{"resources":{"requests":{"memory":"192Mi"},"limits":{"memory":"384Mi"}}}}' >/dev/null
wait_for_explanation '.kubernetes.resources.configured.request.bytes == 201326592 and .kubernetes.resources.configured.limit.bytes == 402653184 and .kubernetes.resources.applied.limit.known == true and .kubernetes.resources.applied.limit.bytes == 402653184'

kctl patch pod "${pod}" -n "${namespace}" --subresource=resize --type=strategic \
  -p '{"spec":{"containers":[{"name":"app","resources":{"requests":{"memory":"48Mi"},"limits":{"memory":"96Mi"}}}]}}' >/dev/null
wait_for_explanation '.kubernetes.memoryRequestBytes == 83886080 and any(.containers[]; .name == "app" and .resources.applied.request.known == true and .resources.applied.request.bytes == 50331648)'
"${cli}" "${cli_args[@]}" capture -n "${namespace}" --pod "${pod}" -o "${work_dir}/after.json" >/dev/null
"${cli}" compare --before "${work_dir}/before.json" --after "${work_dir}/after.json" --pod "${namespace}/${pod}" > "${work_dir}/comparison.txt"
grep -Fq 'Pod configured limit: 256Mi -> 384Mi' "${work_dir}/comparison.txt"
"${cli}" replay "${work_dir}/after.json" --pod "${namespace}/${pod}" > "${work_dir}/replay.txt"
grep -Fq '384Mi (Pod configuration)' "${work_dir}/replay.txt"

# Kubernetes 1.37's PodResize admission plugin rejects requests larger than
# node allocatable before persisting them. Verify the rejection and unchanged
# applied budget; deferred/infeasible condition decoding uses mapper fixtures.
if kctl patch pod "${pod}" -n "${namespace}" --subresource=resize --type=strategic \
  -p '{"spec":{"resources":{"requests":{"memory":"1Ei"},"limits":{"memory":"1Ei"}}}}' \
  > "${work_dir}/oversized-request.txt" 2>&1; then
  echo 'oversized Pod resize unexpectedly passed admission' >&2
  exit 1
fi
grep -Fq "node didn't have enough allocatable resources" "${work_dir}/oversized-request.txt"
wait_for_explanation '.kubernetes.resources.configured.limit.bytes == 402653184 and .kubernetes.resources.applied.limit.bytes == 402653184 and (.kubernetes.resources.pending.state // "") == "" and (.kubernetes.resources.applying.state // "") == ""'

volume_resize=false
if [ "${POD_RESOURCE_TEST_VOLUME_RESIZE:-false}" = true ]; then
  kctl patch pod "${pod}" -n "${namespace}" --subresource=resize --type=strategic \
    -p '{"spec":{"volumes":[{"name":"cache","emptyDir":{"medium":"Memory","sizeLimit":"96Mi"}}]}}' >/dev/null
  wait_for_explanation '.kubernetes.memoryEmptyDirLimitBytes == 100663296'
  for _ in $(seq 1 30); do
    actual_kib=$(kctl exec "${pod}" -n "${namespace}" -c shared -- df -k /cache | awk 'NR == 2 {print $2}')
    [ "${actual_kib}" != 98304 ] || { volume_resize=true; break; }
    sleep 2
  done
  [ "${volume_resize}" = true ] || { echo 'tmpfs mount did not resize' >&2; exit 1; }
fi

"${work_dir}/legacy-cli" "${cli_args[@]}" explain pod "${pod}" -n "${namespace}" -o json > "${work_dir}/legacy-read.json"
jq -e '.schemaVersion == 1 and (.kubernetes | has("resources") | not)' "${work_dir}/legacy-read.json" >/dev/null
"${cli}" "${cli_args[@]}" capture -n "${namespace}" --pod "${pod}" --schema-version=1 -o "${work_dir}/legacy.json" >/dev/null
"${work_dir}/legacy-cli" replay "${work_dir}/legacy.json" --pod "${namespace}/${pod}" > "${work_dir}/legacy-replay.txt"
jq -e '.partial == true and any(.caveats[]; contains("omitted for incident schema 1"))' "${work_dir}/legacy.json" >/dev/null
grep -Fq "Pod: ${namespace}/${pod}" "${work_dir}/legacy-replay.txt"
"${cli}" replay "${work_dir}/legacy.json" --pod "${namespace}/${pod}" > "${work_dir}/current-legacy-replay.txt"
grep -Fq 'omitted for incident schema 1' "${work_dir}/current-legacy-replay.txt"
# Exercise both mixed-version deployment orders and a collector rollback while
# the new agent retains its previously negotiated epoch and schema.
legacy_image="kube-memlens:resource-legacy-${context#kind-}"
docker build -t "${legacy_image}" "${work_dir}/legacy" > "${work_dir}/legacy-build.log" 2>&1 || {
  tail -30 "${work_dir}/legacy-build.log" >&2
  exit 1
}
kind load docker-image "${legacy_image}" --name "${context#kind-}" >/dev/null
collector_image=$(kctl get deployment kube-memlens-collector -n "${collector_namespace}" -o jsonpath='{.spec.template.spec.containers[?(@.name=="collector")].image}')
agent_image=$(kctl get daemonset kube-memlens-agent -n "${collector_namespace}" -o jsonpath='{.spec.template.spec.containers[?(@.name=="agent")].image}')
restore_images=true
kctl set image deployment/kube-memlens-collector -n "${collector_namespace}" "collector=${legacy_image}" >/dev/null
kctl rollout status deployment/kube-memlens-collector -n "${collector_namespace}" --timeout=120s >/dev/null
wait_for_explanation '.schemaVersion == 1 and .kubernetes.memoryRequestBytes == 83886080'
kctl set image deployment/kube-memlens-collector -n "${collector_namespace}" "collector=${collector_image}" >/dev/null
kctl rollout status deployment/kube-memlens-collector -n "${collector_namespace}" --timeout=120s >/dev/null
wait_for_explanation '.schemaVersion == 2 and .kubernetes.resources.configured.limit.bytes == 402653184'
kctl set image daemonset/kube-memlens-agent -n "${collector_namespace}" "agent=${legacy_image}" >/dev/null
kctl rollout status daemonset/kube-memlens-agent -n "${collector_namespace}" --timeout=120s >/dev/null
wait_for_explanation '.schemaVersion == 1 and .kubernetes.memoryRequestBytes == 83886080'
kctl set image daemonset/kube-memlens-agent -n "${collector_namespace}" "agent=${agent_image}" >/dev/null
kctl rollout status daemonset/kube-memlens-agent -n "${collector_namespace}" --timeout=120s >/dev/null
wait_for_explanation '.schemaVersion == 2 and .kubernetes.resources.configured.limit.bytes == 402653184'
restore_images=false

test "$(kctl get pod "${pod}" -n "${namespace}" -o jsonpath='{.metadata.uid}')" = "${original_uid}"
test "$(kctl get pod "${pod}" -n "${namespace}" -o json | jq '[.status.containerStatuses[].restartCount] | add')" = "${original_restarts}"

source_dirty=false
[ -z "$(git status --porcelain)" ] || source_dirty=true
jq -n --arg commit "$(git rev-parse HEAD)" --arg version "$(kctl version -o json | jq -r '.serverVersion.gitVersion')" \
  --argjson volumeResize "${volume_resize}" --argjson sourceDirty "${source_dirty}" '{schemaVersion:1,outcome:"passed",sourceCommit:$commit,sourceDirty:$sourceDirty,kubernetesVersion:$version,
  checks:{podBudgetPrecedence:true,podResize:true,containerResize:true,oversizedRequestRejected:true,appliedBudgetPreserved:true,
  captureReplay:true,resourceComparison:true,legacyLiveReader:true,legacyCaptureReader:true,mixedAgentCollectorVersions:true,collectorRollback:true,
  samePodInstance:true,noContainerRestarts:true,memoryBackedVolumeResize:$volumeResize},
  privacy:{runtimeIdentifiersIncluded:false,credentialsRetained:false}}' > "${artifact_dir}/pod-resource-summary.json"
echo 'Pod resource and resize kind verification passed'
