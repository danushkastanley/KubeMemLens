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

node_fixture=$(mktemp "${TMPDIR:-/tmp}/kube-memlens-node-fixture.XXXXXX")
trap 'rm -f -- "${node_fixture}"' EXIT
printf '%s\n' '{"items":[
  {"metadata":{"name":"kind-control-plane","labels":{"node-role.kubernetes.io/control-plane":""}}},
  {"metadata":{"name":"kind-legacy-control-plane","labels":{"node-role.kubernetes.io/master":""}}},
  {"metadata":{"name":"kind-worker","labels":{"kubernetes.io/hostname":"kind-worker"}}}
]}' > "${node_fixture}"
[ "$(density_select_kind_worker "${node_fixture}")" = kind-worker ] || {
  echo "kind worker selection accepted a control-plane Node" >&2
  exit 1
}
rm -f -- "${node_fixture}"
trap - EXIT

collector_namespace=component-ns
namespace=workload-ns
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-density-library-test.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT
deployment_document=${work_dir}/density-deployment.json
image=test-image
containers_per_pod=1
pod_count=0
creation_batch_pods=1
k() {
  if [ "$*" = "apply -f -" ]; then
    cat > "${deployment_document}"
  fi
}
density_create_staged_workload
jq -e '.spec.strategy.rollingUpdate == {maxSurge:0,maxUnavailable:"10%"}' \
  "${deployment_document}" >/dev/null || {
  echo "density workload rollout strategy changed unexpectedly" >&2
  exit 1
}

