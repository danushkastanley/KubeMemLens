#!/usr/bin/env bash

set -Eeuo pipefail

required_acknowledgement=run-and-remove-kube-memlens-linux-terminal-qualification
image=kube-memlens-terminal-qualification:prod009
cli=${TERMINAL_LINUX_CLI:-}
kubeconfig=${TERMINAL_LINUX_KUBECONFIG:-}
context=${TERMINAL_LINUX_CONTEXT:-}
artifact_dir=${TERMINAL_LINUX_ARTIFACT_DIR:-}
soak_seconds=${TERMINAL_LINUX_SOAK_SECONDS:-}
work_root=${TMPDIR:-/tmp}
work_root=${work_root%/}
work_dir=

fail() { echo "Linux terminal qualification error: $*" >&2; exit 1; }
[ "${TERMINAL_LINUX_ACKNOWLEDGE:-}" = "${required_acknowledgement}" ] || fail "set TERMINAL_LINUX_ACKNOWLEDGE=${required_acknowledgement}"
for command in docker file jq kubectl python3; do command -v "${command}" >/dev/null 2>&1 || fail "required command not found: ${command}"; done
[ -x "${cli}" ] || fail "TERMINAL_LINUX_CLI must be an executable Linux file"
docker_arch=$(docker info --format '{{.Architecture}}')
case "${docker_arch}" in
  aarch64|arm64) file "${cli}" | grep -Eq 'ARM aarch64|ARM64' || fail "TERMINAL_LINUX_CLI must target Linux arm64" ;;
  x86_64|amd64) file "${cli}" | grep -Eq 'x86-64|x86_64' || fail "TERMINAL_LINUX_CLI must target Linux amd64" ;;
  *) fail "unsupported Docker architecture: ${docker_arch}" ;;
esac
[ -f "${kubeconfig}" ] || fail "TERMINAL_LINUX_KUBECONFIG must be a file"
[ -n "${context}" ] || fail "TERMINAL_LINUX_CONTEXT is required"
[ -d "${artifact_dir}" ] || fail "TERMINAL_LINUX_ARTIFACT_DIR must be an existing directory"
[ -z "$(find "${artifact_dir}" -mindepth 1 -maxdepth 1 -print -quit)" ] || fail "artifact directory must be empty"
case "${soak_seconds}" in
  ""|1800) ;;
  *) fail "TERMINAL_LINUX_SOAK_SECONDS must be empty or 1800" ;;
esac
work_dir=$(mktemp -d "${work_root}/kube-memlens-linux-terminal.XXXXXX")

cleanup() {
  status=$?
  trap - EXIT
  docker image rm "${image}" >/dev/null 2>&1 || true
  case "${work_dir}" in
    "${work_root}"/kube-memlens-linux-terminal.*) rm -rf -- "${work_dir}" ;;
  esac
  exit "${status}"
}
trap cleanup EXIT

kubectl --kubeconfig "${kubeconfig}" --context "${context}" config view --raw -o json > "${work_dir}/kubeconfig.json"
python3 - "${work_dir}/kubeconfig.json" "${work_dir}/kubeconfig.container.json" <<'PY'
import json, sys, urllib.parse
source, target = sys.argv[1:]
with open(source, encoding="utf-8") as handle:
    config = json.load(handle)
for cluster in config["clusters"]:
    parsed = urllib.parse.urlsplit(cluster["cluster"]["server"])
    cluster["cluster"]["server"] = f"https://host.docker.internal:{parsed.port}"
    cluster["cluster"]["tls-server-name"] = parsed.hostname
with open(target, "x", encoding="utf-8") as handle:
    json.dump(config, handle)
PY
chmod 600 "${work_dir}/kubeconfig.container.json"

docker build --quiet \
  --build-arg "QUALIFICATION_UID=$(id -u)" \
  -f hack/terminal-qualification/Dockerfile.linux \
  -t "${image}" . >/dev/null
docker run --rm --add-host host.docker.internal:host-gateway \
  -v "${cli}:/qualification/kubectl-memlens:ro" \
  -v "${work_dir}/kubeconfig.container.json:/qualification/kubeconfig.json:ro" \
  -v "${PWD}/hack/terminal-qualification:/scripts:ro" \
  -v "${artifact_dir}:/evidence" \
  "${image}" \
  /scripts/linux_matrix.sh /evidence \
  /qualification/kubectl-memlens \
  --kubeconfig /qualification/kubeconfig.json \
  --context "${context}" \
  --collector-namespace kube-memlens \
  tui --all-namespaces --refresh 1s

docker run --rm --user root --cap-add NET_ADMIN --add-host host.docker.internal:host-gateway \
  -v "${cli}:/qualification/kubectl-memlens:ro" \
  -v "${work_dir}/kubeconfig.container.json:/qualification/kubeconfig.json:ro" \
  -v "${PWD}/hack/terminal-qualification:/scripts:ro" \
  -v "${artifact_dir}:/evidence" \
  "${image}" \
  /scripts/remote_matrix.sh \
  /evidence /qualification/kubectl-memlens /qualification/kubeconfig.json "${context}" "$(id -u)"

expected_results=11
if [ "${soak_seconds}" = 1800 ]; then
  docker run --rm --add-host host.docker.internal:host-gateway \
    -e TERMINAL_EMULATOR_DURATION_SECONDS=1800 \
    -v "${cli}:/qualification/kubectl-memlens:ro" \
    -v "${work_dir}/kubeconfig.container.json:/qualification/kubeconfig.json:ro" \
    -v "${PWD}/hack/terminal-qualification:/scripts:ro" \
    -v "${artifact_dir}:/evidence" \
    "${image}" \
    /scripts/linux_emulator_soak.sh /evidence \
    /qualification/kubectl-memlens \
    --kubeconfig /qualification/kubeconfig.json \
    --context "${context}" \
    --collector-namespace kube-memlens \
    tui --all-namespaces --refresh 1s
  expected_results=12
fi

jq -e -s --argjson expected "${expected_results}" 'length == $expected and all(.[]; .outcome == "passed")' "${artifact_dir}"/*.json >/dev/null
echo "Linux terminal qualification passed for xterm, Kitty, Alacritty, tmux and delayed SSH"
