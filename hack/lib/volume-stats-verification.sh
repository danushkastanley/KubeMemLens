#!/usr/bin/env bash
# Called inside the owned Node-context kind run; never uses the current context.
volume_stats_verification() {
  local upstream=eccd681b18a2c96332f33c2cac5db38656edd500
  local volume_path=/namespaces/kube-memlens-csi-e2e/pods/persistent/volumes
  volume_image="kube-memlens-csi-hostpath:$(basename "${work_dir}")"
  if docker image inspect "${volume_image}" >/dev/null 2>&1; then echo 'fixture image already exists' >&2; return 1; fi
  curl --fail --silent --show-error --location --max-time 60 --max-filesize 10485760 \
    "https://codeload.github.com/kubernetes-csi/csi-driver-host-path/tar.gz/${upstream}" -o "${work_dir}/hostpath.tar.gz"
  [ "$(shasum -a 256 "${work_dir}/hostpath.tar.gz" | awk '{print $1}')" = 3b9e40a1f49435d1b264a28bc821580647d51fdec69d514e61bb70ec83f72bd8 ] || { echo 'CSI source digest mismatch' >&2; return 1; }
  tar -xzf "${work_dir}/hostpath.tar.gz" -C "${work_dir}"
  mkdir -p "${work_dir}/csi-driver-host-path-${upstream}/cmd/volume-seed"
  cp hack/fixtures/volume-stats/seed.go.txt "${work_dir}/csi-driver-host-path-${upstream}/cmd/volume-seed/main.go"
  volume_image_created=true
  docker build -f hack/fixtures/volume-stats/Dockerfile -t "${volume_image}" "${work_dir}/csi-driver-host-path-${upstream}" > "${work_dir}/csi-build.log" 2>&1
  docker image inspect "${volume_image}" --format '{{.Id}}' > "${work_dir}/csi-image-id"
  kind load docker-image "${volume_image}" --name "${cluster}" > "${work_dir}/csi-load.log" 2>&1
  # Isolate the filesystem measured by fs.Info; this is a disposable test backend.
  # Production collection has no host mounts, CSI sockets or privileged access.
  sed -e "s|kube-memlens-csi-hostpath:eccd681-versioned|${volume_image}|" \
    -e 's/emptyDir: {}/emptyDir: {medium: Memory, sizeLimit: 128Mi}/' \
    hack/fixtures/csi-health/driver.yaml > "${work_dir}/csi-driver.yaml"
  kctl apply -f "${work_dir}/csi-driver.yaml" >/dev/null
  kctl wait pod/driver -n kube-memlens-csi-e2e --for=condition=Ready --timeout=120s >/dev/null
  kctl exec driver -n kube-memlens-csi-e2e -c driver -- /volume-seed > "${work_dir}/csi-volume.json"
  python3 - "${work_dir}" <<'PY'
import json,pathlib,re,sys
root=pathlib.Path(sys.argv[1]); handle=json.loads((root/'csi-volume.json').read_text())['volumeID']
assert re.fullmatch('[a-f0-9-]{36}',handle)
(root/'persistent.yaml').write_text(pathlib.Path('hack/fixtures/csi-health/persistent.yaml').read_text().replace('VOLUME_HANDLE',handle).replace('runAsUser: 1000','runAsUser: 1000\n    fsGroup: 1000'))
PY
  kctl apply -f "${work_dir}/persistent.yaml" >/dev/null
  kctl wait pod/persistent -n kube-memlens-csi-e2e --for=condition=Ready --timeout=120s >/dev/null
  kctl create namespace kube-memlens-csi-other >/dev/null
  # An identical claim/Pod name in another namespace is an authorisation case.
  sed 's/kube-memlens-csi-e2e/kube-memlens-csi-other/g' "${work_dir}/persistent.yaml" | kctl apply -f - >/dev/null
  local volume_args=(--set nodeContext.enabled=true --set volumeContext.enabled=true
    --set 'volumeContext.namespaces[0]=kube-memlens-csi-e2e' --set 'volumeContext.namespaces[1]=kube-memlens-csi-other')
  helm "${helm_args[@]}" "${volume_args[@]}" --set nodeContext.volumeStats=true > "${work_dir}/helm-volumes.log" 2>&1
  kctl rollout status daemonset/kube-memlens-node-context -n "${namespace}" --timeout=120s >/dev/null
  kctl create serviceaccount volume-viewer -n kube-memlens-csi-e2e >/dev/null
  kctl create rolebinding volume-reader -n kube-memlens-csi-e2e --clusterrole=kube-memlens-volume-viewer --serviceaccount=kube-memlens-csi-e2e:volume-viewer >/dev/null
  kctl create token volume-viewer -n kube-memlens-csi-e2e --duration=30m > "${work_dir}/volume-viewer.token"
  python3 - "${work_dir}" <<'PY'
import pathlib,sys
p=pathlib.Path(sys.argv[1]); (p/'volume-viewer.header').write_text('Authorization: Bearer '+(p/'volume-viewer.token').read_text().strip()+'\n')
PY
  node_context_expect_status "${work_dir}" node-viewer "${volume_path}" 403 4
  node_context_expect_status "${work_dir}" tenant-viewer "${volume_path}" 403 4
  node_context_expect_status "${work_dir}" volume-viewer /namespaces/kube-memlens-csi-other/pods/persistent/volumes 403 4
  node_context_expect_status "${work_dir}" volume-viewer /namespaces/kube-memlens-csi-other/pods/absent/volumes 403 4
  node_context_expect_status "${work_dir}" volume-viewer "${volume_path}" 404 3
  volume_wait 'd["context"]["volumes"][0]["usage"].get("filesystem",{}).get("capacityBytes")==134217728' volume-before.json
  go build -o "${work_dir}/volume-probe" ./hack/fixtures/volume-stats-probe
  "${work_dir}/volume-probe" --kubeconfig "${kubeconfig}" --token-file "${work_dir}/volume-viewer.token" > "${work_dir}/volume-client.json"
  kctl exec persistent -n kube-memlens-csi-e2e -- sh -c 'dd if=/dev/zero of=/data/known-bytes bs=1048576 count=8 2>/dev/null; i=0; while [ "$i" -lt 64 ]; do : > "/data/inode-$i"; i=$((i+1)); done; sync' >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"].get("filesystem",{}).get("usedBytes",0)>=previous["context"]["volumes"][0]["usage"]["filesystem"]["usedBytes"]+8388608' volume-written.json volume-before.json
  python3 - "${work_dir}" <<'PY'
import json,pathlib,sys
root=pathlib.Path(sys.argv[1]); before=json.loads((root/'volume-before.json').read_text()); after=json.loads((root/'volume-written.json').read_text())
b=before['context']['volumes'][0]['usage']['filesystem']; row=after['context']['volumes'][0]; a=row['usage']['filesystem']
assert row['pvcName']=='data' and 'driver' not in row, 'PV driver disclosed without PV permission'
assert a['capacityBytes']==b['capacityBytes']==128*1024*1024
assert a['usedBytes']-b['usedBytes']==8*1024*1024 and b['availableBytes']-a['availableBytes']==8*1024*1024
assert a['inodesUsed']-b['inodesUsed']==65 and b['inodesFree']-a['inodesFree']==65
assert a['capturedAt']>b['capturedAt'] and row['usage']['freshness']=='fresh'
(root/'volume-deltas.json').write_text(json.dumps({'capacityBytes':a['capacityBytes'],'usedDeltaBytes':a['usedBytes']-b['usedBytes'],'inodeDelta':a['inodesUsed']-b['inodesUsed'],'sourceTimestampAdvanced':True}))
PY
  source hack/lib/volume-stats-lifecycle.sh
  volume_stats_lifecycle
  python3 - "${work_dir}" "${upstream}" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1])
d={'outcome':'passed','upstreamDriverCommit':sys.argv[2],'driverImageDigest':(p/'csi-image-id').read_text().strip(),
 'backend':'isolated 128 MiB tmpfs in disposable CSI fixture','kubeletStatsCadenceSeconds':5,'producerCadenceSeconds':15,
 'measurement':json.loads((p/'volume-deltas.json').read_text()),'productionClient':json.loads((p/'volume-client.json').read_text()),
 'callerPVCWithoutPV':True,'nodeViewerDenied':True,'crossNamespaceDenied':True,'oldSchemaHidden':True,
 **json.loads((p/'volume-lifecycle.json').read_text()),'credentialsRetained':False,'runtimeIdentifiersIncluded':False}
(p/'volume-result.json').write_text(json.dumps(d))
PY
  echo 'PASS isolated CSI bytes/inodes, authenticated volume publication, bounded client, scoped reads and lifecycle'
}

volume_wait() {
  node_context_wait_read "${work_dir}" volume-viewer "${volume_path}" "$1" "$2" "${3:-}" 4
}
