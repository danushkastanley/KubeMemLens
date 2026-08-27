#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

require_text() {
  local file=$1
  local text=$2
  grep -Fq "$text" "$file" || {
    echo "support contract check failed: ${file} is missing: ${text}" >&2
    exit 1
  }
}

reject_text() {
  local file=$1
  local text=$2
  if grep -Fq "$text" "$file"; then
    echo "support contract check failed: ${file} contains obsolete text: ${text}" >&2
    exit 1
  fi
}

contract=docs/compatibility.md

require_text "$contract" '# Support and compatibility contract'
require_text "$contract" '| GKE Autopilot | Unsupported for deep mode |'
require_text "$contract" '| EKS Fargate | Unsupported for deep mode |'
require_text "$contract" '| AKS standard node pools | Unsupported for the recorded candidate |'
require_text "$contract" '| AKS virtual nodes | Unsupported for deep mode |'
require_text "$contract" '| Windows worker nodes | Unsupported for deep mode |'
require_text "$contract" '| cgroup v1 | Unsupported |'
require_text "$contract" 'Shared multi-tenant clusters are a mandatory v1 threat environment.'
require_text "$contract" 'exactly one collector replica holds independent in-memory state'
require_text "$contract" 'Default Pod history is retained in collector memory for at most 15 minutes'
require_text "$contract" 'Configured store ceilings are rejection bounds, not scale claims.'
require_text "$contract" "\`reviewDueAt\` reports advisory freshness"
require_text "$contract" 'Live-cloud qualification is not scheduled, run in CI or repeated automatically for releases.'
require_text "$contract" 'PROD-002 to PROD-005'
require_text "$contract" 'PROD-006'
require_text "$contract" 'PROD-007'
require_text "$contract" 'PROD-008'
require_text "$contract" 'PROD-009'
require_text "$contract" 'PROD-010 and PROD-012'

require_text README.md 'docs/compatibility.md'
require_text README.md 'not suitable for shared multi-tenant clusters'
require_text SECURITY.md 'docs/compatibility.md'
require_text docs/installation.md 'compatibility.md'
require_text docs/security-model.md 'compatibility.md'
require_text charts/kube-memlens/README.md 'docs/compatibility.md'
require_text docs/qualification.md 'provider-qualification.json'
require_text docs/qualification.md 'az aks nodepool delete-machines'
require_text docs/qualification.md 'az aks nodepool scale --node-count 3'
require_text docs/qualification.md 'all five supported and six'
require_text docs/qualification.md 'Stale evidence remains valid historical proof'
reject_text docs/qualification.md 'az vmss reimage'
reject_text docs/qualification.md 'all six supported and five'
require_text docs/provider-qualification-sources.md 'Review date: 2026-08-26'
require_text hack/provider-probe-image.json 'docker.io/library/busybox@sha256:'
require_text docs/release-process.md 'Do not provision cloud resources merely because its advisory freshness date has passed.'
jq -e '.schemaVersion == 1 and .platform == "linux/amd64" and
  (.image | test("^docker.io/library/busybox@sha256:[a-f0-9]{64}$")) and
  .requiredCommands == ["/bin/sh","httpd","wget"]' hack/provider-probe-image.json >/dev/null ||
  { echo 'support contract check failed: provider probe image contract is invalid' >&2; exit 1; }

for profile in \
  gke-cos-containerd-amd64 gke-ubuntu-containerd-amd64 \
  eks-al2023-containerd-amd64 aks-ubuntu-containerd-amd64 \
  self-managed-containerd self-managed-crio-amd64 \
  gke-autopilot eks-fargate aks-virtual-nodes windows-deep-mode cgroup-v1; do
  test -f "hack/provider-profiles/${profile}.json" || {
    echo "support contract check failed: missing provider profile ${profile}" >&2
    exit 1
  }
done

for profile in \
  gke-cos-containerd-amd64 gke-ubuntu-containerd-amd64 \
  eks-al2023-containerd-amd64 aks-ubuntu-containerd-amd64 \
  self-managed-containerd self-managed-crio-amd64 \
  gke-autopilot eks-fargate aks-virtual-nodes windows-deep-mode cgroup-v1; do
  test -f "hack/provider-values/${profile}.yaml" || {
    echo "support contract check failed: missing provider values ${profile}" >&2
    exit 1
  }
done
test -x hack/provider-inventory/collect.py ||
  { echo 'support contract check failed: provider inventory collector is not executable' >&2; exit 1; }
test -x hack/provider-inventory/observe_unsupported.py ||
  { echo 'support contract check failed: unsupported observation collector is not executable' >&2; exit 1; }
test -f hack/provider-profiles/evaluate_matrix.py ||
  { echo 'support contract check failed: provider matrix evaluator is missing' >&2; exit 1; }
test -x hack/provider-profiles/build_unsupported_pending.py ||
  { echo 'support contract check failed: unsupported evidence builder is not executable' >&2; exit 1; }
test -x hack/verify_chart_archive.py ||
  { echo 'support contract check failed: chart archive verifier is not executable' >&2; exit 1; }

echo 'support contract check passed'
