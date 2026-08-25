#!/usr/bin/env bash

set -Eeuo pipefail

# shellcheck source=hack/lib/density-telemetry.sh
source hack/lib/density-telemetry.sh
# shellcheck source=hack/lib/density-runtime.sh
source hack/lib/density-runtime.sh

cli=test-cli
kubeconfig=test-kubeconfig
context=test-context
collector_namespace=test-namespace
namespace=test-soak-namespace

expect() { return 1; }
if density_measure_tui_ms >/dev/null; then
  echo "TUI measurement hid an Expect failure" >&2
  exit 1
fi

k() { return 1; }
if density_measure_canary_ms 1 >/dev/null; then
  echo "canary measurement hid a kubectl failure" >&2
  exit 1
fi

docker() { return 1; }
paused_kind_node=test-worker
if density_restore_paused_node; then
  echo "paused worker restoration hid a Docker failure" >&2
  exit 1
fi
[ "${paused_kind_node}" = test-worker ] || {
  echo "failed worker restoration discarded cleanup state" >&2
  exit 1
}

docker() { return 0; }
density_restore_paused_node
[ -z "${paused_kind_node}" ] || {
  echo "successful worker restoration retained cleanup state" >&2
  exit 1
}

echo "density library failure propagation passed"
