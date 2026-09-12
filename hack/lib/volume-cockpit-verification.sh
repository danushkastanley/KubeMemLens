#!/usr/bin/env bash
# The parent owns the disposable cluster and supplies all paths and Helm options.
# shellcheck disable=SC2154
volume_cockpit_verification() {
  local cli="${work_dir}/kubectl-memlens" config="${work_dir}/volume-reader-kubeconfig"
  kctl create rolebinding volume-memory-reader -n kube-memlens-csi-e2e --clusterrole=kube-memlens-namespace-viewer --serviceaccount=kube-memlens-csi-e2e:volume-viewer >/dev/null
  python3 - "${work_dir}" <<'PY'
import json,pathlib,sys
root=pathlib.Path(sys.argv[1]); config=json.loads((root/'api-config.json').read_text())
cluster=config['clusters'][0]['name']
config['users']=[{'name':'volume-viewer','user':{'tokenFile':str(root/'volume-viewer.token')}}]
config['contexts']=[{'name':'volume-cockpit','context':{'cluster':cluster,'user':'volume-viewer','namespace':'kube-memlens-csi-e2e'}}]
config['current-context']='volume-cockpit'
(root/'volume-reader-kubeconfig').write_text(json.dumps(config))
PY
  local reader=("${cli}" --kubeconfig "${config}")
  volume_wait 'd["context"]["volumes"][0]["usage"]["freshness"]=="fresh"' cockpit-fresh.json
  "${reader[@]}" volumes pod persistent -n kube-memlens-csi-e2e -o json > "${work_dir}/volume-cli.json"
  "${reader[@]}" capture --volumes --pod persistent -n kube-memlens-csi-e2e --include-sensitive -o "${work_dir}/volume-before-capture.json" >/dev/null
  kctl exec persistent -n kube-memlens-csi-e2e -- sh -c 'dd if=/dev/zero of=/data/cockpit-bytes bs=1048576 count=1 2>/dev/null; sync' >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"].get("filesystem",{}).get("usedBytes",0)>=previous["context"]["volumes"][0]["usage"]["filesystem"]["usedBytes"]+1048576' cockpit-written.json cockpit-fresh.json
  "${reader[@]}" capture --volumes --pod persistent -n kube-memlens-csi-e2e --include-sensitive -o "${work_dir}/volume-after-capture.json" >/dev/null
  "${reader[@]}" capture --volumes --pod persistent -n kube-memlens-csi-e2e --include-history -o "${work_dir}/volume-redacted.json" >/dev/null
  "${reader[@]}" explain pod persistent -n kube-memlens-csi-e2e --volumes > "${work_dir}/volume-explain.txt"
  "${reader[@]}" recommend pod persistent -n kube-memlens-csi-e2e --volumes -o json > "${work_dir}/volume-recommend.json"
  "${cli}" --kubeconfig "${work_dir}/missing-config" replay "${work_dir}/volume-redacted.json" > "${work_dir}/volume-replay.txt"
  "${cli}" --kubeconfig "${work_dir}/missing-config" compare --volumes --before "${work_dir}/volume-before-capture.json" --after "${work_dir}/volume-after-capture.json" > "${work_dir}/volume-compare.txt"
  "${cli}" replay "${work_dir}/volume-redacted.json" --export-schema 1 -o "${work_dir}/volume-legacy.json" >/dev/null
  for columns in 80 160; do
    local rows=24
    if [ "${columns}" = 160 ]; then rows=35; fi
    expect hack/volume-cockpit-smoke.exp "${cli}" "${config}" "${work_dir}/volume-pty-${columns}.json" "${columns}" "${rows}" > "${work_dir}/volume-pty-${columns}.log" 2>&1 || {
      cat "${work_dir}/volume-pty-${columns}.log" >&2; return 1;
    }
  done
  kctl delete rolebinding volume-reader -n kube-memlens-csi-e2e >/dev/null
  node_context_expect_status "${work_dir}" volume-viewer "${volume_path}" 403 6
  if "${reader[@]}" capture --volumes --pod persistent -n kube-memlens-csi-e2e -o "${work_dir}/volume-denied.json" > "${work_dir}/volume-denied.log" 2>&1; then
    echo 'revoked volume capture succeeded' >&2; return 1
  fi
  [ ! -e "${work_dir}/volume-denied.json" ] || { echo 'revoked capture left a file' >&2; return 1; }
  kctl create rolebinding volume-reader -n kube-memlens-csi-e2e --clusterrole=kube-memlens-volume-viewer --serviceaccount=kube-memlens-csi-e2e:volume-viewer >/dev/null
  source hack/lib/volume-workload-verification.sh
  volume_workload_verification
  python3 hack/fixtures/volume-stats/check_cockpit.py "${work_dir}"
  echo 'PASS volume CLI, private capture, offline replay/compare, compact/wide PTY and workload composition'
}
