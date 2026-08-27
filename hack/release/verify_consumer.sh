#!/usr/bin/env bash
set -Eeuo pipefail

bundle=${RELEASE_BUNDLE_DIR:?RELEASE_BUNDLE_DIR is required}
version=${RELEASE_VERSION:?RELEASE_VERSION is required}
commit=${RELEASE_COMMIT:?RELEASE_COMMIT is required}
image=${RELEASE_IMAGE:?RELEASE_IMAGE is required}
image_digest=${RELEASE_IMAGE_DIGEST:?RELEASE_IMAGE_DIGEST is required}
chart=${RELEASE_CHART:?RELEASE_CHART is required}
chart_digest=${RELEASE_CHART_DIGEST:?RELEASE_CHART_DIGEST is required}
identity=${RELEASE_CERTIFICATE_IDENTITY:?RELEASE_CERTIFICATE_IDENTITY is required}
issuer=${RELEASE_CERTIFICATE_ISSUER:-https://token.actions.githubusercontent.com}
repository=${RELEASE_REPOSITORY:-danushkastanley/KubeMemLens}
node_image=${RELEASE_NODE_IMAGE:-kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5}

case "${image_digest}" in sha256:????????????????????????????????????????????????????????????????) ;; *) exit 2 ;; esac
case "${chart_digest}" in sha256:????????????????????????????????????????????????????????????????) ;; *) exit 2 ;; esac

work_dir=$(mktemp -d "${RUNNER_TEMP:-/tmp}/kube-memlens-release-consumer.XXXXXX")
cluster_name="kube-memlens-release-consumer-${RANDOM}"
kubeconfig=${work_dir}/kubeconfig
cluster_created=false

cleanup() {
  status=$?
  if [ "${cluster_created}" = true ]; then
    kind delete cluster --name "${cluster_name}" >/dev/null 2>&1 || true
  fi
  rm -rf "${work_dir}"
  exit "${status}"
}
trap cleanup EXIT

for tool in cosign docker gh helm jq kind kubectl sha256sum tar; do
  command -v "${tool}" >/dev/null || {
    echo "clean consumer requires ${tool}" >&2
    exit 1
  }
done

if kind get clusters 2>/dev/null | grep -Fxq "${cluster_name}"; then
  echo "generated clean-consumer cluster name already exists: ${cluster_name}" >&2
  exit 1
fi
cluster_created=true

