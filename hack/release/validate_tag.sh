#!/usr/bin/env bash
set -Eeuo pipefail

tag=${1:?usage: validate_tag.sh TAG SHA [REMOTE]}
sha=${2:?usage: validate_tag.sh TAG SHA [REMOTE]}
remote=${3:-origin}

if [[ ! "${tag}" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.[1-9][0-9]*)?$ ]]; then
  echo "release tag is not an accepted semantic version: ${tag}" >&2
  exit 1
fi

if [ "$(git cat-file -t "refs/tags/${tag}" 2>/dev/null || true)" != tag ]; then
  echo "release tag must be annotated: ${tag}" >&2
  exit 1
fi

tag_commit=$(git rev-parse "refs/tags/${tag}^{commit}")
if [ "${tag_commit}" != "${sha}" ]; then
  echo "release tag target ${tag_commit} differs from workflow commit ${sha}" >&2
  exit 1
fi

git fetch --no-tags "${remote}" main
if ! git merge-base --is-ancestor "${sha}" "refs/remotes/${remote}/main"; then
  echo "release commit is not reachable from ${remote}/main: ${sha}" >&2
  exit 1
fi

echo "release tag is annotated, versioned and reachable from main"
