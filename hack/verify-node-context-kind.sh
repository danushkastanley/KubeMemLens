#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

[ "${NODE_CONTEXT_ACKNOWLEDGE:-}" = create-and-remove-node-context-kind ] || { echo 'set NODE_CONTEXT_ACKNOWLEDGE=create-and-remove-node-context-kind' >&2; exit 1; }
artifact_dir=${NODE_CONTEXT_ARTIFACT_DIR:?NODE_CONTEXT_ARTIFACT_DIR is required}
node_image=${NODE_CONTEXT_NODE_IMAGE:-kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5}
case "${node_image}" in kindest/node:v1.3[67].*@sha256:*) ;; *) echo 'use a pinned local Kubernetes 1.36 or 1.37 image' >&2; exit 1 ;; esac
python3 - "${node_image}" <<'PY'
import re,sys
assert re.fullmatch(r'kindest/node:v1\.(36|37)\.\d+@sha256:[a-f0-9]{64}',sys.argv[1]), 'invalid pinned node image'
PY
cluster=${NODE_CONTEXT_CLUSTER:-kube-memlens-node-context-e2e}
case "${cluster}" in kube-memlens-node-context-*) ;; *) echo 'unexpected disposable cluster prefix' >&2; exit 1 ;; esac
namespace=kube-memlens-node-context
case "${NODE_CONTEXT_MEASUREMENT_METHOD:-docker}" in
  docker) ;;
  kubernetes) [ -n "${NODE_CONTEXT_QUALIFICATION_PROFILE:-}" ] || { echo 'Kubernetes measurement requires a full qualification profile' >&2; exit 1; } ;;
  *) echo 'unknown qualification measurement method' >&2; exit 1 ;;
esac
for command in kind kubectl docker go python3; do command -v "${command}" >/dev/null; done
existing=$(kind get clusters)
if printf '%s\n' "${existing}" | grep -Fxq "${cluster}"; then echo 'refusing to replace an existing kind cluster' >&2; exit 1; fi
[ ! -e "${artifact_dir}/summary.json" ] || { echo 'refusing to replace existing evidence' >&2; exit 1; }
if [ -n "${NODE_CONTEXT_QUALIFICATION_PROFILE:-}" ]; then
  [ -z "${NODE_CONTEXT_LIFECYCLE_PROFILE:-}" ] || { echo 'select full qualification or lifecycle diagnosis, not both' >&2; exit 1; }
  [ "${NODE_CONTEXT_VERIFY_INGESTION:-false}" = true ] || { echo 'qualification requires ingestion verification' >&2; exit 1; }
  for output in qualification-observations.json qualification.json qualification-evaluation.json measurements-baseline.json measurements-enabled.json; do
    [ ! -e "${artifact_dir}/${output}" ] || { echo 'refusing to replace qualification evidence' >&2; exit 1; }
  done
fi
if [ -n "${NODE_CONTEXT_LIFECYCLE_PROFILE:-}" ]; then
  [ "${NODE_CONTEXT_VERIFY_INGESTION:-false}" = true ] || { echo 'lifecycle diagnosis requires ingestion verification' >&2; exit 1; }
  [ ! -e "${artifact_dir}/lifecycle-check.json" ] || { echo 'refusing to replace lifecycle diagnosis' >&2; exit 1; }
fi
if [ -n "${NODE_CONTEXT_OBSERVER_PROFILE:-}" ]; then
  [ "${NODE_CONTEXT_VERIFY_INGESTION:-false}" = true ] || { echo 'observer check requires ingestion verification' >&2; exit 1; }
  [ -z "${NODE_CONTEXT_QUALIFICATION_PROFILE:-}${NODE_CONTEXT_LIFECYCLE_PROFILE:-}" ] || { echo 'observer check must run separately from qualification' >&2; exit 1; }
  if [ -e "${artifact_dir}/observer-baseline.json" ] || [ -e "${artifact_dir}/observer-enabled.json" ]; then
    echo 'observer evidence already exists' >&2; exit 1
  fi
  python3 - "${NODE_CONTEXT_OBSERVER_PROFILE}" "${node_image}" <<'PY'
