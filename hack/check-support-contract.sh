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

contract=docs/compatibility.md

require_text "$contract" '# Support and compatibility contract'
require_text "$contract" '| GKE Autopilot | Unsupported for deep mode |'
require_text "$contract" '| EKS Fargate | Unsupported for deep mode |'
require_text "$contract" '| AKS virtual nodes | Unsupported for deep mode |'
require_text "$contract" '| Windows worker nodes | Unsupported for deep mode |'
require_text "$contract" '| cgroup v1 | Unsupported |'
require_text "$contract" 'Shared multi-tenant clusters are a mandatory v1 threat environment.'
require_text "$contract" 'exactly one collector replica holds independent in-memory state'
require_text "$contract" 'Default Pod history is retained in collector memory for at most 15 minutes'
require_text "$contract" 'Configured store ceilings are rejection bounds, not scale claims.'
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

echo 'support contract check passed'
