#!/usr/bin/env bash

# HTTP and timing helpers for the disposable kind isolation verifier. Raw
# bodies remain in work_dir and are removed by the fixture owner's cleanup.
# shellcheck disable=SC2016,SC2154

isolation_api_server=
isolation_curl_config=

tenant_isolation_prepare_http() {
  local reader_config=$1
  isolation_api_server=$(kubectl --kubeconfig "${reader_config}" --context "${context}" \
    config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')
  local token ca_data
  token=$(kubectl --kubeconfig "${reader_config}" --context "${context}" \
    config view --raw --minify -o json | jq -r '.users[0].user.token')
  ca_data=$(kubectl --kubeconfig "${reader_config}" --context "${context}" \
    config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
  printf '%s' "${ca_data}" | base64 --decode > "${work_dir}/tenant-reader-ca.crt"
  isolation_curl_config=${work_dir}/tenant-reader.curl.conf
  {
    printf 'silent\nshow-error\nmax-time = 5\n'
    printf 'cacert = "%s"\n' "${work_dir}/tenant-reader-ca.crt"
    printf 'header = "Authorization: Bearer %s"\n' "${token}"
  } > "${isolation_curl_config}"
  chmod 0600 "${isolation_curl_config}" "${work_dir}/tenant-reader-ca.crt"
  token=
  ca_data=
}

tenant_isolation_curl() {
  local path=$1
  local output=$2
  curl --config "${isolation_curl_config}" --output "${output}" \
    --write-out '%{http_code} %{time_total}\n' "${isolation_api_server}${path}"
}

tenant_isolation_http_status() {
  awk '{for (field=1; field<NF; field++) if ($field ~ /^HTTP\/[0-9.]+$/ && $(field+1) ~ /^[0-9][0-9][0-9]$/) code=$(field+1)}
    END {if (code == "") print "000"; else print code}' "$1"
}

tenant_isolation_adversary_request() {
  local url=$1
  local output=$2
  local method=${3:-GET}
  local command='token=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token);'
  if [ "${method}" = POST ]; then
    command+=' wget -T 5 -t 1 --no-check-certificate -S -O - --post-data="{}"'
  else
    command+=' wget -T 5 -t 1 --no-check-certificate -S -O -'
  fi
  command+=' --header="Authorization: Bearer ${token}" --header="X-Remote-User: forged-tenant" "$1"'
  set +e
  kctl exec pod/kube-memlens-isolation-adversary -n "${namespace_a}" -- \
    /bin/sh -c "${command}" sh "${url}" > "${output}" 2>&1
  set -e
  tenant_isolation_http_status "${output}"
}

tenant_isolation_percentile() {
  local file=$1
  local percentile=$2
  sort -n "${file}" | awk -v percentile="${percentile}" '
    {values[NR]=$1} END {position=int((NR*percentile+99)/100); if(position<1)position=1; print values[position]}'
}

tenant_isolation_interleaved_denials() {
  local existing_path=$1
  local missing_path=$2
  local existing_name=$3
  local missing_name=$4
  local existing_times=${work_dir}/isolation-existing-times.txt
  local missing_times=${work_dir}/isolation-missing-times.txt
  local expected_hash=
  : > "${existing_times}"
  : > "${missing_times}"
  for pair in $(seq 1 30); do
    local first_label second_label
    if [ $((pair % 2)) -eq 0 ]; then
      first_label=missing
      second_label=existing
    else
      first_label=existing
      second_label=missing
    fi
    local label path result code seconds body normalised hash
    for label in "${first_label}" "${second_label}"; do
      if [ "${label}" = existing ]; then path=${existing_path}; else path=${missing_path}; fi
      body=${work_dir}/isolation-denial-${pair}-${label}.json
      result=$(tenant_isolation_curl "${path}" "${body}")
      read -r code seconds <<<"${result}"
      [ "${code}" = 403 ] || fail "${label} out-of-scope request returned ${code}, want 403"
      normalised=${body}.normalised
      sed -e "s/${existing_name}/<target>/g" -e "s/${missing_name}/<target>/g" "${body}" > "${normalised}"
      hash=$(shasum -a 256 "${normalised}" | awk '{print $1}')
      if [ -z "${expected_hash}" ]; then expected_hash=${hash}; fi
      [ "${hash}" = "${expected_hash}" ] || fail "existing and missing denial bodies differ"
      printf '%s\n' "${seconds}" >> "${work_dir}/isolation-${label}-times.txt"
    done
  done
  local existing_p50 missing_p50 existing_p95 missing_p95 maximum median_delta p95_delta
  existing_p50=$(tenant_isolation_percentile "${existing_times}" 50)
  missing_p50=$(tenant_isolation_percentile "${missing_times}" 50)
  existing_p95=$(tenant_isolation_percentile "${existing_times}" 95)
  missing_p95=$(tenant_isolation_percentile "${missing_times}" 95)
  maximum=$(cat "${existing_times}" "${missing_times}" | sort -n | tail -n 1)
  median_delta=$(awk -v a="${existing_p50}" -v b="${missing_p50}" 'BEGIN{d=a-b;if(d<0)d=-d;print d}')
  p95_delta=$(awk -v a="${existing_p95}" -v b="${missing_p95}" 'BEGIN{d=a-b;if(d<0)d=-d;print d}')
  awk -v delta="${median_delta}" 'BEGIN{exit !(delta <= 0.050)}' || fail "denial median timing differs by more than 50ms"
  awk -v delta="${p95_delta}" 'BEGIN{exit !(delta <= 0.250)}' || fail "denial p95 timing differs by more than 250ms"
  awk -v maximum="${maximum}" 'BEGIN{exit !(maximum <= 2)}' || fail "one denial exceeded the two-second budget"
  jq -n --arg hash "${expected_hash}" --arg existingP50 "${existing_p50}" --arg missingP50 "${missing_p50}" \
    --arg existingP95 "${existing_p95}" --arg missingP95 "${missing_p95}" --arg maximum "${maximum}" \
    '{normalisedBodyHash:$hash,requests:60,existingP50Seconds:($existingP50|tonumber),
      missingP50Seconds:($missingP50|tonumber),existingP95Seconds:($existingP95|tonumber),
      missingP95Seconds:($missingP95|tonumber),maximumSeconds:($maximum|tonumber)}'
}

tenant_isolation_concurrent_reads() {
  local path=$1
  local results=${work_dir}/isolation-concurrent-results.txt
  : > "${results}"
  local pids=()
  for request in $(seq 1 32); do
    (
      tenant_isolation_curl "${path}" "${work_dir}/isolation-concurrent-${request}.json"
    ) > "${work_dir}/isolation-concurrent-${request}.result" &
    pids+=("$!")
  done
  local pid
  for pid in "${pids[@]}"; do wait "${pid}" || true; done
  cat "${work_dir}"/isolation-concurrent-*.result > "${results}"
  [ "$(wc -l < "${results}" | tr -d ' ')" -eq 32 ] || fail "concurrent read result count was incomplete"
  awk '$1 != 200 && $1 != 429 && $1 != 503 {exit 1}' "${results}" || fail "concurrent read returned an unexpected status"
  local maximum ok limited
  maximum=$(awk '{print $2}' "${results}" | sort -n | tail -n 1)
  ok=$(awk '$1 == 200 {count++} END{print count+0}' "${results}")
  limited=$(awk '$1 == 429 || $1 == 503 {count++} END{print count+0}' "${results}")
  [ "${ok}" -gt 0 ] || fail "concurrent read burst had no successful request"
  awk -v maximum="${maximum}" 'BEGIN{exit !(maximum <= 10)}' || fail "concurrent read exceeded the server timeout"
  local recovery code seconds
  recovery=$(tenant_isolation_curl "${path}" "${work_dir}/isolation-recovery.json")
  read -r code seconds <<<"${recovery}"
  [ "${code}" = 200 ] || fail "normal read did not recover after concurrent abuse"
  awk -v seconds="${seconds}" 'BEGIN{exit !(seconds <= 2)}' || fail "post-abuse read exceeded two seconds"
  jq -n --argjson successful "${ok}" --argjson limited "${limited}" --arg maximum "${maximum}" --arg recovery "${seconds}" \
    '{requests:32,successful:$successful,limited:$limited,maximumSeconds:($maximum|tonumber),recoverySeconds:($recovery|tonumber)}'
}
