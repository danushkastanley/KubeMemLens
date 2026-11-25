#!/usr/bin/env bash
# Value combinations the schema cannot express must be refused when the chart
# renders, with a message that names the settings, not fail later at runtime.
set -Eeuo pipefail
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-value-guards.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT

rejects() {
  local description=$1 expected=$2
  shift 2
  if helm template kube-memlens charts/kube-memlens "$@" > /dev/null 2> "${work_dir}/error.txt"; then
    echo "chart rendered ${description}" >&2
    exit 1
  fi
  if ! grep -Fq "${expected}" "${work_dir}/error.txt"; then
    echo "chart refused ${description} without naming the cause:" >&2
    cat "${work_dir}/error.txt" >&2
    exit 1
  fi
}

# bootstrap_render fails unless a render with a bootstrap-owned Secret is the
# same twice and holds no key material: that is what makes it safe to render
# without cluster access.
bootstrap_render() {
  local first="${work_dir}/bootstrap-1.yaml" second="${work_dir}/bootstrap-2.yaml" unwanted
  helm template kube-memlens charts/kube-memlens --set extensionTLS.source=bootstrap > "${first}"
  helm template kube-memlens charts/kube-memlens --set extensionTLS.source=bootstrap > "${second}"
  if ! cmp -s "${first}" "${second}"; then
    echo 'two renders with extensionTLS.source=bootstrap differ:' >&2
    diff "${first}" "${second}" | cut -c1-80 >&2
    exit 1
  fi
  for unwanted in 'caBundle:' 'kind: Secret' 'BEGIN '; do
    if grep -Fq "${unwanted}" "${first}"; then
      echo "render with extensionTLS.source=bootstrap contains ${unwanted}" >&2
      exit 1
    fi
  done
  if ! grep -Fq 'name: kube-memlens-cert-prepare' "${first}"; then
    echo 'render with extensionTLS.source=bootstrap has no Job that creates the Secret before the workloads' >&2
    exit 1
  fi
}

accepts() {
  local description=$1
  shift
  if ! helm template kube-memlens charts/kube-memlens "$@" > /dev/null 2> "${work_dir}/error.txt"; then
    echo "chart refused ${description}:" >&2
    cat "${work_dir}/error.txt" >&2
    exit 1
  fi
}

# renders_without fails when the rendered chart contains the given text.
renders_without() {
  local description=$1 unwanted=$2
  shift 2
  helm template kube-memlens charts/kube-memlens "$@" > "${work_dir}/rendered.yaml"
  if grep -Fq "${unwanted}" "${work_dir}/rendered.yaml"; then
    echo "chart rendered ${description}:" >&2
    grep -Fn -B3 "${unwanted}" "${work_dir}/rendered.yaml" >&2
    exit 1
  fi
}

renders_with() {
  local description=$1 wanted=$2
  shift 2
  helm template kube-memlens charts/kube-memlens "$@" > "${work_dir}/rendered.yaml"
  if ! grep -Fq "${wanted}" "${work_dir}/rendered.yaml"; then
    echo "chart did not render ${description}: no line contains ${wanted}" >&2
    exit 1
  fi
}

rejects 'a rotation window as long as the certificate validity' 'extensionTLS.rotateBefore' \
  --set extensionTLS.validityDays=30 --set extensionTLS.rotateBefore=720h
rejects 'a rotation window longer than the certificate validity' 'extensionTLS.rotateBefore' \
  --set extensionTLS.validityDays=1 --set extensionTLS.rotateBefore=90000s
rejects 'the health and extension listeners on one port' 'collector.ingestion.extensionPort' \
  --set collector.service.port=8443
rejects 'a snapshot lifetime shorter than the agent interval' 'collector.snapshotTTL' \
  --set collector.snapshotTTL=4s --set agent.interval=5s
rejects 'a snapshot lifetime shorter than the agent interval in mixed units' 'collector.snapshotTTL' \
  --set collector.snapshotTTL=900ms --set agent.interval=1s
rejects 'alert rules without metrics' 'metrics.prometheusRule.enabled' \
  --set metrics.enabled=false --set metrics.prometheusRule.enabled=true
rejects 'a bootstrap-owned Secret in a namespace the chart creates' 'extensionTLS.source=bootstrap' \
  --set extensionTLS.source=bootstrap --set namespace.create=true
bootstrap_render

renders_with 'the chosen Pod Security level on a namespace the chart creates' 'pod-security.kubernetes.io/enforce: "privileged"' \
  --set namespace.create=true --set namespace.podSecurityEnforce=privileged
renders_without 'a Pod Security level nobody chose' 'pod-security.kubernetes.io/enforce' \
  --set namespace.create=true
rejects 'a Pod Security level that does not exist' 'podSecurityEnforce' \
  --set namespace.create=true --set namespace.podSecurityEnforce=open

accepts 'the default values'
accepts 'a rotation window just inside the validity' \
  --set extensionTLS.validityDays=30 --set extensionTLS.rotateBefore=719h
accepts 'a snapshot lifetime equal to the agent interval' \
  --set collector.snapshotTTL=1m --set agent.interval=60s
accepts 'a short snapshot lifetime with the agent disabled' \
  --set agent.enabled=false --set collector.snapshotTTL=1s
accepts 'metrics disabled without alert rules' --set metrics.enabled=false
accepts 'alert rules with metrics' --set metrics.prometheusRule.enabled=true
renders_without 'agent RBAC with the agent disabled' 'name: kube-memlens-agent' --set agent.enabled=false
renders_with 'agent RBAC with the agent enabled' 'name: kube-memlens-agent'
# The collector has its own token lifetime: 7200 must appear, and with the agent
# disabled it can only come from the collector Deployment.
renders_with 'the collector token lifetime from its own value' 'expirationSeconds: 7200' \
  --set agent.enabled=false --set collector.tokenExpirationSeconds=7200
renders_without 'the agent token lifetime on the collector' 'expirationSeconds: 900' \
  --set agent.enabled=false --set agent.tokenExpirationSeconds=900
echo 'chart value guards passed; no resources installed'
