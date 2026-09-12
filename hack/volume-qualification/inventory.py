#!/usr/bin/env python3
"""Bind local source and built artefacts before measurement starts."""
import argparse
import hashlib
import json
import re
from pathlib import Path
from common import require, load, write_new, sha
from profile import validate
from runtime import VolumeRuntime


def file_digest(path):
    h=hashlib.sha256()
    with Path(path).open('rb') as source:
        while chunk:=source.read(1<<20):h.update(chunk)
    return 'sha256:'+h.hexdigest()

def chart_digest(root):
    h=hashlib.sha256()
    for p in sorted(Path(root).rglob('*')):
        if p.is_file():h.update(p.relative_to(root).as_posix().encode()+b'\0'+p.read_bytes()+b'\0')
    return 'sha256:'+h.hexdigest()

def inventory(r,p,out):
    private=r.private;raw=json.loads((private/'source.json').read_text())
    source={'commit':raw['sourceCommit'],'tree':'sha256:'+raw['sourceTreeSHA256'],'dirty':raw['sourceDirty'],
            'producer':file_digest(private/'image/producer'),'collector':file_digest(private/'image/memlens-collector'),
            'cli':file_digest(private/'kubectl-memlens'),'chart':chart_digest('charts/kube-memlens'),
            'image':(private/'image-id').read_text().strip(),'driverImage':(private/'csi-image-id').read_text().strip()}
    # Compare the binaries in the running processes, not only copied build files.
    components=r.containers()
    for name,key in (('collector','collector'),('node-context','producer')):
        actual='sha256:'+r.host('sha256sum',f"/proc/{components[name]['pid']}/exe").split()[0]
        require(actual==source[key],'running component differs from built binary')
    pod=json.loads(r.k('get','pod','driver','-n','kube-memlens-csi-e2e','-o','json'))
    registrar=next(c for c in pod['spec']['containers'] if c['name']=='registrar')
    require(registrar['image']==p['registrarImage'],'registrar specification differs from frozen profile')
    statuses={c['name']:c for c in pod['status']['containerStatuses']}
    require(all(c['ready'] and c['restartCount']==0 for c in statuses.values()),'driver or registrar unstable')
    reference=statuses['registrar']['imageID']
    match=re.search(r'sha256:[a-f0-9]{64}$',reference)
    require(match is not None,'running registrar digest unreported')
    # The driver executable and CRI image are independently bound to the local build.
    cri=json.loads(r.host('crictl','ps','-o','json'))['containers']
    drivers=[c for c in cri if c.get('labels',{}).get('io.kubernetes.pod.namespace')=='kube-memlens-csi-e2e' and c.get('labels',{}).get('io.kubernetes.container.name')=='driver']
    require(len(drivers)==1,'ambiguous fixture driver')
    item=json.loads(r.host('crictl','inspect','-o','json',drivers[0]['id']))
    source['driverBinary']='sha256:'+r.host('sha256sum',f"/proc/{item['info']['pid']}/exe").split()[0]
    expected='sha256:'+r.docker('run','--rm','--network=none','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--entrypoint','sha256sum',source['driverImage'],'/hostpathplugin').split()[0]
    require(source['driverBinary']==expected,'running driver differs from built image')
    require(all(sha(source[k]) for k in ('producer','collector','cli','chart','image','driverImage','driverBinary')),'invalid actual artefact identity')
    node=json.loads(r.k('get','node',r.node,'-o','json'))['status']['nodeInfo']
    require(node['kubeletVersion']==p['kubernetes'],'running Kubernetes differs from profile')
    caps=json.loads((private/'csi-volume.json').read_text())['nodeCapabilities']
    environment={k:p[k] for k in ('kubernetes','nodeImage','driverSource','registrarImage')}
    environment.update(kernel=node['kernelVersion'],runtime=node['containerRuntimeVersion'],architecture=node['architecture'],registrarRuntimeImage=match[0],externalHealthMonitor='not-installed',controllerEvidence='api-fixture',driverCapabilities=sorted(set(caps)))
    write_new(Path(out)/'volume-source.json',source);write_new(private/'volume-environment.json',environment)

if __name__=='__main__':
    a=argparse.ArgumentParser(description=__doc__)
    for name in ('profile','cluster','node','kubeconfig','output-dir'):a.add_argument('--'+name,required=True)
    args=a.parse_args();p=validate(load(args.profile))
    inventory(VolumeRuntime(args.cluster,args.node,args.kubeconfig,'baseline'),p,args.output_dir)
