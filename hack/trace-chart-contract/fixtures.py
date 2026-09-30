import copy
import json
import re
import sys
from pathlib import Path


def write_fixtures(folder, image, use_defaults):
    if not re.fullmatch(r'[a-z0-9][a-z0-9./:_-]*@sha256:[0-9a-f]{64}', image):
        raise SystemExit('expected an exact image reference')
    repository,digest=image.split('@')
    values={'enabled':True,'profile':'development-linux-containerd',
            'acknowledgeUnqualifiedDevelopment':True,
            'image':{'repository':repository,'digest':digest},
            'acceptancePolicyConfigMap':'accepted-policy','apiTLSSecret':'api-tls',
            'acceptancePolicySHA256':'d'*64,
            'auditReferenceKeySecret':'audit-key','auditReferenceKeySHA256':'e'*64,
            'preflightServiceAccount':'trace-installer',
            'apiCABundle':'Y2E=','controlCertificateSHA256':'b'*64,'nodes':[]}
    for suffix,arch in [('one','amd64'),('two','arm64')]:
        values['nodes'].append({'id':suffix,'name':'node-'+suffix,'uid':'node-'+suffix+'-uid',
            'architecture':arch,'kernelVersion':'6.12.0','runtimeVersion':'containerd://2.2.0','tlsSecret':'node-'+suffix+'-tls',
            'certificateSHA256':'c'*64,'kubeletCgroupRoot':'/kubelet'})
    # A packaged chart must use its own pinned image defaults.
    valid=copy.deepcopy(values)
    if use_defaults: del valid['image']
    (folder/'valid.json').write_text(json.dumps(valid))
    numeric=copy.deepcopy(valid)
    numeric['nodes'][0]['id']='123'
    numeric['nodes'][1]['id']='456'
    (folder/'numeric.json').write_text(json.dumps(numeric))
    cases=[(['acknowledgeUnqualifiedDevelopment'],False),(['profile'],'qualified'),
           (['image','digest'],'latest'),(['nodes'],[]),(['maxNodeTraces'],3),
           (['extraArgs'],['--arbitrary']),(['nodes',0,'architecture'],'riscv64'),
           (['nodes',1,'name'],'node-one'),(['nodes',1,'uid'],'node-one-uid'),
           (['nodes',1,'id'],'one'),(['nodes',0,'name'],'bad..name'),
           (['nodes',0,'kubeletCgroupRoot'],'/../kubelet'),
           (['nodes',0,'kubeletCgroupRoot'],'/'+'a'*64),
           (['controlCertificateSHA256'],'not-a-pin')]
    cases.append((['acceptancePolicySHA256'],''))
    cases.append((['auditReferenceKeySecret'],''))
    cases.append((['auditReferenceKeySHA256'],''))
    cases.append((['auditReferenceKeySHA256'],'not-a-digest'))
    cases.append((['preflightServiceAccount'],''))
    cases.append((['nodes',0,'runtimeVersion'],'containerd://'))
    cases.append((['apiCABundle'],'a'*90000))
    for index,(path,value) in enumerate(cases):
        changed=copy.deepcopy(values);target=changed
        for key in path[:-1]:target=target[key]
        target[path[-1]]=value
        (folder/f'invalid-{index}.json').write_text(json.dumps(changed))


if __name__ == "__main__":
    write_fixtures(Path(sys.argv[1]), sys.argv[2], sys.argv[3] == "true")
