#!/usr/bin/env bash

# Checks the chart's target-namespace guard against a live cluster. The guard
# uses lookup, so it cannot be exercised by helm template. Server dry runs
# change nothing in the cluster. Run it before the chart is installed: a server
# dry run refuses objects that another release already owns.
#
# Usage: verify_namespace_guard <chart> <kubeconfig> <existing-namespace> <work-dir>
verify_namespace_guard() {
  local chart=$1 kubeconfig=$2 existing=$3 work=$4
  local missing=kube-memlens-absent-namespace output

  if helm install namespace-guard "${chart}" --kubeconfig "${kubeconfig}" --dry-run=server \
    --namespace default --set-string namespace.name="${missing}" > "${work}/namespace-guard-missing.log" 2>&1; then
    echo "chart accepted a target namespace that does not exist" >&2
    return 1
  fi
  if ! grep -Fq "namespace.name is \"${missing}\", but that namespace does not exist" "${work}/namespace-guard-missing.log"; then
    echo "missing target namespace was rejected without the expected explanation:" >&2
    cat "${work}/namespace-guard-missing.log" >&2
    return 1
  fi

  # A release namespace that differs from an existing target is a supported layout.
  output=${work}/namespace-guard-split.log
  if ! helm install namespace-guard "${chart}" --kubeconfig "${kubeconfig}" --dry-run=server \
    --namespace default --set-string namespace.name="${existing}" > "${output}" 2>&1; then
    echo "chart rejected an existing target namespace:" >&2
    cat "${output}" >&2
    return 1
  fi
  if ! grep -Fq "is installed in the ${existing} namespace." "${output}" ||
    ! grep -Fq "kubectl rollout status deployment/kube-memlens-collector -n ${existing}" "${output}"; then
    echo "install notes do not name the namespace the workloads are in" >&2
    return 1
  fi
}