import sys
sys.path.insert(0,'hack/node-qualification')
from common import load,require
from profiles import validate_profile
p=validate_profile(load(sys.argv[1]))
require(p['profileClass']=='local-kind' and p['nodeImage']==sys.argv[2], 'observer profile/fixture mismatch')
PY
fi
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-node-context.XXXXXX")
kubeconfig=${work_dir}/kubeconfig
created=false
image_created=false
volume_image_created=false
image="kube-memlens-node-context:$(basename "${work_dir}")"
if docker image inspect "${image}" >/dev/null 2>&1; then echo 'refusing to replace an existing image' >&2; exit 1; fi
kctl() { kubectl --kubeconfig "${kubeconfig}" --context "kind-${cluster}" "$@"; }
cleanup() {
  local result=$?
  trap - EXIT
  if [ "${created}" = true ]; then kind delete cluster --name "${cluster}" > "${work_dir}/cleanup.log" 2>&1 || result=1; fi
  if [ "${image_created}" = true ]; then docker image rm "${image}" >/dev/null 2>&1 || result=1; fi
  if [ "${volume_image_created}" = true ]; then docker image rm "${volume_image}" >/dev/null 2>&1 || result=1; fi
  if [ "${result}" -ne 0 ]; then
    # Only bounded failure codes leave the private work directory.
    for log in "${work_dir}"/*-wait.log; do [ ! -f "${log}" ] || tail -2 "${log}" >&2; done
    for log in "${work_dir}"/allowed.log "${work_dir}"/denied.log "${work_dir}"/bad-ca.log; do
      [ ! -f "${log}" ] || grep '^node-context read failed:' "${log}" >&2 || true
    done
  fi
  rm -rf -- "${work_dir}"
  exit "${result}"
}
trap cleanup EXIT
mkdir -p "${artifact_dir}" "${work_dir}/image"
python3 - "${work_dir}/source.json" <<'PY'
import hashlib,json,pathlib,subprocess,sys
files=[pathlib.Path('go.mod'),pathlib.Path('go.sum')]
files += [p for folder in ('cmd','internal') for p in pathlib.Path(folder).rglob('*.go') if not p.name.endswith('_test.go')]
digest=hashlib.sha256()
for p in sorted(files):
    digest.update(p.as_posix().encode()+b'\0'+p.read_bytes()+b'\0')
pathlib.Path(sys.argv[1]).write_text(json.dumps({
 'sourceCommit':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),
 'sourceDirty':bool(subprocess.check_output(['git','status','--porcelain']).strip()),
 'sourceTreeSHA256':digest.hexdigest()}))
PY
architecture=$(docker info --format '{{.Architecture}}')
case "${architecture}" in aarch64|arm64) architecture=arm64 ;; x86_64|amd64) architecture=amd64 ;; *) echo 'unsupported local architecture' >&2; exit 1 ;; esac
CGO_ENABLED=0 GOOS=linux GOARCH="${architecture}" go build -trimpath -o "${work_dir}/image/producer" ./cmd/memlens-node-context
cat > "${work_dir}/image/Dockerfile" <<'EOF'
FROM scratch
COPY --chmod=0555 producer /memlens-node-context
USER 65532:65532
ENTRYPOINT ["/memlens-node-context"]
EOF
if [ "${NODE_CONTEXT_VERIFY_INGESTION:-false}" = true ]; then
  for command in helm curl; do command -v "${command}" >/dev/null; done
  for binary in memlens-agent memlens-collector memlens-cert-bootstrap kubectl-memlens; do
    CGO_ENABLED=0 GOOS=linux GOARCH="${architecture}" go build -trimpath -o "${work_dir}/image/${binary}" "./cmd/${binary}"
    printf '\nCOPY --chmod=0555 %s /%s\n' "${binary}" "${binary}" >> "${work_dir}/image/Dockerfile"
  done
fi
image_created=true
docker build -t "${image}" "${work_dir}/image" > "${work_dir}/image-build.log" 2>&1
docker image inspect "${image}" --format '{{.Id}}' > "${work_dir}/image-id"
kind_args=()
if [ "${NODE_CONTEXT_VERIFY_VOLUME_STATS:-false}" = true ]; then
  [ "${NODE_CONTEXT_VERIFY_INGESTION:-false}" = true ] || { echo 'volume verification requires ingestion' >&2; exit 1; }
  cat > "${work_dir}/kind.yaml" <<'YAML'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
kubeadmConfigPatches:
  - |
    apiVersion: kubelet.config.k8s.io/v1beta1
    kind: KubeletConfiguration
    volumeStatsAggPeriod: 5s
YAML
  kind_args+=(--config "${work_dir}/kind.yaml")
fi
created=true
kind create cluster --name "${cluster}" --image "${node_image}" --kubeconfig "${kubeconfig}" "${kind_args[@]}" --wait 90s > "${work_dir}/kind-create.log" 2>&1
kind load docker-image "${image}" --name "${cluster}" > "${work_dir}/image-load.log" 2>&1
node=$(kind get nodes --name "${cluster}")
kctl get node "${node}" -o json > "${work_dir}/node.json"
ip=$(python3 - "${work_dir}/node.json" <<'PY'
import json, sys
n = json.load(open(sys.argv[1]))
print(sorted(a['address'] for a in n['status']['addresses'] if a['type']=='InternalIP')[0])
PY
)
source hack/lib/node-context-fixture.sh
prepare_node_context_tls "${cluster}" "${work_dir}" "${node}" "${ip}"
kctl wait node "${node}" --for=condition=Ready --timeout=90s >/dev/null
audience=$(kctl get --raw /.well-known/openid-configuration | python3 -c 'import json,sys; print(json.load(sys.stdin)["issuer"])')
kctl create namespace "${namespace}" >/dev/null
kctl create configmap node-context-trust -n "${namespace}" --from-file=ca.crt="${work_dir}/serving-ca.crt" --from-literal=invalid.crt=invalid-certificate >/dev/null
kctl apply -n "${namespace}" -f - >/dev/null <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata: {name: node-context}
---
apiVersion: v1
kind: ServiceAccount
metadata: {name: no-stats}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: node-context-node-object}
rules:
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: node-context-stats}
rules:
  - apiGroups: [""]
    resources: ["nodes/stats"]
    verbs: ["get"]
EOF
kctl create clusterrolebinding node-context-node-object --clusterrole=node-context-node-object --serviceaccount="${namespace}:node-context" --serviceaccount="${namespace}:no-stats" >/dev/null
kctl create clusterrolebinding node-context-stats --clusterrole=node-context-stats --serviceaccount="${namespace}:node-context" >/dev/null
for resource in nodes/proxy nodes/metrics nodes/log pods/proxy pods/exec secrets; do
  args=(get "${resource%%/*}")
  if [[ "${resource}" = */* ]]; then args+=(--subresource="${resource#*/}"); fi
  result=$(kctl auth can-i "${args[@]}" --as="system:serviceaccount:${namespace}:node-context" 2>/dev/null || true)
  [ "${result}" = no ] || { echo "unexpected ${resource} permission in optional producer role" >&2; exit 1; }
