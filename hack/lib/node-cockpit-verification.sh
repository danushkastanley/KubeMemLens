#!/usr/bin/env bash

# Only called inside the owned Node-context kind fixture. Never retain captures
# or reader credentials outside its private temporary directory.
node_cockpit_verification() {
  local work_dir=$1 namespace=$2 node=$3 admin_config=$4 admin_context=$5
  command -v expect >/dev/null
  CGO_ENABLED=0 go build -trimpath -o "${work_dir}/kubectl-memlens" ./cmd/kubectl-memlens
  python3 - "${work_dir}" <<'PY'
import json,pathlib,sys
root=pathlib.Path(sys.argv[1]); config=json.loads((root/'api-config.json').read_text())
cluster=config['clusters'][0]['name']
config['users']=[{'name':'node-viewer','user':{'tokenFile':str(root/'node-viewer.token')}}]
config['contexts']=[{'name':'node-cockpit','context':{'cluster':cluster,'user':'node-viewer'}}]
config['current-context']='node-cockpit'
(root/'node-reader-kubeconfig').write_text(json.dumps(config))
PY
  local config="${work_dir}/node-reader-kubeconfig" cli="${work_dir}/kubectl-memlens"
  node_cockpit_read "${work_dir}" "${cli}" --kubeconfig "${config}" explain node "${node}" -o json > "${work_dir}/cli-node-only.json"
  "${cli}" --kubeconfig "${config}" history node "${node}" -o json > "${work_dir}/cli-node-history.json"
  node_cockpit_read "${work_dir}" "${cli}" --kubeconfig "${config}" capture --node "${node}" --include-history -o "${work_dir}/cli-node-only-capture.json" >/dev/null
  kctl create clusterrolebinding node-cockpit-contributors --clusterrole=node-analysis-pod-list --serviceaccount="${namespace}:node-viewer" >/dev/null
  node_cockpit_read "${work_dir}" "${cli}" --kubeconfig "${config}" capture --node "${node}" --include-history -o "${work_dir}/cli-node-full-capture.json" >/dev/null
  "${cli}" --kubeconfig "${work_dir}/missing-offline-config" replay "${work_dir}/cli-node-full-capture.json" > "${work_dir}/cli-replay.txt"
  "${cli}" --kubeconfig "${work_dir}/missing-offline-config" compare --before "${work_dir}/cli-node-full-capture.json" --after "${work_dir}/cli-node-full-capture.json" --node "${node}" > "${work_dir}/cli-compare.txt"
  kctl delete clusterrolebinding node-cockpit-contributors >/dev/null
  node_cockpit_read "${work_dir}" "${cli}" --kubeconfig "${config}" capture --node "${node}" --include-sensitive -o "${work_dir}/cli-node-revoked.json" >/dev/null
  for columns in 80 160; do
    local rows=24
    if [ "${columns}" = 160 ]; then rows=35; fi
    if ! expect hack/node-cockpit-smoke.exp "${cli}" "${admin_config}" "${admin_context}" "${node}" "${work_dir}/pty-${columns}.json" "${columns}" "${rows}" > "${work_dir}/pty-${columns}.log" 2>&1; then
      cat "${work_dir}/pty-${columns}.log" >&2
      return 1
    fi
    "${cli}" --kubeconfig "${work_dir}/missing-offline-config" replay "${work_dir}/pty-${columns}.json" > "${work_dir}/pty-${columns}-replay.txt"
  done
  python3 - "${work_dir}" <<'PY'
import hashlib,json,pathlib,stat,sys
root=pathlib.Path(sys.argv[1])
for name in ('cli-node-only-capture.json','cli-node-revoked.json'):
    b=json.loads((root/name).read_text());a=b['evidence']['analysis']
    assert b['schemaVersion']==4 and a['contributorAccess']=='node-only'
    assert all(k not in a for k in ('rankings','coverage','observedPodChargeBytes'))
    assert 'node-analysis-a' not in json.dumps(b) and 'node-analysis-b' not in json.dumps(b)
for name in ('cli-node-full-capture.json','pty-80.json','pty-160.json'):
    p=root/name;b=json.loads(p.read_text());a=b['evidence']['analysis']
    assert b['schemaVersion']==4 and b['redacted'] and stat.S_IMODE(p.stat().st_mode)==0o600
    assert b['evidence']['record']['nodeUID'].startswith('sha256-')
    assert a['contributorAccess']=='cluster-pods' and a['rankings']['pods']
    if name.startswith('pty-'): assert a['rankings']['metric']=='anon'
    assert b['history']['series']
    assert 'node-analysis-a' not in json.dumps(b) and 'node-analysis-b' not in json.dumps(b)
    assert all(row['namespace'].startswith('namespace-') and row['name'].startswith('pod-') for row in a['rankings']['pods'])
result={'cliExplain':True,'cliHistory':True,'nodeOnlyCapture':True,'freshExportAuthorisation':True,
 'redactedCapture':True,'privateFileMode':True,'offlineReplayCompare':True,'pty80x24':True,'pty160x35':True,
 'ptyScrollPauseRankCaptureDrillDown':True,'cliBinarySHA256':hashlib.sha256((root/'kubectl-memlens').read_bytes()).hexdigest()}
(root/'cockpit-result.json').write_text(json.dumps(result))
PY
  echo 'PASS Node CLI capture/replay/compare, redaction, revocation and compact/wide PTY workflow'
}

node_cockpit_read() {
  local work_dir=$1
  shift
  for _ in $(seq 1 5); do
    if "$@" 2> "${work_dir}/cockpit-read-error.log"; then return 0; fi
    if ! grep -Fq 'Node evidence changed during the read' "${work_dir}/cockpit-read-error.log"; then
      cat "${work_dir}/cockpit-read-error.log" >&2
      return 1
    fi
  done
  echo 'Node source did not stabilise across bounded cockpit reads' >&2
  return 1
}
