#!/usr/bin/env bash

# Loaded by the disposable tenant-read verifier. Its caller supplies raw_get
# and work_dir so evidence never includes credentials or raw identity values.

tenant_read_measure_requests() {
  local output_dir=$1
  local user=$2
  local path=$3
  local label=$4
  local times=${output_dir}/${label}-times.txt
  : > "${times}"
  for _ in $(seq 1 12); do
    local started finished
    started=$(perl -MTime::HiRes=time -e 'printf "%.6f", time')
    raw_get "${user}" "${path}" "${output_dir}/${label}-response.json"
    finished=$(perl -MTime::HiRes=time -e 'printf "%.6f", time')
    awk -v started="${started}" -v finished="${finished}" 'BEGIN { printf "%.3f\n", (finished-started)*1000 }' >> "${times}"
  done
  sort -n -o "${times}" "${times}"
  local p50 p95 maximum
  p50=$(awk 'NR==6 { print }' "${times}")
  p95=$(awk 'NR==12 { print }' "${times}")
  maximum=$(tail -n 1 "${times}")
  jq -n --arg scope "${label}" --arg p50 "${p50}" --arg p95 "${p95}" --arg maximum "${maximum}" \
    '{scope:$scope,requests:12,p50Milliseconds:($p50|tonumber),p95Milliseconds:($p95|tonumber),maxMilliseconds:($maximum|tonumber)}'
}
