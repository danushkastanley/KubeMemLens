#!/usr/bin/env bash

# Sourced inside the owned kind verifier, after normal production preflight.
# The parent owns and validates these private fixture variables.
# shellcheck disable=SC2154
node_qualification_baseline() {
  local profile=${NODE_CONTEXT_QUALIFICATION_PROFILE:?}
  python3 - "${profile}" "${node_image}" "${work_dir}/qualification-start" <<'PY'
import pathlib,sys
sys.path.insert(0,'hack/node-qualification')
from common import load,require,utc_text
from profiles import validate_profile
p=validate_profile(load(sys.argv[1]))
require(p['profileClass']=='local-kind' and p['nodeImage']==sys.argv[2], 'profile/fixture mismatch')
pathlib.Path(sys.argv[3]).write_text(utc_text())
PY
  python3 hack/node-qualification/workload.py --profile "${profile}" --node "${node}" > "${work_dir}/qualification-workload.json"
  kctl apply -f "${work_dir}/qualification-workload.json" >/dev/null
  kctl rollout status deployment/qualification-load -n node-qualification-load --timeout=120s >/dev/null
  kctl rollout status daemonset/kindnet -n kube-system --timeout=60s >/dev/null
  node_qualification_measure baseline
}

node_qualification_measure() {
  python3 hack/node-qualification/sample.py --cluster "${cluster}" --node "${node}" \
    --kubeconfig "${kubeconfig}" --profile "${NODE_CONTEXT_QUALIFICATION_PROFILE}" \
    --phase "$1" --output "${work_dir}/qualification-$1.json"
  python3 - "$1" "${work_dir}/qualification-$1.json" "${artifact_dir}/measurements-$1.json" <<'PY'
import sys
sys.path.insert(0,'hack/node-qualification')
from common import load,privacy,write_new
from evidence import validate_samples
d=load(sys.argv[2]); privacy(d); validate_samples(d['samples'],sys.argv[1])
write_new(sys.argv[3],d)
PY
}

node_qualification_lifecycle() {
  helm get values kube-memlens --all --output json --kubeconfig "${kubeconfig}" \
    --kube-context "kind-${cluster}" -n "${namespace}" > "${work_dir}/qualification-values.json"
  printf '%s' "${audience}" > "${work_dir}/qualification-audience"
  python3 hack/node-qualification/lifecycle.py --cluster "${cluster}" --node "${node}" \
    --kubeconfig "${kubeconfig}" --profile "${NODE_CONTEXT_QUALIFICATION_PROFILE}" \
    --output "${work_dir}/qualification-lifecycle.json"
  python3 hack/node-qualification/record_kind.py --profile "${NODE_CONTEXT_QUALIFICATION_PROFILE}" \
    --work-dir "${work_dir}" --output-dir "${artifact_dir}"
}

node_qualification_lifecycle_diagnostic() {
  python3 hack/node-qualification/lifecycle.py --cluster "${cluster}" --node "${node}" \
    --kubeconfig "${kubeconfig}" --profile "${NODE_CONTEXT_LIFECYCLE_PROFILE}" \
    --output "${work_dir}/qualification-lifecycle.json"
  python3 - "${NODE_CONTEXT_LIFECYCLE_PROFILE}" "${node_image}" "${work_dir}/qualification-lifecycle.json" "${artifact_dir}/lifecycle-check.json" <<'PY'
import sys
sys.path.insert(0,'hack/node-qualification')
from common import load,privacy,require,write_new
from profiles import validate_profile
p=validate_profile(load(sys.argv[1])); require(p['nodeImage']==sys.argv[2], 'diagnostic profile/image mismatch')
events=load(sys.argv[3]); privacy(events)
passed=all(e['state']=='passed' and e['identityVerified'] and e['freshEvidence'] and e['elapsedSeconds'] is not None and e['elapsedSeconds']<=p['budgets']['recoverySeconds'] for name,e in events.items() if name!='providerNodeReplacement')
passed=passed and events['sourceLoss']['staleRetained']
write_new(sys.argv[4],{'schemaVersion':1,'scope':'local-lifecycle-diagnostic','qualified':False,'profile':{'id':p['id'],'digest':p['profileDigest']},'lifecycle':events,'passed':passed})
raise SystemExit(0 if passed else 1)
PY
}