bundle=$(cd "${bundle}" && pwd)
chart_version=${version#v}
archive="${bundle}/kube-memlens_${chart_version}_linux_amd64.tar.gz"
chart_package="${bundle}/kube-memlens-${chart_version}.tgz"
subjects="${bundle}/release-subjects.txt"

(
  cd "${bundle}"
  sha256sum --check checksums.txt
)
grep -Fxq "image=${image}@${image_digest}" "${subjects}"
grep -Fxq "chart=${chart}@${chart_digest}" "${subjects}"
grep -Fxq "commit=${commit}" "${subjects}"
grep -Fxq "tag=${version}" "${subjects}"

cosign verify-blob \
  --bundle "${bundle}/checksums.txt.sigstore.json" \
  --certificate-identity "${identity}" \
  --certificate-oidc-issuer "${issuer}" \
  "${bundle}/checksums.txt" >/dev/null

while read -r _ filename; do
  [ -n "${filename}" ] || continue
  gh attestation verify "${bundle}/${filename}" --repo "${repository}" >/dev/null
done < "${bundle}/checksums.txt"

cosign verify \
  --certificate-identity "${identity}" \
  --certificate-oidc-issuer "${issuer}" \
  "${image}@${image_digest}" >/dev/null
gh attestation verify "oci://${image}@${image_digest}" --repo "${repository}" >/dev/null
cosign verify \
  --certificate-identity "${identity}" \
  --certificate-oidc-issuer "${issuer}" \
  "${chart}@${chart_digest}" >/dev/null
gh attestation verify "oci://${chart}@${chart_digest}" --repo "${repository}" >/dev/null

export HELM_CACHE_HOME=${work_dir}/helm/cache
export HELM_CONFIG_HOME=${work_dir}/helm/config
export HELM_DATA_HOME=${work_dir}/helm/data
mkdir -p "${work_dir}/chart"
helm pull "oci://${chart%/*}/${chart##*/}" --version "${chart_version}" --destination "${work_dir}/chart" 2>&1 \
  | tee "${work_dir}/helm-pull.txt"
grep -Fq "Digest: ${chart_digest}" "${work_dir}/helm-pull.txt"
cmp "${chart_package}" "${work_dir}/chart/kube-memlens-${chart_version}.tgz"
test "$(helm show chart "${chart_package}" | awk '$1 == "version:" {print $2; exit}')" = "${chart_version}"
test "$(helm show chart "${chart_package}" | awk '$1 == "appVersion:" {gsub(/\"/, "", $2); print $2; exit}')" = "${chart_version}"

# Candidate binaries and cluster workloads do not need a GitHub token.
unset GH_TOKEN

mkdir -p "${work_dir}/cli"
tar -xzf "${archive}" -C "${work_dir}/cli"
cli=${work_dir}/cli/kubectl-memlens
"${cli}" version --output json > "${work_dir}/cli-version.json"
jq -e --arg version "${version}" --arg commit "${commit}" \
  '.version == $version and .commit == $commit and .platform == "linux/amd64"' \
  "${work_dir}/cli-version.json" >/dev/null

docker pull "${image}@${image_digest}" >/dev/null
test "$(docker image inspect "${image}@${image_digest}" --format '{{.Config.User}}')" = '65532:65532'
test "$(docker image inspect "${image}@${image_digest}" --format '{{index .Config.Labels "org.opencontainers.image.version"}}')" = "${version}"
test "$(docker image inspect "${image}@${image_digest}" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}')" = "${commit}"
docker run --rm "${image}@${image_digest}" version --output json > "${work_dir}/image-version.json"
jq -e --arg version "${version}" --arg commit "${commit}" \
  '.version == $version and .commit == $commit' "${work_dir}/image-version.json" >/dev/null

kind create cluster --name "${cluster_name}" --image "${node_image}" --kubeconfig "${kubeconfig}" --wait 120s

helm upgrade --install kube-memlens "${chart_package}" \
  --kubeconfig "${kubeconfig}" \
  --namespace kube-memlens \
  --create-namespace \
  --set image.repository="${image}" \
  --set image.digest="${image_digest}" \
  --wait \
  --timeout 3m
helm test kube-memlens --kubeconfig "${kubeconfig}" --namespace kube-memlens --timeout 1m

cli_args=(--kubeconfig "${kubeconfig}" --context "kind-${cluster_name}" --collector-namespace kube-memlens)
doctor_ok=false
for _ in $(seq 1 24); do
  if "${cli}" "${cli_args[@]}" doctor --strict > "${work_dir}/doctor.txt" 2>&1; then
    doctor_ok=true
    break
  fi
  sleep 5
done
[ "${doctor_ok}" = true ] || {
  cat "${work_dir}/doctor.txt" >&2
  exit 1
}

helm upgrade kube-memlens "${chart_package}" \
  --kubeconfig "${kubeconfig}" --namespace kube-memlens \
  --reuse-values --set agent.interval=6s --wait --timeout 3m
helm test kube-memlens --kubeconfig "${kubeconfig}" --namespace kube-memlens --timeout 1m
helm rollback kube-memlens 1 \
  --kubeconfig "${kubeconfig}" --namespace kube-memlens --wait --timeout 3m
helm test kube-memlens --kubeconfig "${kubeconfig}" --namespace kube-memlens --timeout 1m
"${cli}" "${cli_args[@]}" doctor --strict >/dev/null

helm uninstall kube-memlens --kubeconfig "${kubeconfig}" --namespace kube-memlens --wait
for resource in \
  apiservice/v1alpha1.memory.kubememlens.io \
  clusterrole/kube-memlens-agent \
  clusterrolebinding/kube-memlens-agent \
  clusterrole/kube-memlens-namespace-viewer \
  clusterrole/kube-memlens-cluster-viewer \
  clusterrole/kube-memlens-metrics-reader \
  clusterrolebinding/kube-memlens-auth-delegator \
  clusterrole/kube-memlens-collector-node-reader \
  clusterrolebinding/kube-memlens-collector-node-reader \
  clusterrole/kube-memlens-cert-bootstrap \
  clusterrolebinding/kube-memlens-cert-bootstrap; do
  if KUBECONFIG="${kubeconfig}" kubectl get "${resource}" >/dev/null 2>&1; then
    echo "release consumer uninstall left ${resource}" >&2
    exit 1
  fi
done
if KUBECONFIG="${kubeconfig}" kubectl get rolebinding kube-memlens-extension-authentication-reader \
  --namespace kube-system >/dev/null 2>&1; then
  echo 'release consumer uninstall left kube-system rolebinding/kube-memlens-extension-authentication-reader' >&2
  exit 1
fi

echo "clean release consumer verification passed"
