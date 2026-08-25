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

collector_namespace=component-ns
namespace=workload-ns
startup_component_pod_uids='[]'
startup_workload_pod_uids='[]'
startup_component_restarts=0
startup_component_oom_kills=0
component_document='{"items":[{"metadata":{"uid":"component-1"},"status":{"containerStatuses":[{"restartCount":0,"lastState":{}}]}}]}'
workload_document='{"items":[{"metadata":{"uid":"workload-1"},"status":{"containerStatuses":[{"restartCount":0,"lastState":{}}]}}]}'
node_document='{"items":[{"status":{"conditions":[{"type":"MemoryPressure","status":"False"}]}}]}'
k() {
  case "$*" in
    *component-ns*) printf '%s' "${component_document}" ;;
    *workload-ns*) printf '%s' "${workload_document}" ;;
    *"get nodes"*) printf '%s' "${node_document}" ;;
  esac
}
fail() {
  echo "$*" >&2
  exit 1
}
density_capture_startup_baseline
density_assert_startup_stable
[ "${startup_workload_pod_uids}" = '["workload-1"]' ] || {
  echo "startup stability check did not retain the accepted workload set" >&2
  exit 1
}

workload_document='{"items":[{"metadata":{"uid":"workload-1"},"status":{"containerStatuses":[{"restartCount":1,"lastState":{}}]}}]}'
if (density_assert_startup_stable >/dev/null 2>&1); then
  echo "startup stability check accepted a workload restart" >&2
  exit 1
fi

echo "density library failure propagation passed"
