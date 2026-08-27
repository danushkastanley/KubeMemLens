#!/usr/bin/env bash
set -Eeuo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/kube-memlens-resume-draft.XXXXXX")
expected=${work}/expected
source_dir=${work}/source
fake_bin=${work}/bin
release_json=${work}/release.json

cleanup() { rm -rf "${work}"; }
trap cleanup EXIT
mkdir -p "${expected}" "${source_dir}" "${fake_bin}"
printf 'one\n' > "${expected}/one.txt"
printf 'two\n' > "${expected}/two.txt"
cp "${expected}"/* "${source_dir}/"

write_release() {
  local draft=$1 prerelease=$2 second=${3:-two.txt}
  jq -c -n --argjson draft "${draft}" --argjson prerelease "${prerelease}" --arg second "${second}" '
    [[{id:42,tag_name:"v1.2.3-rc.1",draft:$draft,prerelease:$prerelease,
      assets:[{id:1,name:"one.txt",size:4,updated_at:"2026-08-27T00:00:00Z"},
              {id:2,name:$second,size:4,updated_at:"2026-08-27T00:00:00Z"}]}]]' > "${release_json}"
}

cat > "${fake_bin}/gh" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ " $* " == *' --paginate --slurp '* ]]; then
  cat "${FAKE_RELEASE_JSON}"
else
  id=${*: -1}; id=${id##*/}
  name=$(jq -r --argjson id "${id}" '.[0][0].assets[] | select(.id == $id) | .name' "${FAKE_RELEASE_JSON}")
  cat "${FAKE_SOURCE}/${name}"
fi
SH
chmod +x "${fake_bin}/gh"

resume() {
  PATH="${fake_bin}:${PATH}" FAKE_RELEASE_JSON="${release_json}" FAKE_SOURCE="${source_dir}" \
    "${root}/hack/release/resume_draft.sh" v1.2.3-rc.1 "${expected}" true
}

write_release true true
resume >/dev/null

write_release false true
if resume >/dev/null 2>&1; then
  echo 'draft resume accepted a published release' >&2; exit 1
fi

write_release true true unexpected.txt
if resume >/dev/null 2>&1; then
  echo 'draft resume accepted a mismatched asset inventory' >&2; exit 1
fi

printf '[]\n' > "${release_json}"
set +e
resume >/dev/null 2>&1
status=$?
set -e
test "${status}" -eq 3

echo 'release draft resume tests passed'
