#!/usr/bin/env bash
# Only the parent-owned, isolated 128 MiB tmpfs CSI fixture is modified.
# shellcheck disable=SC2154
volume_pressure_verification() {
  local kind bytes inode_limit free_inodes count
  kind=$(kctl exec driver -n kube-memlens-csi-e2e -c driver -- stat -f -c %T /csi-data-dir)
  # The arithmetic runs inside the isolated driver container.
  # shellcheck disable=SC2016
  bytes=$(kctl exec driver -n kube-memlens-csi-e2e -c driver -- sh -c 'echo $(($(stat -f -c %b /csi-data-dir)*$(stat -f -c %S /csi-data-dir)))')
  inode_limit=$(kctl exec driver -n kube-memlens-csi-e2e -c driver -- stat -f -c %c /csi-data-dir)
  if [ "${kind}" != tmpfs ] || [ "${bytes}" != 134217728 ]; then
    echo 'pressure fixture is not the isolated backend' >&2; return 1
  fi
  [[ "${inode_limit}" =~ ^[0-9]+$ ]] || { echo 'invalid original inode ceiling' >&2; return 1; }
  kctl apply -f hack/fixtures/volume-stats/pressure-writer.yaml >/dev/null
  kctl wait pod/pressure-writer -n kube-memlens-csi-e2e --for=condition=Ready --timeout=120s >/dev/null
  # 114 MiB plus the preceding controlled 9 MiB stays below the 128 MiB bound.
  kctl exec pressure-writer -n kube-memlens-csi-e2e -- sh -c 'dd if=/dev/zero of=/data/r5-pressure-bytes bs=1048576 count=114 2>/dev/null; sync' >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"].get("filesystem",{}).get("usedBytes",0)>=120*1048576' pressure-bytes.json
  "${work_dir}/kubectl-memlens" --kubeconfig "${work_dir}/volume-reader-kubeconfig" volumes pod persistent -n kube-memlens-csi-e2e -o json > "${work_dir}/pressure-explain-bytes.json"
  kctl exec driver -n kube-memlens-csi-e2e -c driver -- mount -o remount,rw,nosuid,nodev,noexec,size=128m,nr_inodes=128 /csi-data-dir
  free_inodes=$(kctl exec driver -n kube-memlens-csi-e2e -c driver -- stat -f -c %d /csi-data-dir)
  if ! [[ "${free_inodes}" =~ ^[0-9]+$ ]] || [ "${free_inodes}" -le 12 ] || [ "${free_inodes}" -gt 128 ]; then
    echo 'invalid bounded inode baseline' >&2; return 1
  fi
  count=$((free_inodes-8))
  # Expand only fixture-local shell counters, never runner commands.
  # shellcheck disable=SC2016
  kctl exec pressure-writer -n kube-memlens-csi-e2e -- sh -c 'set -e; i=0; while [ "$i" -lt "$1" ]; do : > "/data/r5-pressure-inode-$i"; i=$((i+1)); done; sync' sh "${count}" >/dev/null
  volume_wait 'd["context"]["volumes"][0]["usage"].get("filesystem",{}).get("inodesFree",999999)<=12' pressure-inodes.json
  "${work_dir}/kubectl-memlens" --kubeconfig "${work_dir}/volume-reader-kubeconfig" volumes pod persistent -n kube-memlens-csi-e2e -o json > "${work_dir}/pressure-explain-inodes.json"
  kctl exec pressure-writer -n kube-memlens-csi-e2e -- sh -c 'rm /data/r5-pressure-bytes /data/r5-pressure-inode-*; sync' >/dev/null
  kctl exec driver -n kube-memlens-csi-e2e -c driver -- mount -o "remount,rw,nosuid,nodev,noexec,size=128m,nr_inodes=${inode_limit}" /csi-data-dir
  volume_wait 'd["context"]["volumes"][0]["usage"].get("filesystem",{}).get("usedBytes",999999999)<16*1048576' pressure-recovered.json
  kctl delete pod pressure-writer -n kube-memlens-csi-e2e --wait=true >/dev/null
  python3 - "${work_dir}" <<'PY'
import json,pathlib,sys
p=pathlib.Path(sys.argv[1])
b=json.loads((p/'pressure-explain-bytes.json').read_text());i=json.loads((p/'pressure-explain-inodes.json').read_text())
assert any(s['kind']=='filesystem-pressure' for s in b['analysis']['signals'])
assert any(s['kind']=='inode-pressure' for s in i['analysis']['signals'])
assert b['analysis']['storageSeverity']=='high' and i['analysis']['storageSeverity']=='high'
(p/'volume-pressure-result.json').write_text(json.dumps({'filesystemPressure':True,'inodePressure':True,'recovered':True,'capacityBytes':134217728,'inodeCeiling':128}))
PY
}
