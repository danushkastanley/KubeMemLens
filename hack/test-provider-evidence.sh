#!/usr/bin/env bash

set -Eeuo pipefail

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-provider-evidence.XXXXXX")

cleanup() {
  local status=$?
  trap - EXIT
  if [[ "${work_dir}" == "${TMPDIR:-/tmp}/kube-memlens-provider-evidence."* ]]; then
    rm -rf -- "${work_dir}"
  fi
  exit "${status}"
}
trap cleanup EXIT

artifact_dir=${work_dir}
profile=hack/provider-profiles/gke-cos-containerd-amd64.json
profile_id=gke-cos-containerd-amd64
profile_digest=$(jq -r .profileDigest "${profile}")
image_digest=sha256:$(printf '1%.0s' $(seq 1 64))
chart_digest=sha256:$(printf '2%.0s' $(seq 1 64))
values_digest=sha256:$(printf '3%.0s' $(seq 1 64))
provider_receipt_digest=sha256:$(printf '4%.0s' $(seq 1 64))
evidence_manifest_digest=sha256:$(printf '6%.0s' $(seq 1 64))
probe_image=docker.io/library/busybox@sha256:$(printf '7%.0s' $(seq 1 64))
source_commit=$(printf '5%.0s' $(seq 1 40))
chart_version=1.0.0-rc.1
windows_nodes=0

jq '{schemaVersion:1} + .environment' \
  hack/provider-profiles/fixtures/gke-cos-pass.json > "${artifact_dir}/environment.json"

# shellcheck source=hack/lib/provider-qualification-evidence.sh
source hack/lib/provider-qualification-evidence.sh

write_pending_evidence passed
python3 hack/provider-profiles/validate.py --profile "${profile}" \
  --evidence "${artifact_dir}/provider-qualification.pending.json" --pending >/dev/null

rm "${artifact_dir}/provider-qualification.pending.json"
write_pending_evidence failed networkPolicy
validation_status=0
validation=$(python3 hack/provider-profiles/validate.py --profile "${profile}" \
  --evidence "${artifact_dir}/provider-qualification.pending.json" --pending) || validation_status=$?
[ "${validation_status}" -eq 1 ]
jq -e '.result == "fail"' <<<"${validation}" >/dev/null
jq -e '.outcome == "failed" and .reasonCode == "network_policy_failed" and
  .checks.networkPolicy == "fail"' "${artifact_dir}/provider-qualification.pending.json" >/dev/null

rm "${artifact_dir}/provider-qualification.pending.json"
write_pending_evidence failed mixedOSScheduling
validation_status=0
validation=$(python3 hack/provider-profiles/validate.py --profile "${profile}" \
  --evidence "${artifact_dir}/provider-qualification.pending.json" --pending) || validation_status=$?
[ "${validation_status}" -eq 1 ]
jq -e '.result == "fail"' <<<"${validation}" >/dev/null
jq -e '.reasonCode == "mixed_os_scheduling_failed" and
  .checks.mixedOSScheduling == "fail"' "${artifact_dir}/provider-qualification.pending.json" >/dev/null

echo "provider evidence writer checks passed"
