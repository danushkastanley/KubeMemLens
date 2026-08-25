#!/usr/bin/env bash

set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "${root}"

[ "${ISOLATION_ACKNOWLEDGE:-}" = remove-and-restore-kube-memlens-security-controls ] || {
  echo "set ISOLATION_ACKNOWLEDGE=remove-and-restore-kube-memlens-security-controls" >&2
  exit 1
}
[ "${TENANT_READ_PHASE:-install}" = install ] || {
  echo "tenant isolation verification runs only during the install phase" >&2
  exit 1
}

export TENANT_READ_PHASE=install
export TENANT_READ_RUN_ISOLATION=true
export TENANT_READ_ACKNOWLEDGE=run-and-clean-tenant-read-verification
exec hack/verify-tenant-scoped-reads-kind.sh
