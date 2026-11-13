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

accepts() {
  local description=$1
  shift
  if ! helm template kube-memlens charts/kube-memlens "$@" > /dev/null 2> "${work_dir}/error.txt"; then
    echo "chart refused ${description}:" >&2
    cat "${work_dir}/error.txt" >&2
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

accepts 'the default values'
accepts 'a rotation window just inside the validity' \
  --set extensionTLS.validityDays=30 --set extensionTLS.rotateBefore=719h
accepts 'a snapshot lifetime equal to the agent interval' \
  --set collector.snapshotTTL=1m --set agent.interval=60s
accepts 'a short snapshot lifetime with the agent disabled' \
  --set agent.enabled=false --set collector.snapshotTTL=1s
accepts 'metrics disabled without alert rules' --set metrics.enabled=false
accepts 'alert rules with metrics' --set metrics.prometheusRule.enabled=true
echo 'chart value guards passed; no resources installed'
