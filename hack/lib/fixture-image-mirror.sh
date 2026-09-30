#!/usr/bin/env bash

# The two existing public fixture locations carry the same reviewed OCI index.
# Only a classified rate limit permits this bounded alternate transport. Never
# substitute a tag, different digest, arbitrary registry or denied credential.
prefetch_rate_limited_fixture_mirror() {
  local work_dir=$1 node=$2 requested=$3 mirror report
  local digest=sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028
  case "${requested}" in
    "public.ecr.aws/docker/library/busybox@${digest}") mirror="docker.io/library/busybox@${digest}" ;;
    "docker.io/library/busybox@${digest}") mirror="public.ecr.aws/docker/library/busybox@${digest}" ;;
    *) return 1 ;;
  esac
  report=$(python3 hack/node-qualification/image_pull_failure.py \
    "${work_dir}/qualification-image-pull.private.log") || return 1
  if ! python3 -c 'import json,sys; sys.exit(json.load(sys.stdin)["failureClass"] != "rate-limited")' <<< "${report}"; then
    return 1
  fi
  if ! docker exec "${node}" timeout 10s crictl inspecti "${mirror}" \
    > "${work_dir}/fixture-mirror-inspect.private.log" 2>&1; then
    if ! docker exec "${node}" timeout 30s crictl pull "${mirror}" \
      > "${work_dir}/fixture-mirror-pull.log" 2> "${work_dir}/fixture-mirror-pull.private.log"; then
      python3 hack/node-qualification/image_pull_failure.py \
        "${work_dir}/fixture-mirror-pull.private.log" >&2
      return 1
    fi
  fi
  # No --force: a concurrent reference change must fail, never be overwritten.
  docker exec "${node}" timeout 10s ctr -n k8s.io images tag "${mirror}" "${requested}" \
    > "${work_dir}/fixture-mirror-tag.private.log" 2>&1 || return 1
  docker exec "${node}" timeout 10s crictl inspecti "${requested}" \
    > "${work_dir}/fixture-image-inspect.private.log" 2>&1 || return 1
  echo 'pinned qualification fixture available through its identical-digest public mirror' >&2
}
