#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-skopeo-check.XXXXXX")
trap 'rm -rf "${work}"' EXIT
mkdir "${work}/bin"
cat > "${work}/bin/docker" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
test "$#" -eq 4
test "$1" = run
test "$2" = --rm
test "$3" = test-skopeo
test "$4" = --version
if [ "${FAKE_TOOL_STATE}" = missing ]; then
  echo 'docker: image manifest unknown' >&2
  exit 125
fi
printf '%s\n' "${FAKE_TOOL_STATE}"
SH
chmod +x "${work}/bin/docker"

check() {
  PATH="${work}/bin:${PATH}" SKOPEO_IMAGE=test-skopeo FAKE_TOOL_STATE="$1" \
    "${root}/hack/release/check_skopeo.sh"
}
check 'skopeo version 1.22.2' >/dev/null
check 'skopeo version 1.22.2 commit: test' >/dev/null
for state in missing 'skopeo version 1.22.20' 'skopeo version 1.22.1' ''; do
  if check "${state}" >"${work}/failure.log" 2>&1; then
    echo "Skopeo preflight accepted invalid tool state: ${state}" >&2
    exit 1
  fi
  grep -q 'pinned Skopeo image' "${work}/failure.log"
done
echo 'Skopeo availability and version tests passed'
