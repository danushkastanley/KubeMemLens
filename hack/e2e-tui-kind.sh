#!/usr/bin/env bash

set -Eeuo pipefail

required_acknowledgement=run-and-remove-kube-memlens-tui-smoke
context=${TUI_E2E_CONTEXT:-}
kubeconfig=${TUI_E2E_KUBECONFIG:-${KUBECONFIG:-}}
cli=${TUI_E2E_CLI:-}
collector_namespace=${TUI_E2E_COLLECTOR_NAMESPACE:-kube-memlens}
image=${TUI_E2E_WORKLOAD_IMAGE:-}
artifact_dir=${TUI_E2E_ARTIFACT_DIR:-}
namespace_prefix=kube-memlens-tui
namespaces=("${namespace_prefix}-alpha" "${namespace_prefix}-beta" "${namespace_prefix}-ops")
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-tui-e2e.XXXXXX")
created_namespaces=()
outcome=failed

fail() {
  echo "TUI smoke error: $*" >&2
  exit 1
}

for command in kubectl jq expect; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done
[ "${TUI_E2E_ACKNOWLEDGE:-}" = "${required_acknowledgement}" ] || fail "set TUI_E2E_ACKNOWLEDGE=${required_acknowledgement}"
[ -n "${context}" ] || fail "TUI_E2E_CONTEXT is required"
[ -f "${kubeconfig}" ] || fail "TUI_E2E_KUBECONFIG must be a file"
[ -x "${cli}" ] || fail "TUI_E2E_CLI must be executable"
[[ "${image}" =~ @sha256:[a-f0-9]{64}$ ]] || fail "TUI_E2E_WORKLOAD_IMAGE must be digest-pinned"
[ -n "${artifact_dir}" ] || fail "TUI_E2E_ARTIFACT_DIR is required"

k() { kubectl --kubeconfig "${kubeconfig}" --context "${context}" "$@"; }

write_summary() {
  mkdir -p "${artifact_dir}"
  jq -n --arg outcome "${outcome}" --arg completedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
    {schemaVersion: 1, outcome: $outcome, completedAt: $completedAt,
     workload: {pods: 20, namespaces: 3, workloadKinds: ["Deployment", "StatefulSet"], multiContainerPods: 3},
     terminalSizesCoveredByLivePTY: ["80x24", "120x30", "180x50"],
     checks: ["open", "long-list navigation", "risk sort", "node view", "filter", "Pod detail", "live history refresh", "detail scroll", "pause/resume", "manual refresh", "read-only recommendation", "clean exit"],
     privacy: {rawPTYRetained: false, clusterIdentifiersIncluded: false, workloadPodNamesIncluded: false}}' \
    > "${artifact_dir}/tui-smoke-summary.json"
  chmod 600 "${artifact_dir}/tui-smoke-summary.json"
}

cleanup() {
  status=$?
  trap - EXIT
  cleanup_failed=0
  for namespace in "${created_namespaces[@]}"; do
    owner=$(k get namespace "${namespace}" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}' 2>/dev/null || true)
    if [ "${owner}" = kube-memlens-tui-e2e ]; then
      k delete namespace "${namespace}" --wait=true --timeout=2m >/dev/null 2>&1 || cleanup_failed=1
    fi
  done
  if [ "${cleanup_failed}" -ne 0 ]; then
    outcome=failed
    status=1
  fi
  write_summary
  if [[ "${work_dir}" == "${TMPDIR:-/tmp}/kube-memlens-tui-e2e."* ]]; then
    rm -rf -- "${work_dir}"
  fi
  exit "${status}"
}
trap cleanup EXIT

k config get-contexts "${context}" -o name | grep -Fxq "${context}" || fail "context not found"
k get deployment kube-memlens-collector -n "${collector_namespace}" >/dev/null || fail "collector not installed"
for namespace in "${namespaces[@]}"; do
  if k get namespace "${namespace}" >/dev/null 2>&1; then
    fail "namespace already exists: ${namespace}"
  fi
  k create namespace "${namespace}" >/dev/null
  created_namespaces+=("${namespace}")
  k label namespace "${namespace}" \
    app.kubernetes.io/managed-by=kube-memlens-tui-e2e \
    pod-security.kubernetes.io/enforce=restricted >/dev/null
done

cat > "${work_dir}/workloads.yaml" <<EOF
apiVersion: apps/v1
kind: Deployment
metadata: {name: api, namespace: ${namespace_prefix}-alpha}
spec:
  replicas: 7
  selector: {matchLabels: {app: tui-api}}
  template:
    metadata: {labels: {app: tui-api, app.kubernetes.io/part-of: kube-memlens-tui-e2e}}
    spec:
      automountServiceAccountToken: false
      securityContext: {seccompProfile: {type: RuntimeDefault}}
      containers:
        - name: app
          image: ${image}
          command: ["/bin/sh", "-c", "exec sleep 3600"]
          resources: {requests: {cpu: 1m, memory: 2Mi}, limits: {memory: 32Mi}}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, runAsNonRoot: true, runAsUser: 65532, capabilities: {drop: ["ALL"]}}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: worker, namespace: ${namespace_prefix}-alpha}
