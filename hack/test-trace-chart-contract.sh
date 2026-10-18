#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
if [[ $# != 0 && $# != 2 ]]; then
  echo 'usage: test-trace-chart-contract.sh [packaged-chart exact-image-reference]' >&2
  exit 1
fi
trace_chart_target=${1:-charts/kube-memlens-trace}
trace_expected_image=${2:-example.invalid/trace@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
trace_use_defaults=false
if [[ $# == 2 ]]; then trace_use_defaults=true; fi
# Relative fixtures are also readable inside CI's /work Helm container.
trace_chart_dir=$(mktemp -d './.trace-chart-contract.XXXXXX')
trap 'rm -rf -- "${trace_chart_dir}"' EXIT
python3 hack/trace-chart-contract/fixtures.py "${trace_chart_dir}" "${trace_expected_image}" "${trace_use_defaults}"
go build -trimpath -o "${trace_chart_dir}/verify" ./hack/trace-chart-contract
helm lint --strict "${trace_chart_target}" -f "${trace_chart_dir}/valid.json" --namespace trace-admin
helm template trial "${trace_chart_target}" > "${trace_chart_dir}/disabled.yaml"
helm template standard charts/kube-memlens > "${trace_chart_dir}/standard.yaml"
python3 - "${trace_chart_dir}/standard.yaml" <<'PYTHON'
import sys
from pathlib import Path
if 'tracing.kubememlens.io' in Path(sys.argv[1]).read_text():
    raise SystemExit('standard chart contains trace resources')
PYTHON
helm template trial "${trace_chart_target}" --namespace trace-admin \
  -f "${trace_chart_dir}/valid.json" > "${trace_chart_dir}/enabled.yaml"
for invalid in "${trace_chart_dir}"/invalid-*.json; do
  if helm template trial "${trace_chart_target}" --namespace trace-admin \
    -f "${invalid}" > "${trace_chart_dir}/rejected.yaml" 2> "${trace_chart_dir}/error.log"; then
    echo "invalid trace profile rendered: ${invalid##*/}" >&2
    exit 1
  fi
done
for namespace in default kube-system kube-public kube-node-lease kube-memlens; do
  if helm template trial "${trace_chart_target}" --namespace "${namespace}" \
    -f "${trace_chart_dir}/valid.json" > "${trace_chart_dir}/rejected.yaml" 2> "${trace_chart_dir}/error.log"; then
    echo 'trace profile accepted a reserved namespace' >&2
    exit 1
  fi
done
"${trace_chart_dir}/verify" "${trace_chart_dir}/disabled.yaml" \
  "${trace_chart_dir}/enabled.yaml" trace-admin "${trace_expected_image}"
helm template 123 "${trace_chart_target}" --namespace 456 \
  -f "${trace_chart_dir}/numeric.json" > "${trace_chart_dir}/numeric.yaml"
"${trace_chart_dir}/verify" "${trace_chart_dir}/disabled.yaml" \
  "${trace_chart_dir}/numeric.yaml" 456 "${trace_expected_image}"

python3 hack/trace-chart-contract/test_render_images.py "${trace_chart_dir}" "${trace_expected_image}"
