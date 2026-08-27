#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-publish-candidate-draft.XXXXXX")
source_dir=${work}/source
fake_bin=${work}/bin
release_json=${work}/release.json
log=${work}/gh.log
candidate=v1.2.3-rc.4
ga=v1.2.3
commit=0123456789abcdef0123456789abcdef01234567

cleanup() { rm -rf "${work}"; }
trap cleanup EXIT
mkdir -p "${source_dir}" "${fake_bin}"

for name in \
  kube-memlens_1.2.3_darwin_amd64.tar.gz \
  kube-memlens_1.2.3_darwin_arm64.tar.gz \
  kube-memlens_1.2.3_linux_amd64.tar.gz \
  kube-memlens_1.2.3_linux_arm64.tar.gz \
  kube-memlens_1.2.3_windows_amd64.zip \
  kube-memlens_1.2.3_windows_arm64.zip \
  kube-memlens-1.2.3.tgz; do
  printf '%s\n' "${name}" > "${source_dir}/${name}"
done

archive_json=$(cd "${source_dir}" && sha256sum kube-memlens_* | \
  jq -Rn '[inputs | capture("^(?<digest>[0-9a-f]{64})  (?<name>.*)$") | {key: .name, value: .digest}] | from_entries')
chart_sha=$(sha256sum "${source_dir}/kube-memlens-1.2.3.tgz" | awk '{print $1}')
jq -cS -n --argjson archives "${archive_json}" --arg commit "${commit}" --arg chart_sha "${chart_sha}" '
  {schema_version:1,candidate_tag:"v1.2.3-rc.4",intended_ga_tag:"v1.2.3",source_commit:$commit,
   cli_archives:$archives,
   image:{repository:"ghcr.io/danushkastanley/candidates/1.2.3-rc.4/kube-memlens",digest:("sha256:"+("a"*64))},
   chart:{repository:"ghcr.io/danushkastanley/candidates/1.2.3-rc.4/charts/kube-memlens",digest:("sha256:"+("b"*64)),package:{name:"kube-memlens-1.2.3.tgz",sha256:$chart_sha}},
   workflow_identity:"https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/v1.2.3-rc.4"}' \
  > "${source_dir}/candidate-manifest.json"
printf 'tag=%s\ncandidate=%s\ncommit=%s\nimage=%s\nchart=%s\n' \
  "${ga}" "${candidate}" "${commit}" \
  "ghcr.io/danushkastanley/candidates/1.2.3-rc.4/kube-memlens@sha256:$(printf 'a%.0s' {1..64})" \
  "ghcr.io/danushkastanley/candidates/1.2.3-rc.4/charts/kube-memlens@sha256:$(printf 'b%.0s' {1..64})" \
  > "${source_dir}/release-subjects.txt"
(
  cd "${source_dir}"
  sha256sum kube-memlens_* kube-memlens-1.2.3.tgz candidate-manifest.json release-subjects.txt \
    > checksums.txt
)
printf 'signature\n' > "${source_dir}/checksums.txt.sigstore.json"
printf 'signature\n' > "${source_dir}/candidate-manifest.sigstore.json"
"${root}/hack/release/validate_candidate_manifest.sh" \
  "${source_dir}/candidate-manifest.json" "${candidate}" "${ga}" "${commit}" >/dev/null

write_release() {
  local draft=$1 prerelease=$2 drop=${3:-}
  local assets='[]' id=100 name size
  while IFS= read -r name; do
    [ "${name}" != "${drop}" ] || continue
    size=$(wc -c < "${source_dir}/${name}" | tr -d ' ')
    assets=$(jq -c --argjson id "${id}" --arg name "${name}" --argjson size "${size}" \
      '. + [{id:$id,name:$name,size:$size,updated_at:"2026-08-27T00:00:00Z"}]' <<< "${assets}")
    id=$((id + 1))
  done < <(find "${source_dir}" -maxdepth 1 -type f -exec basename '{}' \; | LC_ALL=C sort)
  jq -c -n --arg tag "${candidate}" --argjson draft "${draft}" \
    --argjson prerelease "${prerelease}" --argjson assets "${assets}" \
    '[{id:42,tag_name:$tag,draft:$draft,prerelease:$prerelease,
      assets_url:"https://api.github.com/repos/danushkastanley/KubeMemLens/releases/42/assets",
      assets:$assets}]' > "${release_json}"
}

cat > "${fake_bin}/gh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ " $* " == *' --paginate --slurp '* ]]; then
  jq -c -s . "${FAKE_RELEASE_JSON}"
elif [[ " $* " == *'/releases/assets/'* ]]; then
  id=${*: -1}; id=${id##*/}
  name=$(jq -r --argjson id "${id}" '.[0].assets[] | select(.id == $id) | .name' "${FAKE_RELEASE_JSON}")
  if [ "${name}" = "${FAKE_MUTATE_ASSET:-}" ]; then printf 'changed\n'; else cat "${FAKE_SOURCE}/${name}"; fi
elif [[ " $* " == *' --method PATCH '* ]]; then
  printf '%s\n' "$*" >> "${FAKE_LOG}"
  cat >/dev/null
else
  jq -c '.[0]' "${FAKE_RELEASE_JSON}"
fi
SH
cat > "${fake_bin}/cosign" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "${fake_bin}/gh" "${fake_bin}/cosign"

run_publish() {
  local output=$1
  PATH="${fake_bin}:${PATH}" FAKE_RELEASE_JSON="${release_json}" FAKE_SOURCE="${source_dir}" \
    FAKE_LOG="${log}" RELEASE_BUNDLE_DIR="${output}" RELEASE_CANDIDATE_TAG="${candidate}" \
    RELEASE_COMMIT="${commit}" \
    RELEASE_CERTIFICATE_IDENTITY="https://github.com/danushkastanley/KubeMemLens/.github/workflows/candidate.yml@refs/tags/${candidate}" \
    "${root}/hack/release/publish_candidate_draft.sh"
}

write_release true true
run_publish "${work}/success"
grep -Fq -- '--method PATCH repos/danushkastanley/KubeMemLens/releases/42 --input -' "${log}"

write_release false true
if run_publish "${work}/published" >/dev/null 2>&1; then
  echo 'candidate publisher accepted a published release' >&2; exit 1
fi

write_release true true checksums.txt.sigstore.json
if run_publish "${work}/missing" >/dev/null 2>&1; then
  echo 'candidate publisher accepted a missing asset' >&2; exit 1
fi

write_release true true
if FAKE_MUTATE_ASSET=kube-memlens-1.2.3.tgz run_publish "${work}/changed" >/dev/null 2>&1; then
  echo 'candidate publisher accepted a changed asset' >&2; exit 1
fi

echo 'candidate draft publication tests passed'
