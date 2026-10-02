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
  shift 2
  helm template kube-memlens "${chart}" --show-only "templates/${template}" "$@" > "${output}"
}

for command in grep helm ruby cmp; do
  command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"
done

helm lint "${chart}" >/dev/null
render_template daemonset.yaml "${work_dir}/daemonset.yaml"
render_template deployment.yaml "${work_dir}/deployment.yaml"
render_template extension-cert-bootstrap.yaml "${work_dir}/bootstrap.yaml"
render_template networkpolicy.yaml "${work_dir}/networkpolicy.yaml"
render_template service.yaml "${work_dir}/service.yaml"

# Keep heap page behaviour explicit on both long-running standard services.
ruby -ryaml - "${work_dir}/daemonset.yaml" "${work_dir}/deployment.yaml" <<'RUBY'
ARGV.each do |path|
  workload = YAML.safe_load(File.read(path), aliases: true)
  containers = workload.dig("spec", "template", "spec", "containers")
  service = containers.find { |container| %w[agent collector].include?(container.fetch("name")) }
  abort "standard service missing" unless service
  settings = service.fetch("env", []).select { |entry| entry.fetch("name") == "GODEBUG" }
  abort "standard heap page setting differs" unless settings == [{"name" => "GODEBUG", "value" => "disablethp=1"}]
end
RUBY

# Collector placement must preserve Linux and all unrelated workload settings.
render_template deployment.yaml "${work_dir}/collector-without-new-value.yaml" --set collector.nodeSelector=null
cmp "${work_dir}/deployment.yaml" "${work_dir}/collector-without-new-value.yaml" || fail 'omitted collector selector changed defaults'
collector_placement=(--set-string 'collector.nodeSelector.kubernetes\.io/hostname=ip-10-0-0-1.ec2.internal'
                     --set-string 'collector.nodeSelector.qualification=true')
render_template deployment.yaml "${work_dir}/collector-placement.yaml" "${collector_placement[@]}"
render_template daemonset.yaml "${work_dir}/agent-placement.yaml" "${collector_placement[@]}"
cmp "${work_dir}/daemonset.yaml" "${work_dir}/agent-placement.yaml" || fail 'collector placement changed agent configuration'
ruby -ryaml - "${work_dir}/deployment.yaml" "${work_dir}/collector-placement.yaml" <<'RUBY'
normal, placed = ARGV.map { |path| YAML.safe_load(File.read(path), aliases: true) }
expected = {"kubernetes.io/os" => "linux", "kubernetes.io/hostname" => "ip-10-0-0-1.ec2.internal", "qualification" => "true"}
abort "collector selector differs from requested string labels" unless placed.dig("spec", "template", "spec", "nodeSelector") == expected
placed.fetch("spec").fetch("template").fetch("spec")["nodeSelector"] = normal.dig("spec", "template", "spec", "nodeSelector")
abort "collector placement changed unrelated configuration" unless placed == normal
RUBY
for validation in schema template; do
  options=(--set-string 'collector.nodeSelector.kubernetes\.io/os=windows')
  if [[ "${validation}" == template ]]; then options+=(--skip-schema-validation); fi
  if helm template kube-memlens "${chart}" "${options[@]}" >"${work_dir}/placement-rejected.log" 2>&1; then
    fail 'collector placement accepted a non-Linux selector'
  fi
  # Helm 3.22 uses JSON pointers; earlier versions use dotted schema paths.
  require_text "${work_dir}/placement-rejected.log" 'nodeSelector'
  require_text "${work_dir}/placement-rejected.log" 'linux'
done
if helm template kube-memlens "${chart}" --set collector.nodeSelector.qualification=true >"${work_dir}/placement-rejected.log" 2>&1; then
  fail 'collector placement accepted a non-string label value'
fi
require_text "${work_dir}/placement-rejected.log" 'nodeSelector'
require_text "${work_dir}/placement-rejected.log" 'string'

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
require_text "${work_dir}/service.yaml" 'name: "https-extension"'
require_text "${work_dir}/service.yaml" "targetPort: extension"
if grep -Eq 'port: (8080|8081)' "${work_dir}/service.yaml"; then
  fail "collector Service exposes a plaintext port"
fi

# Provider mappings must agree across the listener, Service, aggregation route
# and the actual connectivity probe, without exposing the plaintext listener.
for mapping in eks custom; do
  port=8443
  options=(--values hack/provider-values/eks-al2023-containerd-amd64.yaml)
  if [ "${mapping}" = custom ]; then
    port=9443
    options+=(--set collector.ingestion.extensionPort=9443 --set collector.service.extensionPort=9443)
  fi
  for template in service.yaml deployment.yaml extension-tls.yaml tests/test-connection.yaml; do
    render_template "${template}" "${work_dir}/${mapping}-${template##*/}" "${options[@]}"
  done
  require_text "${work_dir}/${mapping}-service.yaml" 'name: "extension"'
  require_text "${work_dir}/${mapping}-service.yaml" "port: ${port}"
  require_text "${work_dir}/${mapping}-service.yaml" "targetPort: extension"
  require_text "${work_dir}/${mapping}-deployment.yaml" "--extension-port=${port}"
  require_text "${work_dir}/${mapping}-deployment.yaml" "containerPort: ${port}"
  require_text "${work_dir}/${mapping}-extension-tls.yaml" "port: ${port}"
  require_text "${work_dir}/${mapping}-test-connection.yaml" ".svc ${port}; do"
done

# A valid port name that YAML also recognises as a boolean must remain a string.
render_template service.yaml "${work_dir}/string-port-name.yaml" --set-string collector.service.extensionPortName=true
require_text "${work_dir}/string-port-name.yaml" 'name: "true"'

for setting in collector.service.extensionPort=0 collector.service.extensionPort=65536 \
  collector.service.extensionPortName=bad.name collector.service.extensionPortName=; do
  if helm template kube-memlens "${chart}" --set "${setting}" >/dev/null 2>&1; then
    fail "invalid authenticated Service setting rendered: ${setting}"
  fi
done

if helm template kube-memlens "${chart}" --set security.privileged=true >/dev/null 2>&1; then
  fail "privileged standard profile rendered"
fi
if helm template kube-memlens "${chart}" --set security.readOnlyRootFilesystem=false >/dev/null 2>&1; then
  fail "writable-root standard profile rendered"
fi

echo "provider chart contract passed"