spec:
  replicas: 4
  selector: {matchLabels: {app: tui-worker}}
  template:
    metadata: {labels: {app: tui-worker, app.kubernetes.io/part-of: kube-memlens-tui-e2e}}
    spec:
      automountServiceAccountToken: false
      securityContext: {seccompProfile: {type: RuntimeDefault}}
      containers:
        - name: worker
          image: ${image}
          command: ["/bin/sh", "-c", "exec sleep 3600"]
          resources: {requests: {cpu: 1m, memory: 2Mi}, limits: {memory: 32Mi}}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, runAsNonRoot: true, runAsUser: 65532, capabilities: {drop: ["ALL"]}}
---
apiVersion: v1
kind: Service
metadata: {name: cache, namespace: ${namespace_prefix}-beta}
spec: {clusterIP: None, selector: {app: tui-cache}, ports: [{name: peer, port: 8080}]}
---
apiVersion: apps/v1
kind: StatefulSet
metadata: {name: cache, namespace: ${namespace_prefix}-beta}
spec:
  serviceName: cache
  replicas: 4
  selector: {matchLabels: {app: tui-cache}}
  template:
    metadata: {labels: {app: tui-cache, app.kubernetes.io/part-of: kube-memlens-tui-e2e}}
    spec:
      automountServiceAccountToken: false
      securityContext: {seccompProfile: {type: RuntimeDefault}}
      volumes: [{name: cache, emptyDir: {}}]
      containers:
        - name: cache
          image: ${image}
          command: ["/bin/sh", "-c", "dd if=/dev/zero of=/cache/blob bs=1M count=4 2>/dev/null; while true; do cat /cache/blob >/dev/null; sleep 2; done"]
          resources: {requests: {cpu: 1m, memory: 2Mi}, limits: {memory: 32Mi}}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, runAsNonRoot: true, runAsUser: 65532, capabilities: {drop: ["ALL"]}}
          volumeMounts: [{name: cache, mountPath: /cache}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: sidecars, namespace: ${namespace_prefix}-beta}
spec:
  replicas: 3
  selector: {matchLabels: {app: tui-sidecars}}
  template:
    metadata: {labels: {app: tui-sidecars, app.kubernetes.io/part-of: kube-memlens-tui-e2e}}
    spec:
      automountServiceAccountToken: false
      securityContext: {seccompProfile: {type: RuntimeDefault}}
      volumes: [{name: memory, emptyDir: {medium: Memory, sizeLimit: 8Mi}}]
      containers:
        - name: app
          image: ${image}
          command: ["/bin/sh", "-c", "exec sleep 3600"]
          resources: {requests: {cpu: 1m, memory: 2Mi}, limits: {memory: 32Mi}}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, runAsNonRoot: true, runAsUser: 65532, capabilities: {drop: ["ALL"]}}
        - name: memory-sidecar
          image: ${image}
          command: ["/bin/sh", "-c", "dd if=/dev/zero of=/memory/blob bs=1M count=4 2>/dev/null; exec sleep 3600"]
          resources: {requests: {cpu: 1m, memory: 2Mi}, limits: {memory: 32Mi}}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, runAsNonRoot: true, runAsUser: 65532, capabilities: {drop: ["ALL"]}}
          volumeMounts: [{name: memory, mountPath: /memory}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: batch, namespace: ${namespace_prefix}-ops}
spec:
  replicas: 2
  selector: {matchLabels: {app: tui-batch}}
  template:
    metadata: {labels: {app: tui-batch, app.kubernetes.io/part-of: kube-memlens-tui-e2e}}
    spec:
      automountServiceAccountToken: false
      securityContext: {seccompProfile: {type: RuntimeDefault}}
      containers:
        - name: batch
          image: ${image}
          command: ["/bin/sh", "-c", "exec sleep 3600"]
          resources: {requests: {cpu: 1m, memory: 2Mi}, limits: {memory: 32Mi}}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, runAsNonRoot: true, runAsUser: 65532, capabilities: {drop: ["ALL"]}}
EOF

k apply -f "${work_dir}/workloads.yaml" >/dev/null
for namespace in "${namespaces[@]}"; do
  k wait --for=condition=Ready pod -n "${namespace}" -l app.kubernetes.io/part-of=kube-memlens-tui-e2e --timeout=5m >/dev/null
done

deadline=$((SECONDS + 300))
mapped=0
while [ "${SECONDS}" -lt "${deadline}" ]; do
  mapped=$("${cli}" --kubeconfig "${kubeconfig}" --context "${context}" --collector-namespace "${collector_namespace}" \
    top pods --all-namespaces --output json 2>/dev/null \
    | jq '[.[] | select(.namespace | startswith("kube-memlens-tui-"))] | length' || true)
  [ "${mapped:-0}" -eq 20 ] && break
  sleep 5
done
[ "${mapped:-0}" -eq 20 ] || fail "expected 20 mapped workload Pods, got ${mapped:-0}"

for terminal_size in 80x24 120x30 180x50; do
  columns=${terminal_size%x*}
  rows=${terminal_size#*x}
  mode=layout
  [ "${terminal_size}" = 80x24 ] && mode=full
  "$(dirname "$0")/tui-smoke.exp" "${cli}" "${kubeconfig}" "${context}" "${collector_namespace}" "${columns}" "${rows}" "${mode}"
done
outcome=passed
echo "local TUI smoke passed with 20 workload Pods"
