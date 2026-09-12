#!/usr/bin/env bash
# Uses only parent-owned fixture state; no current-context fallback.
# shellcheck disable=SC2154
volume_workload_verification() {
  helm "${helm_args[@]}" "${volume_args[@]}" --set nodeContext.volumeStats=true --set volumeContext.workloads=true > "${work_dir}/helm-volume-workloads.log" 2>&1
  kctl apply -f hack/fixtures/volume-stats/workload.yaml >/dev/null
  kctl rollout status deployment/volume-workload -n kube-memlens-csi-e2e --timeout=120s >/dev/null
  local workload_path='/namespaces/kube-memlens-csi-e2e/workloads/volume-workload/volumes?kind=Deployment'
  node_context_expect_status "${work_dir}" volume-viewer "${workload_path}" 403 6
  kctl create rolebinding workload-volume-reader -n kube-memlens-csi-e2e --clusterrole=kube-memlens-workload-volume-viewer --serviceaccount=kube-memlens-csi-e2e:volume-viewer >/dev/null
  node_context_wait_read "${work_dir}" volume-viewer "${workload_path}" 'len(d["workload"]["pods"])==2 and len(d["filesystems"])==3 and len([g for g in d["filesystems"] if len(g["members"])==2])==1 and d["workload"]["memory"]["ShmemBytes"]>=4194304' workload-volumes.json '' 6
  "${reader[@]}" volumes workload Deployment/volume-workload -n kube-memlens-csi-e2e -o json > "${work_dir}/volume-workload-cli.json"
  "${reader[@]}" explain workload Deployment/volume-workload -n kube-memlens-csi-e2e --volumes > "${work_dir}/volume-workload-explain.txt"
  "${reader[@]}" recommend workload Deployment/volume-workload -n kube-memlens-csi-e2e --volumes -o json > "${work_dir}/volume-workload-recommend.json"
  kctl delete rolebinding workload-volume-reader -n kube-memlens-csi-e2e >/dev/null
  node_context_expect_status "${work_dir}" volume-viewer "${workload_path}" 403 6
  kctl delete deployment volume-workload -n kube-memlens-csi-e2e --wait=true >/dev/null
}
