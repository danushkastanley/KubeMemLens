#!/usr/bin/env bash

set -Eeuo pipefail

# shellcheck source=hack/lib/retry.sh
source hack/lib/retry.sh

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-retry-test.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT

attempt_file=${work_dir}/attempt
output_file=${work_dir}/output.json
printf '0\n' > "${attempt_file}"

fail_twice_then_pass() {
  local attempt
  attempt=$(cat "${attempt_file}")
  attempt=$((attempt + 1))
  printf '%s\n' "${attempt}" > "${attempt_file}"
  printf '{"attempt":%s,"status":"%s"}\n' \
    "${attempt}" "$([ "${attempt}" -ge 3 ] && printf pass || printf retry)"
  [ "${attempt}" -ge 3 ]
}

retry_to_file 3 0 "${output_file}" fail_twice_then_pass
jq -e '.attempt == 3 and .status == "pass"' "${output_file}" >/dev/null

always_fail() {
  local attempt
  attempt=$(cat "${attempt_file}")
  attempt=$((attempt + 1))
  printf '%s\n' "${attempt}" > "${attempt_file}"
  printf '{"attempt":%s,"status":"failed"}\n' "${attempt}"
  return 1
}

printf '0\n' > "${attempt_file}"
if retry_to_file 2 0 "${output_file}" always_fail; then
  echo "retry helper accepted a persistently failing command" >&2
  exit 1
fi
jq -e '.attempt == 2 and .status == "failed"' "${output_file}" >/dev/null

printf '0\n' > "${attempt_file}"
captured=$(retry_capture 3 0 fail_twice_then_pass)
jq -e '.attempt == 3 and .status == "pass"' <<<"${captured}" >/dev/null

printf '0\n' > "${attempt_file}"
if retry_capture 2 0 always_fail >/dev/null; then
  echo "capture retry helper accepted a persistently failing command" >&2
  exit 1
fi
[ "$(cat "${attempt_file}")" -eq 2 ]