done
node_context_probe "${work_dir}" "${namespace}" "${node}" "${image}" "${audience}" allowed node-context ca.crt Succeeded
node_context_probe "${work_dir}" "${namespace}" "${node}" "${image}" "${audience}" denied no-stats ca.crt Failed
node_context_probe "${work_dir}" "${namespace}" "${node}" "${image}" "${audience}" bad-ca node-context invalid.crt Failed
node_context_probe "${work_dir}" "${namespace}" "${node}" "${image}" "${audience}" wrong-node node-context ca.crt Failed unassigned-fixture-node
grep -q '^node-context read failed: access-denied$' "${work_dir}/denied.log"
grep -q '^node-context read failed: untrusted-tls$' "${work_dir}/bad-ca.log"
grep -q '^node-context read failed: invalid-target$' "${work_dir}/wrong-node.log"
if [ "${NODE_CONTEXT_VERIFY_INGESTION:-false}" = true ]; then
  source hack/lib/node-context-ingestion.sh
  node_context_ingestion "${work_dir}" "${namespace}" "${node}" "${image}" "${audience}" "${ip}"
fi
python3 - "${work_dir}" "${artifact_dir}" "${node_image}" "${NODE_CONTEXT_OBSERVER_PROFILE:-}" "${NODE_CONTEXT_MEASUREMENT_METHOD:-docker}" <<'PY'
import hashlib, json, pathlib, sys
root, output = map(pathlib.Path, sys.argv[1:3])
document = json.loads((root/'allowed.log').read_text())
assert document['kind']=='NodeContextDiagnostic' and document['schemaVersion']==1 and document['redacted']
report = document['observation']
node = json.loads((root/'node.json').read_text())
assert report['availability']=='available'
assert report['evidence']['source']=='kubelet-summary'
assert report['evidence']['freshness']=='fresh'
assert report['nodeName']==node['metadata']['name'] and report['nodeUID']==''
assert report['stats']['memory']['usageBytes']>0
assert report['context']['capacityBytes']>0
stats = report['stats']
summary = {'schemaVersion':1, 'nodeImage':sys.argv[3], **json.loads((root/'source.json').read_text()),
 'fixtureImageDigest':(root/'image-id').read_text().strip(),
 'kubernetes':node['status']['nodeInfo']['kubeletVersion'],
 'kernel':node['status']['nodeInfo']['kernelVersion'],
 'runtime':node['status']['nodeInfo']['containerRuntimeVersion'],
 'architecture':node['status']['nodeInfo']['architecture'],
 'binarySHA256':hashlib.sha256((root/'image/producer').read_bytes()).hexdigest(),
 'directSummary':True, 'podBoundAudienceVerified':True, 'scheduledNodeBound':True, 'statsPermissionDenied':True,
 'untrustedTLSRejected':True, 'proxyPermission':False, 'hostMounts':False,
 'servingTLS':'fixture certificate signed inside the disposable kind node',
 'provenance':stats['provenance'], 'psiAvailable':stats['memory'].get('psi') is not None,
 'swapAvailable':stats.get('swap') is not None, 'systemCategories':[s['category'] for s in stats.get('systemContainers',[])],
 'completeness':report['evidence']['completeness'], 'networkPolicy':'not qualified',
 'cleanup':'pending'}
