#!/usr/bin/env bash

# Reuse the bounded, privacy-safe pull already used by node qualification.
source hack/lib/node-context-fixture.sh

prefetch_helm_hook_image() {
  local cluster=$1 chart=$2 work_dir=$3
  local hook_image nodes node owner node_dir index=0
  helm template kube-memlens "${chart}" --show-only templates/tests/test-connection.yaml \
    > "${work_dir}/helm-hook.private.yaml" || return 1
  hook_image=$(ruby hack/check-helm-test-hook.rb "${work_dir}/helm-hook.private.yaml" --image) || return 1
  nodes=$(kind get nodes --name "${cluster}") || return 1
  if [ -z "${nodes}" ]; then
    echo 'owned kind cluster has no nodes for hook image preflight' >&2
    return 1
  fi
  while IFS= read -r node; do
    owner=$(docker inspect --format '{{index .Config.Labels "io.x-k8s.kind.cluster"}}' "${node}") || return 1
    if [ "${owner}" != "${cluster}" ]; then
      echo 'refusing hook image preflight outside the owned kind cluster' >&2
      return 1
    fi
    index=$((index + 1))
    node_dir=${work_dir}/helm-hook-image-${index}
    mkdir -p "${node_dir}" || return 1
    node_context_prefetch_image "${node_dir}" "${node}" "${hook_image}" || return 1
  done <<< "${nodes}"
}
