#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

kubeconfig=${EVIDENCE_KUBECONFIG:?EVIDENCE_KUBECONFIG is required}
context=${EVIDENCE_CONTEXT:?EVIDENCE_CONTEXT is required}
cli=${EVIDENCE_CLI:?EVIDENCE_CLI is required}
artifact_dir=${EVIDENCE_ARTIFACT_DIR:?EVIDENCE_ARTIFACT_DIR is required}
namespace=kube-memlens-evidence-e2e
case "${context}" in kind-*) ;; *) echo 'evidence verification requires kind' >&2; exit 1 ;; esac
[ "${EVIDENCE_ACKNOWLEDGE:-}" = run-and-remove-evidence-fixture ] || { echo 'set EVIDENCE_ACKNOWLEDGE=run-and-remove-evidence-fixture' >&2; exit 1; }
kctl() { kubectl --kubeconfig "${kubeconfig}" --context "${context}" "$@"; }
if kctl get namespace "${namespace}" >/dev/null 2>&1; then
  echo 'refusing to replace existing evidence fixture namespace' >&2
  exit 1
fi
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-evidence.XXXXXX")
created=false
cleanup() {
  local result=$?
  if [ "${created}" = true ]; then
    kctl delete namespace "${namespace}" --wait=true --timeout=90s >/dev/null || result=1
  fi
  rm -rf "${work_dir}"
  exit "${result}"
}
trap cleanup EXIT
mkdir -p "${artifact_dir}"
kctl create namespace "${namespace}" >/dev/null
created=true
kctl apply -n "${namespace}" -f - >/dev/null <<'YAML'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: evidence-reader
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: evidence-reader
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: evidence-reader
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: evidence-reader
subjects:
  - kind: ServiceAccount
    name: evidence-reader
    namespace: kube-memlens-evidence-e2e
YAML
kctl config view --raw --minify -o json > "${work_dir}/cluster.json"
kctl create token evidence-reader -n "${namespace}" --duration=10m > "${work_dir}/token"
python3 - "${work_dir}" <<'PY'
import json
import pathlib
import sys
root = pathlib.Path(sys.argv[1])
original = json.loads((root / 'cluster.json').read_text())
config = {
    'apiVersion': 'v1', 'kind': 'Config',
    'clusters': [{'name': 'fixture', 'cluster': original['clusters'][0]['cluster']}],
    'users': [{'name': 'reader', 'user': {'token': (root / 'token').read_text().strip()}}],
    'contexts': [{'name': 'fixture', 'context': {'cluster': 'fixture', 'user': 'reader'}}],
    'current-context': 'fixture',
}
(root / 'reader.json').write_text(json.dumps(config))
PY
"${cli}" --kubeconfig "${work_dir}/reader.json" --mode=restricted status -n "${namespace}" --output=json > "${artifact_dir}/restricted.json"
"${cli}" --kubeconfig "${work_dir}/reader.json" status -n "${namespace}" --output=json > "${artifact_dir}/auto.json"
for check in deep denied cluster; do
  args=(--mode=restricted status -n kube-system --output=json)
  if [ "${check}" = deep ]; then args=(--mode=deep status -n "${namespace}" --output=json); fi
  if [ "${check}" = cluster ]; then args=(--mode=restricted status --output=json); fi
  if "${cli}" --kubeconfig "${work_dir}/reader.json" "${args[@]}" > "${artifact_dir}/${check}.json" 2> "${work_dir}/error"; then
    echo "unexpected successful ${check} access" >&2
    exit 1
  fi
done
python3 - "${artifact_dir}" <<'PY'
import json
import pathlib
import sys
root = pathlib.Path(sys.argv[1])
for name in ('restricted', 'auto'):
    report = json.loads((root / (name + '.json')).read_text())
    plan = report['evidence']
    assert plan['mode'] == 'restricted' and plan['completeness'] == 'partial'
    assert 'store' not in report
    queries = {q['query']: q for q in plan['queries']}
    assert queries['current']['reason'] == 'query-not-implemented'
for name in ('deep', 'denied', 'cluster'):
    report = json.loads((root / (name + '.json')).read_text())
    assert report['evidence']['state'] == 'unavailable'
    assert any(s['availability'] == 'forbidden' for s in report['evidence']['sources'])
for path in root.glob('*.json'):
    content = path.read_text()
    for forbidden in ('kube-system', 'kube-memlens-evidence-e2e', 'Bearer ', 'client-key-data', 'token', 'certificate-authority-data'):
        assert forbidden not in content, (path.name, forbidden)
print('PASS scoped restricted discovery, auto selection, explicit deep denial, cross-namespace denial, cluster denial and sanitised reports')
PY
