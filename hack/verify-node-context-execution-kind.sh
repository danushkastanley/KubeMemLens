#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

[ "${NODE_CONTEXT_ACKNOWLEDGE:-}" = create-and-remove-node-context-kind ] || { echo 'explicit owned-kind acknowledgement is required' >&2; exit 1; }
artifact_dir=${NODE_CONTEXT_ARTIFACT_DIR:?NODE_CONTEXT_ARTIFACT_DIR is required}
[ ! -e "${artifact_dir}/execution.json" ] || { echo 'execution evidence already exists' >&2; exit 1; }
cluster=${NODE_CONTEXT_CLUSTER:-kube-memlens-node-context-execution}
execution_mode=${NODE_CONTEXT_EXECUTION_MODE:-qualification}
profile=hack/node-qualification/profiles/kind-137-execution.json
case "${execution_mode}" in
  qualification) execution_script=local_execution.py ;;
  api-reads) execution_script=check_api_reads.py ;;
  network-policy) execution_script=local_execution.py; profile=hack/node-qualification/profiles/kind-136-network.json ;;
  *) echo 'unsupported local execution mode' >&2; exit 1 ;;
esac
case "${cluster}" in kube-memlens-node-context-*) ;; *) echo 'unexpected local fixture name' >&2; exit 1 ;; esac
[ "${KIND_EXPERIMENTAL_PROVIDER:-docker}" = docker ] || { echo 'the fixture requires local Docker' >&2; exit 1; }
endpoint=${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}')}
case "${endpoint}" in unix://*) ;; *) echo 'the fixture requires a local Docker socket' >&2; exit 1 ;; esac
if kind get clusters | grep -Fxq "${cluster}"; then echo 'refusing to adopt an existing cluster' >&2; exit 1; fi
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/node-execution.XXXXXX")
kubeconfig=${work_dir}/kubeconfig
repository=localhost/kube-memlens-node-execution
image=${repository}:$(basename "${work_dir}")
created=false
image_created=false
cleanup() {
  local result=$?
  trap - EXIT
  if [ "${created}" = true ]; then kind delete cluster --name "${cluster}" >/dev/null 2>&1 || result=1; fi
  if [ "${image_created}" = true ]; then docker image rm "${image}" >/dev/null 2>&1 || result=1; fi
  rm -rf -- "${work_dir}"
  exit "${result}"
}
trap cleanup EXIT
mkdir -p "${artifact_dir}" "${work_dir}/image"
node_image=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["nodeImage"])' "${profile}")
architecture=$(docker info --format '{{.Architecture}}')
case "${architecture}" in aarch64|arm64) architecture=arm64 ;; x86_64|amd64) architecture=amd64 ;; *) echo 'unsupported Docker architecture' >&2; exit 1 ;; esac
commit=$(git rev-parse HEAD)
python3 - "${work_dir}/source.json" <<'PY'
import json,pathlib,subprocess,sys
pathlib.Path(sys.argv[1]).write_text(json.dumps({'sourceCommit':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),
 'sourceDirty':bool(subprocess.check_output(['git','status','--porcelain']).strip())}))
PY
echo 'local execution fixture: build immutable test image'
printf 'FROM scratch\nUSER 65532:65532\n' > "${work_dir}/image/Dockerfile"
for binary in memlens-agent memlens-collector memlens-cert-bootstrap memlens-node-context kubectl-memlens; do
  CGO_ENABLED=0 GOOS=linux GOARCH="${architecture}" go build -trimpath \
    -ldflags "-X github.com/danushkastanley/kube-memlens/internal/buildinfo.Commit=${commit}" \
    -o "${work_dir}/image/${binary}" "./cmd/${binary}"
  printf 'COPY --chmod=0555 %s /%s\n' "${binary}" "${binary}" >> "${work_dir}/image/Dockerfile"
done
for tool in api-bridge chart-inventory; do
  CGO_ENABLED=0 go build -trimpath -o "${work_dir}/${tool}" "./hack/node-qualification/${tool}"
done
if docker image inspect "${image}" >/dev/null 2>&1; then echo 'refusing to adopt an existing image' >&2; exit 1; fi
image_created=true
docker build -t "${image}" "${work_dir}/image" > "${work_dir}/image-build.log" 2>&1
docker save --output "${work_dir}/image-archive.tar" "${image}"
cat > "${work_dir}/kind.yaml" <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
  - role: worker
EOF
ready_wait=90s
if [ "${execution_mode}" = network-policy ]; then
  printf '\nnetworking:\n  disableDefaultCNI: true\n' >> "${work_dir}/kind.yaml"
  ready_wait=0s
fi
echo 'local execution fixture: create two owned Nodes'
created=true
kind create cluster --name "${cluster}" --image "${node_image}" --config "${work_dir}/kind.yaml" \
  --kubeconfig "${kubeconfig}" --wait "${ready_wait}" > "${work_dir}/kind-create.log" 2>&1
if [ "${execution_mode}" = network-policy ]; then
  echo 'local execution fixture: install pinned Cilium network policy enforcement'
  python3 hack/node-qualification/cilium_kind.py --kubeconfig "${kubeconfig}" --context "kind-${cluster}" --private "${work_dir}"
fi
kind load image-archive "${work_dir}/image-archive.tar" --name "${cluster}" > "${work_dir}/image-load.log" 2>&1
python3 hack/node-qualification/prepare_execution_kind.py --cluster "${cluster}" --kubeconfig "${kubeconfig}" \
  --private "${work_dir}" --image "${image}" --repository "${repository}"
digest=$(cat "${work_dir}/image-digest")
python3 - "${work_dir}/chart.tgz" <<'PY'
import pathlib,sys
sys.path.insert(0,'hack')
from release.package_chart import package_chart
package_chart(pathlib.Path('charts/kube-memlens'),pathlib.Path(sys.argv[1]),0)
PY
python3 "hack/node-qualification/${execution_script}" --profile "${profile}" --kubeconfig "${kubeconfig}" \
  --context "kind-${cluster}" --private "${work_dir}" --output "${work_dir}/execution.json" \
  --image-repository "${repository}" --image-digest "${digest}"
kind delete cluster --name "${cluster}" > "${work_dir}/cleanup.log" 2>&1
created=false
if kind get clusters | grep -Fxq "${cluster}"; then echo 'fixture cleanup incomplete' >&2; exit 1; fi
docker image rm "${image}" >/dev/null
image_created=false
python3 - "${work_dir}" "${artifact_dir}/execution.json" <<'PY'
import pathlib,sys
sys.path.insert(0,'hack/node-qualification')
from common import load,privacy,write_new
root=pathlib.Path(sys.argv[1]); result=load(root/'execution.json');result.update(load(root/'source.json'));result['fixtureCleanup']='passed'
privacy(result);write_new(sys.argv[2],result)
PY
echo "PASS two-Node ${execution_mode} execution and UID-owned cleanup"