fail() {
  echo "$*" >&2
  exit 1
}
containers=24
containers_per_pod=2
pod_count=12
creation_batch_pods=10
baseline_workload_document=$(jq -cn --argjson containersPerPod "${containers_per_pod}" '
  {items:[range(0;12) as $pod |
    {metadata:{name:("worker-" + (11-$pod|tostring)),uid:("uid-" + (11-$pod|tostring)),deletionTimestamp:null},
     status:{phase:"Running",containerStatuses:[range(0;$containersPerPod) |
       {ready:true,state:{running:{}},restartCount:0,lastState:{}}]}}]}')
workload_document=${baseline_workload_document}
workload_pod_uids=$(jq -c '[.items[].metadata.uid] | sort' <<<"${workload_document}")
baseline_workload_restarts=0
baseline_workload_oom_kills=0
accepted_workload_pod_uids='[]'
workload_replacement_expected_pods=0
workload_replacement_observed_pods=0
workload_replacement_resident_containers_before=0
workload_replacement_resident_containers_after=0
selection_file=${work_dir}/workload-churn-selection.json
deleted_resources=
k() {
  case "$*" in
    *"get pods"*) printf '%s' "${workload_document}" ;;
    *"delete pod"*) deleted_resources=$* ;;
  esac
}
density_prepare_workload_batch "${selection_file}"
[ "$(find "${selection_file}" -prune -perm 0600 -print)" = "${selection_file}" ] || {
  echo "density workload replacement selection is not private" >&2
  exit 1
}
[ "$(jq -c 'map(.name)' "${selection_file}")" = \
  '["worker-0","worker-1","worker-10","worker-11","worker-2","worker-3","worker-4","worker-5","worker-6","worker-7"]' ] || {
  echo "density workload replacement did not select the exact sorted batch" >&2
  exit 1
}
density_delete_workload_batch "${selection_file}"
expected_deleted='worker-0 worker-1 worker-10 worker-11 worker-2 worker-3 worker-4 worker-5 worker-6 worker-7'
actual_deleted=${deleted_resources#*--wait=false }
[ "${actual_deleted}" = "${expected_deleted}" ] || {
  echo "density workload replacement did not delete the exact batch" >&2
  exit 1
}

workload_document=$(jq -cn --argjson containersPerPod "${containers_per_pod}" '
  def pod($name;$uid):
    {metadata:{name:$name,uid:$uid,deletionTimestamp:null},status:{phase:"Running",
      containerStatuses:[range(0;$containersPerPod) |
        {ready:true,state:{running:{}},restartCount:0,lastState:{}}]}};
  ([8,9] | map(. as $pod | pod("worker-"+($pod|tostring);"uid-"+($pod|tostring)))) as $retained |
  [range(0;10) | pod("replacement-"+tostring;"replacement-uid-"+tostring)] as $replacements |
  {items:($retained + $replacements)}')
density_wait_for_workload_batch_recovery "${selection_file}" "$((SECONDS + 2))"
if [ "${workload_replacement_expected_pods}" -ne 10 ] ||
  [ "${workload_replacement_observed_pods}" -ne 10 ] ||
  [ "${workload_replacement_resident_containers_before}" -ne 24 ] ||
  [ "${workload_replacement_resident_containers_after}" -ne 24 ]; then
  echo "density workload replacement did not retain exact sanitised evidence" >&2
  exit 1
fi
disruption_unexplained_restarts=0
disruption_oom_kills=0
density_accept_workload_batch

extra_replacement_document=$(jq -cn --argjson containersPerPod "${containers_per_pod}" '
  def pod($name;$uid):
    {metadata:{name:$name,uid:$uid,deletionTimestamp:null},status:{phase:"Running",
      containerStatuses:[range(0;$containersPerPod) |
        {ready:true,state:{running:{}},restartCount:0,lastState:{}}]}};
  ([9] | map(. as $pod | pod("worker-"+($pod|tostring);"uid-"+($pod|tostring)))) as $retained |
  [range(0;11) | pod("replacement-"+tostring;"replacement-uid-"+tostring)] as $replacements |
  {items:($retained + $replacements)}')
if (workload_document=${extra_replacement_document};
  density_wait_for_workload_batch_recovery "${selection_file}" "$((SECONDS + 2))" >/dev/null 2>&1); then
  echo "density workload replacement accepted extra Pod churn" >&2
  exit 1
fi

not_ready_document=$(jq '.items[0].status.containerStatuses[0].ready = false' <<<"${baseline_workload_document}")
if failure_output=$(workload_document=${not_ready_document};
  density_prepare_workload_batch "${work_dir}/rejected-selection.json" 2>&1); then
  echo "density workload replacement accepted a not-ready Pod" >&2
  exit 1
fi
case "${failure_output}" in
  *worker-*|*uid-*)
    echo "density workload replacement failure exposed a workload identifier" >&2
    exit 1
    ;;
esac

restarted_document=$(jq '.items[0].status.containerStatuses[0].restartCount = 1' \
  <<<"${baseline_workload_document}")
if (workload_document=${restarted_document};
  density_prepare_workload_batch "${work_dir}/restarted-selection.json" >/dev/null 2>&1); then
  echo "density workload replacement accepted a pre-delete restart" >&2
  exit 1
fi

mapped_page='{"metadata":{"continue":""},"items":[
  {"snapshot":{"podUID":"mapped-2","freshness":"fresh","completeness":"partial","context":{"labels":{"app.kubernetes.io/name":"density-workers"}}}},
  {"snapshot":{"podUID":"mapped-1","freshness":"fresh","completeness":"complete","context":{"labels":{"app.kubernetes.io/name":"density-workers"}}}},
  {"snapshot":{"podUID":"canary","freshness":"fresh","completeness":"complete","context":{"labels":{"app.kubernetes.io/name":"density-canary"}}}}
]}'
k() {
  case "$*" in
    *"get --raw"*) printf '%s' "${mapped_page}" ;;
  esac
}
[ "$(mapped_workload_pod_uids)" = '["mapped-1","mapped-2"]' ] || {
  echo "mapped workload UID check did not accept exact fresh partial evidence" >&2
  exit 1
}
mapped_page=$(jq '.items[0].snapshot.freshness = "stale"' <<<"${mapped_page}")
if mapped_workload_pod_uids >/dev/null 2>&1; then
  echo "mapped workload UID check accepted stale evidence" >&2
  exit 1
fi
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

startup_workload_pod_uids='[]'
workload_document=$(jq -cn '
  {items:[range(0;100) as $pod | {metadata:{uid:("workload-"+($pod|tostring))},
    status:{containerStatuses:[range(0;50) |
      {restartCount:0,lastState:{},fixturePadding:("x" * 512)}]}}]}')
[ "$(LC_ALL=C printf '%s' "${workload_document}" | wc -c | tr -d ' ')" -gt 2500000 ] || {
  echo "large workload fixture does not exceed common command argument limits" >&2
  exit 1
}
density_assert_startup_stable
[ "$(jq 'length' <<<"${startup_workload_pod_uids}")" -eq 100 ] || {
  echo "startup stability check did not handle the full RC Pod document" >&2
  exit 1
}
rm -rf -- "${work_dir}"
trap - EXIT

echo "density library failure propagation passed"
