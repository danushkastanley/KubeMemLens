#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
kubeconfig=${AGENTLESS_KUBECONFIG:?AGENTLESS_KUBECONFIG is required}
context=${AGENTLESS_CONTEXT:?AGENTLESS_CONTEXT is required}
artifact_dir=${AGENTLESS_ARTIFACT_DIR:?AGENTLESS_ARTIFACT_DIR is required}
namespace=kube-memlens-agentless-e2e
reader_namespace=kube-memlens-agentless-a
denied_namespace=kube-memlens-agentless-b
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-agentless.XXXXXX")
created=false
services_created=false
case "${context}" in kind-*) ;; *) echo 'agentless verification requires kind' >&2; exit 1 ;; esac
[ "${AGENTLESS_ACKNOWLEDGE:-}" = run-and-remove-agentless-fixture ] || { echo 'set AGENTLESS_ACKNOWLEDGE=run-and-remove-agentless-fixture' >&2; exit 1; }
kctl() { kubectl --kubeconfig "${kubeconfig}" --context "${context}" "$@"; }
cleanup() {
  local result=$?
  if [ "${services_created}" = true ]; then kctl delete apiservice v1.metrics.k8s.io v1beta1.metrics.k8s.io --ignore-not-found >/dev/null || result=1; fi
  if [ "${created}" = true ]; then kctl delete namespace "${namespace}" "${reader_namespace}" "${denied_namespace}" --ignore-not-found --wait=true --timeout=90s >/dev/null || result=1; fi
  rm -rf "${work_dir}"
  exit "${result}"
}
trap cleanup EXIT
trap 'echo "agentless verification failed at line ${LINENO}" >&2' ERR
for object in namespace/${namespace} namespace/${reader_namespace} namespace/${denied_namespace} apiservice/v1alpha1.memory.kubememlens.io apiservice/v1.metrics.k8s.io apiservice/v1beta1.metrics.k8s.io; do
  if kctl get "${object}" >/dev/null 2>&1; then echo 'refusing to replace existing metrics fixture resources' >&2; exit 1; fi