ingestion=root/'ingestion-result.json'
if ingestion.exists(): summary['ingestion']=json.loads(ingestion.read_text())
analysis=root/'analysis-result.json'
if analysis.exists(): summary['analysis']=json.loads(analysis.read_text())
cockpit=root/'cockpit-result.json'
if cockpit.exists(): summary['cockpit']=json.loads(cockpit.read_text())
volumes=root/'volume-result.json'
if volumes.exists(): summary['volumes']=json.loads(volumes.read_text())
if sys.argv[4] or sys.argv[5]=='kubernetes':
    summary['hostMountsScope']='producer'
    summary['observer']={'method':'kubernetes-probes-v1','readOnlyHostCgroups':True,'hostPID':False,'hostNetwork':False}
    sys.path.insert(0,'hack/node-qualification')
    from common import privacy
    privacy(summary)
with (output/'summary.json').open('x') as file: file.write(json.dumps(summary,indent=2)+'\n')
PY
kind delete cluster --name "${cluster}" > "${work_dir}/cleanup.log" 2>&1
created=false
remaining=$(kind get clusters)
if printf '%s\n' "${remaining}" | grep -Fxq "${cluster}"; then echo 'cluster cleanup incomplete' >&2; exit 1; fi
docker image rm "${image}" >/dev/null
image_created=false
if [ "${volume_image_created}" = true ]; then
  docker image rm "${volume_image}" >/dev/null
  volume_image_created=false
fi
python3 - "${artifact_dir}/summary.json" "${NODE_CONTEXT_OBSERVER_PROFILE:-}" <<'PY'
import json, pathlib, sys
p=pathlib.Path(sys.argv[1]); data=json.loads(p.read_text()); data['cleanup']='passed'; p.write_text(json.dumps(data,indent=2)+'\n')
if sys.argv[2]:
    for phase in ('baseline','enabled'):
        observation=p.parent/('observer-'+phase+'.json')
        data=json.loads(observation.read_text()); data['cleanup']='passed'; observation.write_text(json.dumps(data,indent=2)+'\n')
PY
if [ -n "${NODE_CONTEXT_QUALIFICATION_PROFILE:-}" ]; then
  python3 hack/node-qualification/record_kind.py --profile "${NODE_CONTEXT_QUALIFICATION_PROFILE}" \
    --output-dir "${artifact_dir}" --cleanup-confirmed
fi
echo 'PASS direct kubelet TLS, Pod-bound token audience, stats-only RBAC, denial, bad CA, bounded normalisation and cleanup'
