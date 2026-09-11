#!/usr/bin/env bash

# Sourced only by the owned local fixture. No provider context is accepted.
# shellcheck disable=SC2154
node_observer_check() {
  if [ "$1" = baseline ]; then
    CGO_ENABLED=0 go build -trimpath -o "${work_dir}/api-bridge" ./hack/node-qualification/api-bridge
  fi
  python3 hack/node-qualification/check_kubernetes_observer.py \
    --kubeconfig "${kubeconfig}" --context "kind-${cluster}" --node "${node}" \
    --bridge "${work_dir}/api-bridge" --profile "${NODE_CONTEXT_OBSERVER_PROFILE}" \
    --phase "$1" --audience "${audience}" --output "${artifact_dir}/observer-$1.json"
}