done
mkdir -p "${artifact_dir}"
go build -trimpath -o "${work_dir}/fixture" ./hack/fixtures/metrics-api
go build -trimpath -o "${work_dir}/probe" ./hack/fixtures/agentless-probe
"${work_dir}/fixture" --write-cert-dir "${work_dir}/tls" --namespace "${namespace}"
image="kube-memlens:agentless-fixture-${context#kind-}"
docker build -t "${image}" hack/fixtures/metrics-api > "${work_dir}/build.log" 2>&1 || { tail -30 "${work_dir}/build.log" >&2; exit 1; }
kind load docker-image "${image}" --name "${context#kind-}" >/dev/null
kctl create namespace "${namespace}" >/dev/null
created=true
kctl create namespace "${reader_namespace}" >/dev/null
kctl create namespace "${denied_namespace}" >/dev/null
kctl create secret tls metrics-fixture-tls -n "${namespace}" --cert "${work_dir}/tls/tls.crt" --key "${work_dir}/tls/tls.key" >/dev/null
kctl apply -n "${reader_namespace}" -f - >/dev/null <<'YAML'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: restricted-reader
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: restricted-reader
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["list"]
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods"]
    verbs: ["list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: restricted-reader
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: restricted-reader
subjects:
  - kind: ServiceAccount
    name: restricted-reader
    namespace: kube-memlens-agentless-a
YAML
for scope in "${reader_namespace}" "${denied_namespace}"; do
  kctl apply -n "${scope}" -f - >/dev/null <<'YAML'
apiVersion: v1
kind: Pod
metadata:
  name: fixture-pod
spec:
  automountServiceAccountToken: false
  nodeSelector:
    restricted-fixture.invalid/unschedulable: "true"
  containers:
    - name: worker
      image: registry.k8s.io/pause:3.10
      resources:
        requests: {memory: 64Mi}
    - name: missing
      image: registry.k8s.io/pause:3.10
YAML
done
pod_uid=$(kctl get pod fixture-pod -n "${reader_namespace}" -o jsonpath='{.metadata.uid}')
kctl config view --raw --minify -o json > "${work_dir}/cluster.json"
kctl create token restricted-reader -n "${reader_namespace}" --duration=10m > "${work_dir}/token"
python3 - "${work_dir}" <<'PYCONFIG'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
original = json.loads((root / 'cluster.json').read_text())
config = {'apiVersion': 'v1', 'kind': 'Config',
          'clusters': [{'name': 'fixture', 'cluster': original['clusters'][0]['cluster']}],
          'users': [{'name': 'reader', 'user': {'token': (root / 'token').read_text().strip()}}],
          'contexts': [{'name': 'fixture', 'context': {'cluster': 'fixture', 'user': 'reader'}}],
          'current-context': 'fixture'}
(root / 'reader.json').write_text(json.dumps(config))
PYCONFIG
probe() {
  local label=$1
  "${work_dir}/probe" --kubeconfig "${work_dir}/reader.json" --namespace "${reader_namespace}" \
    --denied-namespace "${denied_namespace}" --expect "${label}" --artifact "${artifact_dir}/${label}.json"
}
probe missing
kctl apply -n "${namespace}" -f - >/dev/null <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-fixture
spec:
  replicas: 1
  selector:
    matchLabels:
      app: metrics-fixture
  template:
    metadata:
      labels:
        app: metrics-fixture
    spec:
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: fixture
          image: ${image}
          imagePullPolicy: Never
          args: ["--pod-uid=${pod_uid}"]
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          ports:
            - containerPort: 9443
          resources:
            requests: {cpu: 10m, memory: 16Mi}
            limits: {cpu: 100m, memory: 64Mi}
          readinessProbe:
            httpGet:
              path: /healthz
              port: 9443
              scheme: HTTPS
          volumeMounts:
            - name: tls
              mountPath: /tls
              readOnly: true
      volumes:
        - name: tls
          secret:
            secretName: metrics-fixture-tls
            defaultMode: 0440
---
apiVersion: v1
kind: Service
metadata:
  name: metrics-fixture
spec:
  selector:
    app: metrics-fixture
  ports:
    - port: 443
      targetPort: 9443
YAML
kctl rollout status deployment/metrics-fixture -n "${namespace}" --timeout=120s >/dev/null
ca_bundle=$(base64 < "${work_dir}/tls/ca.crt" | tr -d '\n')
services_created=true
for version in v1 v1beta1; do
  kctl apply -f - >/dev/null <<YAML
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: ${version}.metrics.k8s.io
spec:
  group: metrics.k8s.io
  version: ${version}
  groupPriorityMinimum: 100
  versionPriority: 100
  service:
    namespace: ${namespace}
    name: metrics-fixture
    port: 443
  caBundle: ${ca_bundle}
YAML
  kctl wait --for=condition=Available "apiservice/${version}.metrics.k8s.io" --timeout=120s >/dev/null
done
probe measured
kctl delete rolebinding restricted-reader -n "${reader_namespace}" >/dev/null
probe revoked
source_dirty=false
[ -z "$(git status --porcelain)" ] || source_dirty=true
jq -n --arg commit "$(git rev-parse HEAD)" --argjson sourceDirty "${source_dirty}" \
  --slurpfile measured "${artifact_dir}/measured.json" --slurpfile missing "${artifact_dir}/missing.json" \
  --slurpfile revoked "${artifact_dir}/revoked.json" \
  '{outcome:"passed",sourceCommit:$commit,sourceDirty:$sourceDirty,checks:{measured:$measured[0],missing:$missing[0],revoked:$revoked[0]}}' > "${artifact_dir}/summary.json"
echo 'agentless kind verification passed'
