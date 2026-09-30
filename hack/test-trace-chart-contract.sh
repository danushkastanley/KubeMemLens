#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
# Relative fixtures are also readable inside CI's /work Helm container.
trace_chart_dir=$(mktemp -d './.trace-chart-contract.XXXXXX')
trap 'rm -rf -- "${trace_chart_dir}"' EXIT
python3 - "${trace_chart_dir}" <<'PY'
import copy,json,sys
from pathlib import Path
folder=Path(sys.argv[1])
values={'enabled':True,'profile':'development-linux-containerd',
        'acknowledgeUnqualifiedDevelopment':True,
        'image':{'repository':'example.invalid/trace','digest':'sha256:'+'a'*64},
        'acceptancePolicyConfigMap':'accepted-policy','apiTLSSecret':'api-tls',
        'acceptancePolicySHA256':'d'*64,
        'preflightServiceAccount':'trace-installer',
        'apiCABundle':'Y2E=','controlCertificateSHA256':'b'*64,'nodes':[]}
for suffix,arch in [('one','amd64'),('two','arm64')]:
    values['nodes'].append({'id':suffix,'name':'node-'+suffix,'uid':'node-'+suffix+'-uid',
        'architecture':arch,'kernelVersion':'6.12.0','runtimeVersion':'containerd://2.2.0','tlsSecret':'node-'+suffix+'-tls',
        'certificateSHA256':'c'*64,'kubeletCgroupRoot':'/kubelet'})
(folder/'valid.json').write_text(json.dumps(values))
numeric=copy.deepcopy(values)
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
cases.append((['preflightServiceAccount'],''))
cases.append((['nodes',0,'runtimeVersion'],'containerd://'))
cases.append((['apiCABundle'],'a'*90000))
for index,(path,value) in enumerate(cases):
    changed=copy.deepcopy(values);target=changed
    for key in path[:-1]:target=target[key]
    target[path[-1]]=value
    (folder/f'invalid-{index}.json').write_text(json.dumps(changed))
PY
helm template trial charts/kube-memlens-trace > "${trace_chart_dir}/disabled.yaml"
helm template standard charts/kube-memlens > "${trace_chart_dir}/standard.yaml"
helm template trial charts/kube-memlens-trace --namespace trace-admin \
  -f "${trace_chart_dir}/valid.json" > "${trace_chart_dir}/enabled.yaml"
for invalid in "${trace_chart_dir}"/invalid-*.json; do
  if helm template trial charts/kube-memlens-trace --namespace trace-admin \
    -f "${invalid}" > "${trace_chart_dir}/rejected.yaml" 2> "${trace_chart_dir}/error.log"; then
    echo "invalid trace profile rendered: ${invalid##*/}" >&2
    exit 1
  fi
done
for namespace in default kube-system kube-public kube-node-lease kube-memlens; do
  if helm template trial charts/kube-memlens-trace --namespace "${namespace}" \
    -f "${trace_chart_dir}/valid.json" > "${trace_chart_dir}/rejected.yaml" 2> "${trace_chart_dir}/error.log"; then
    echo 'trace profile accepted a reserved namespace' >&2
    exit 1
  fi
done
go run ./hack/trace-chart-contract "${trace_chart_dir}/disabled.yaml" \
  "${trace_chart_dir}/enabled.yaml" "${trace_chart_dir}/standard.yaml" trace-admin
helm template 123 charts/kube-memlens-trace --namespace 456 \
  -f "${trace_chart_dir}/numeric.json" > "${trace_chart_dir}/numeric.yaml"
go run ./hack/trace-chart-contract "${trace_chart_dir}/disabled.yaml" \
  "${trace_chart_dir}/numeric.yaml" "${trace_chart_dir}/standard.yaml" 456
