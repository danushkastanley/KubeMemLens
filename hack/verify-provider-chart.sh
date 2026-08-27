#!/usr/bin/env bash

set -Eeuo pipefail

chart=charts/kube-memlens
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-provider-chart.XXXXXX")

cleanup() {
  local status=$?
  trap - EXIT
  if [[ "${work_dir}" == "${TMPDIR:-/tmp}/kube-memlens-provider-chart."* ]]; then
    rm -rf -- "${work_dir}"
  fi
  exit "${status}"
}
trap cleanup EXIT

fail() {
  echo "provider chart check failed: $*" >&2
  exit 1
}

require_text() {
  local file=$1
  local value=$2
  grep -Fq -- "${value}" "${file}" || fail "${file} is missing ${value}"
}

render_template() {
  local template=$1
  local output=$2
  helm template kube-memlens "${chart}" --show-only "templates/${template}" > "${output}"
}

for command in grep helm; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done

helm lint "${chart}" >/dev/null
render_template daemonset.yaml "${work_dir}/daemonset.yaml"
render_template deployment.yaml "${work_dir}/deployment.yaml"
render_template extension-cert-bootstrap.yaml "${work_dir}/bootstrap.yaml"
render_template networkpolicy.yaml "${work_dir}/networkpolicy.yaml"
render_template service.yaml "${work_dir}/service.yaml"

for workload in daemonset deployment bootstrap; do
  manifest="${work_dir}/${workload}.yaml"
  require_text "${manifest}" "automountServiceAccountToken: false"
  require_text "${manifest}" "kubernetes.io/os: linux"
  require_text "${manifest}" "runAsNonRoot: true"
  require_text "${manifest}" "runAsUser: 65532"
  require_text "${manifest}" "runAsGroup: 65532"
  require_text "${manifest}" "type: RuntimeDefault"
  require_text "${manifest}" "privileged: false"
  require_text "${manifest}" "readOnlyRootFilesystem: true"
  require_text "${manifest}" "allowPrivilegeEscalation: false"
  require_text "${manifest}" 'drop: ["ALL"]'
done

require_text "${work_dir}/daemonset.yaml" "path: \"/sys/fs/cgroup\""
require_text "${work_dir}/daemonset.yaml" "type: Directory"
require_text "${work_dir}/daemonset.yaml" "mountPath: /host/sys/fs/cgroup"
require_text "${work_dir}/daemonset.yaml" "readOnly: true"
require_text "${work_dir}/daemonset.yaml" "expirationSeconds: 3600"
require_text "${work_dir}/deployment.yaml" "expirationSeconds: 3600"
require_text "${work_dir}/bootstrap.yaml" "expirationSeconds: 600"

require_text "${work_dir}/networkpolicy.yaml" "policyTypes:"
require_text "${work_dir}/networkpolicy.yaml" "- Ingress"
require_text "${work_dir}/networkpolicy.yaml" "port: http"
require_text "${work_dir}/networkpolicy.yaml" "port: extension"
require_text "${work_dir}/service.yaml" "port: 443"
require_text "${work_dir}/service.yaml" "targetPort: extension"
if grep -Eq 'port: (8080|8081)' "${work_dir}/service.yaml"; then
  fail "collector Service exposes a plaintext port"
fi

if helm template kube-memlens "${chart}" --set security.privileged=true >/dev/null 2>&1; then
  fail "privileged standard profile rendered"
fi
if helm template kube-memlens "${chart}" --set security.readOnlyRootFilesystem=false >/dev/null 2>&1; then
  fail "writable-root standard profile rendered"
fi

echo "provider chart contract passed"
