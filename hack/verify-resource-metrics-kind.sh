#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
kubeconfig=${RESOURCE_METRICS_KUBECONFIG:?RESOURCE_METRICS_KUBECONFIG is required}
context=${RESOURCE_METRICS_CONTEXT:?RESOURCE_METRICS_CONTEXT is required}
artifact_dir=${RESOURCE_METRICS_ARTIFACT_DIR:?RESOURCE_METRICS_ARTIFACT_DIR is required}
namespace=kube-memlens-metrics-e2e
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-metrics.XXXXXX")
created=false
services_created=false
case "${context}" in kind-*) ;; *) echo 'resource metrics verification requires kind' >&2; exit 1 ;; esac
[ "${RESOURCE_METRICS_ACKNOWLEDGE:-}" = run-and-remove-metrics-api-fixture ] || { echo 'set RESOURCE_METRICS_ACKNOWLEDGE=run-and-remove-metrics-api-fixture' >&2; exit 1; }
kctl() { kubectl --kubeconfig "${kubeconfig}" --context "${context}" "$@"; }
cleanup() {
  local result=$?
  if [ "${services_created}" = true ]; then kctl delete apiservice v1.metrics.k8s.io v1beta1.metrics.k8s.io --ignore-not-found >/dev/null || result=1; fi
  if [ "${created}" = true ]; then kctl delete namespace "${namespace}" --wait=true --timeout=90s >/dev/null || result=1; fi
  rm -rf "${work_dir}"
  exit "${result}"
}
trap cleanup EXIT
trap 'echo "resource metrics verification failed at line ${LINENO}" >&2' ERR
for object in namespace/${namespace} apiservice/v1.metrics.k8s.io apiservice/v1beta1.metrics.k8s.io; do
  if kctl get "${object}" >/dev/null 2>&1; then echo 'refusing to replace existing metrics fixture resources' >&2; exit 1; fi
done
mkdir -p "${artifact_dir}"
go build -trimpath -o "${work_dir}/fixture" ./hack/fixtures/metrics-api
go build -trimpath -o "${work_dir}/probe" ./hack/fixtures/metrics-probe
"${work_dir}/fixture" --write-cert-dir "${work_dir}/tls" --namespace "${namespace}"
image="kube-memlens:metrics-fixture-${context#kind-}"
docker build -t "${image}" hack/fixtures/metrics-api > "${work_dir}/build.log" 2>&1 || { tail -30 "${work_dir}/build.log" >&2; exit 1; }
kind load docker-image "${image}" --name "${context#kind-}" >/dev/null
kctl create namespace "${namespace}" >/dev/null
created=true
kctl create secret tls metrics-fixture-tls -n "${namespace}" --cert "${work_dir}/tls/tls.crt" --key "${work_dir}/tls/tls.key" >/dev/null
kctl apply -n "${namespace}" -f - >/dev/null <<'YAML'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: metrics-reader
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: metrics-reader
rules:
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods"]
    verbs: ["list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: metrics-reader
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: metrics-reader
subjects:
  - kind: ServiceAccount
    name: metrics-reader
    namespace: kube-memlens-metrics-e2e
YAML
kctl create token metrics-reader -n "${namespace}" --duration=10m > "${work_dir}/reader-token"
probe() {
  local label=$1 scope=$2 expected=$3 version=$4
  "${work_dir}/probe" --kubeconfig "${kubeconfig}" --context "${context}" \
    --namespace "${scope}" --token-file "${work_dir}/reader-token" \
    --state "${expected}" --version "${version}" --artifact "${artifact_dir}/${label}.json"
}
probe missing-provider "${namespace}" missing-provider ''
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
probe stable "${namespace}" available v1
probe denied default forbidden ''
kctl delete apiservice v1.metrics.k8s.io >/dev/null
probe transition "${namespace}" available v1beta1
source_dirty=false
[ -z "$(git status --porcelain)" ] || source_dirty=true
jq -n --arg commit "$(git rev-parse HEAD)" --argjson sourceDirty "${source_dirty}" \
  --slurpfile stable "${artifact_dir}/stable.json" --slurpfile transition "${artifact_dir}/transition.json" \
  --slurpfile denied "${artifact_dir}/denied.json" --slurpfile missing "${artifact_dir}/missing-provider.json" \
  '{outcome:"passed",sourceCommit:$commit,sourceDirty:$sourceDirty,checks:{stable:$stable[0],transition:$transition[0],denied:$denied[0],missingProvider:$missing[0]}}' > "${artifact_dir}/summary.json"
echo 'resource metrics kind verification passed'
