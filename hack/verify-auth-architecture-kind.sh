#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

kubeconfig=${AUTH_VERIFY_KUBECONFIG:-}
context=${AUTH_VERIFY_CONTEXT:-}
acknowledgement=${AUTH_VERIFY_ACKNOWLEDGE:-}
namespace=kube-memlens-auth-feasibility
agent_role=kube-memlens-auth-feasibility-agent
agent_binding=kube-memlens-auth-feasibility-agent
cluster_viewer_role=kube-memlens-auth-feasibility-cluster-viewer
cluster_viewer_binding=kube-memlens-auth-feasibility-cluster-viewer
metrics_role=kube-memlens-auth-feasibility-metrics
metrics_binding=kube-memlens-auth-feasibility-metrics
pause_image=registry.k8s.io/pause:3.10@sha256:ee6521f290b2168b6e0935a181d4cff9be1ac3f505666ef0e3c98fae8199917a

fail() {
  echo "auth architecture verification failed: $*" >&2
  exit 1
}

command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
[ -n "$kubeconfig" ] || fail "AUTH_VERIFY_KUBECONFIG is required"
[ -f "$kubeconfig" ] || fail "AUTH_VERIFY_KUBECONFIG does not exist"
[ -n "$context" ] || fail "AUTH_VERIFY_CONTEXT is required"
[ "$acknowledgement" = run-and-clean-auth-feasibility ] || fail "set AUTH_VERIFY_ACKNOWLEDGE=run-and-clean-auth-feasibility"

kctl() {
  kubectl --kubeconfig "$kubeconfig" --context "$context" "$@"
}

for resource in \
  "namespace/$namespace" \
  "clusterrole/$agent_role" \
  "clusterrolebinding/$agent_binding" \
  "clusterrole/$cluster_viewer_role" \
  "clusterrolebinding/$cluster_viewer_binding" \
  "clusterrole/$metrics_role" \
  "clusterrolebinding/$metrics_binding"
do
  ! kctl get "$resource" >/dev/null 2>&1 || fail "refusing to replace existing $resource"
done

cleanup() {
  kctl delete clusterrolebinding "$agent_binding" "$cluster_viewer_binding" "$metrics_binding" --ignore-not-found >/dev/null 2>&1 || true
  kctl delete clusterrole "$agent_role" "$cluster_viewer_role" "$metrics_role" --ignore-not-found >/dev/null 2>&1 || true
  kctl delete namespace "$namespace" --ignore-not-found --wait=true --timeout=60s >/dev/null 2>&1 || true
}
trap cleanup EXIT

sar() {
  local user=$1
  local verb=$2
  local resource=$3
  local target_namespace=${4:-}
  jq -nc \
    --arg user "$user" \
    --arg namespace_group "system:serviceaccounts:${namespace}" \
    --arg verb "$verb" \
    --arg resource "$resource" \
    --arg namespace "$target_namespace" \
    '{apiVersion:"authorization.k8s.io/v1",kind:"SubjectAccessReview",spec:{user:$user,groups:["system:serviceaccounts",$namespace_group,"system:authenticated"],resourceAttributes:{group:"memory.kubememlens.io",verb:$verb,resource:$resource}}} | if $namespace == "" then . else .spec.resourceAttributes.namespace=$namespace end' \
    | kctl create -f - -o json \
    | jq -r '.status.allowed'
}

review_agent_token() {
  local review_audience=$1
  kctl create token auth-agent -n "$namespace" --audience=kube-memlens-ingest --duration=10m --bound-object-kind=Pod --bound-object-name=auth-agent \
    | jq -Rsc --arg audience "$review_audience" '{apiVersion:"authentication.k8s.io/v1",kind:"TokenReview",spec:{token:rtrimstr("\n"),audiences:[$audience]}}' \
    | kctl create -f - -o json
}

requestheader_keys=$(kctl get configmap extension-apiserver-authentication -n kube-system -o json | jq -c '.data | keys | sort')
for requestheader_key in \
  client-ca-file \
  requestheader-allowed-names \
  requestheader-client-ca-file \
  requestheader-extra-headers-prefix \
  requestheader-group-headers \
  requestheader-username-headers
do
  jq -e --arg key "$requestheader_key" 'index($key) != null' <<<"$requestheader_keys" >/dev/null || fail "aggregation config is missing $requestheader_key"
done

kctl create namespace "$namespace" >/dev/null
kctl apply -f - >/dev/null <<YAML
apiVersion: v1
kind: ServiceAccount
metadata:
  name: auth-agent
  namespace: ${namespace}
automountServiceAccountToken: false
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: auth-viewer
  namespace: ${namespace}
automountServiceAccountToken: false
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: auth-cluster-viewer
  namespace: ${namespace}
automountServiceAccountToken: false
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: auth-metrics
  namespace: ${namespace}
automountServiceAccountToken: false
---
apiVersion: v1
kind: Pod
metadata:
  name: auth-agent
  namespace: ${namespace}
spec:
  serviceAccountName: auth-agent
  automountServiceAccountToken: false
  containers:
    - name: pause
      image: ${pause_image}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kube-memlens-namespace-viewer
  namespace: ${namespace}
rules:
  - apiGroups: ["memory.kubememlens.io"]
    resources: ["pods", "pods/history", "containers", "workloads"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-memlens-namespace-viewer
  namespace: ${namespace}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: kube-memlens-namespace-viewer
subjects:
  - kind: ServiceAccount
    name: auth-viewer
    namespace: ${namespace}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${agent_role}
rules:
  - apiGroups: ["memory.kubememlens.io"]
    resources: ["nodesnapshots"]
    verbs: ["create"]
  - apiGroups: ["memory.kubememlens.io"]
    resources: ["ingestionepochs"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${agent_binding}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ${agent_role}
subjects:
  - kind: ServiceAccount
    name: auth-agent
    namespace: ${namespace}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${cluster_viewer_role}
rules:
  - apiGroups: ["memory.kubememlens.io"]
    resources: ["pods", "pods/history", "containers", "workloads", "nodes", "clusterstatus"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${cluster_viewer_binding}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ${cluster_viewer_role}
subjects:
  - kind: ServiceAccount
    name: auth-cluster-viewer
    namespace: ${namespace}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${metrics_role}
rules:
  - apiGroups: ["memory.kubememlens.io"]
    resources: ["metrics"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${metrics_binding}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ${metrics_role}
subjects:
  - kind: ServiceAccount
    name: auth-metrics
    namespace: ${namespace}
YAML

kctl wait --for=condition=Ready pod/auth-agent -n "$namespace" --timeout=60s >/dev/null
pod_uid=$(kctl get pod auth-agent -n "$namespace" -o jsonpath='{.metadata.uid}')
node_name=$(kctl get pod auth-agent -n "$namespace" -o jsonpath='{.spec.nodeName}')
node_uid=$(kctl get node "$node_name" -o jsonpath='{.metadata.uid}')
review=$(review_agent_token kube-memlens-ingest)
wrong_audience=$(review_agent_token not-kube-memlens)

review_user=$(jq -r '.status.user.username' <<<"$review")
review_pod_uid=$(jq -r '.status.user.extra["authentication.kubernetes.io/pod-uid"][0]' <<<"$review")
review_node_name=$(jq -r '.status.user.extra["authentication.kubernetes.io/node-name"][0]' <<<"$review")
review_node_uid=$(jq -r '.status.user.extra["authentication.kubernetes.io/node-uid"][0]' <<<"$review")
review_credential_id=$(jq -r '.status.user.extra["authentication.kubernetes.io/credential-id"][0]' <<<"$review")
[ "$(jq -r '.status.authenticated' <<<"$review")" = true ] || fail "Pod-bound token did not authenticate"
[ "$review_user" = "system:serviceaccount:${namespace}:auth-agent" ] || fail "unexpected agent principal"
[ "$review_pod_uid" = "$pod_uid" ] || fail "Pod UID claim does not match"
[ "$review_node_name" = "$node_name" ] || fail "node claim does not match"
[ "$review_node_uid" = "$node_uid" ] || fail "node UID claim does not match"
[ -n "$review_credential_id" ] || fail "credential ID claim is missing"
[ "$(jq -r '.status.authenticated // false' <<<"$wrong_audience")" = false ] || fail "wrong audience was accepted"

viewer="system:serviceaccount:${namespace}:auth-viewer"
cluster_viewer="system:serviceaccount:${namespace}:auth-cluster-viewer"
agent="system:serviceaccount:${namespace}:auth-agent"
metrics="system:serviceaccount:${namespace}:auth-metrics"
namespace_allow=$(sar "$viewer" list pods "$namespace")
namespace_cross_deny=$(sar "$viewer" list pods default)
namespace_nodes_deny=$(sar "$viewer" get nodes)
cluster_allow=$(sar "$cluster_viewer" list pods)
cluster_nodes_allow=$(sar "$cluster_viewer" get nodes)
agent_create_allow=$(sar "$agent" create nodesnapshots)
agent_epoch_allow=$(sar "$agent" get ingestionepochs)
agent_list_deny=$(sar "$agent" list nodesnapshots)
metrics_allow=$(sar "$metrics" get metrics)
metrics_pods_deny=$(sar "$metrics" list pods)

[ "$namespace_allow" = true ] || fail "namespace viewer could not list its Pods"
[ "$namespace_cross_deny" = false ] || fail "namespace viewer crossed namespace boundary"
[ "$namespace_nodes_deny" = false ] || fail "namespace viewer read cluster Nodes"
[ "$cluster_allow" = true ] || fail "cluster viewer could not list Pods"
[ "$cluster_nodes_allow" = true ] || fail "cluster viewer could not read Nodes"
[ "$agent_create_allow" = true ] || fail "agent could not create a snapshot"
[ "$agent_epoch_allow" = true ] || fail "agent could not read the ingestion epoch"
[ "$agent_list_deny" = false ] || fail "agent could list snapshots"
[ "$metrics_allow" = true ] || fail "metrics scraper could not read metrics"
[ "$metrics_pods_deny" = false ] || fail "metrics scraper could list Pods"

jq -n \
  --arg server_version "$(kctl version -o json | jq -r '.serverVersion.gitVersion')" \
  --arg node_claim "$review_node_name" \
  --argjson requestheader_keys "$requestheader_keys" \
  --argjson extra_keys "$(jq -c '.status.user.extra | keys | sort' <<<"$review")" \
  --arg namespace_allow "$namespace_allow" \
  --arg namespace_cross_deny "$namespace_cross_deny" \
  --arg namespace_nodes_deny "$namespace_nodes_deny" \
  --arg cluster_allow "$cluster_allow" \
  --arg cluster_nodes_allow "$cluster_nodes_allow" \
  --arg agent_create_allow "$agent_create_allow" \
  --arg agent_epoch_allow "$agent_epoch_allow" \
  --arg agent_list_deny "$agent_list_deny" \
  --arg metrics_allow "$metrics_allow" \
  --arg metrics_pods_deny "$metrics_pods_deny" \
  '{serverVersion:$server_version,aggregation:{requestHeaderKeys:$requestheader_keys},tokenReview:{authenticated:true,audienceMismatchRejected:true,nodeClaimPresent:($node_claim|length>0),extraKeys:$extra_keys},rbac:{namespaceRead:$namespace_allow,crossNamespaceRead:$namespace_cross_deny,namespaceNodeRead:$namespace_nodes_deny,clusterRead:$cluster_allow,clusterNodeRead:$cluster_nodes_allow,agentCreateSnapshot:$agent_create_allow,agentReadEpoch:$agent_epoch_allow,agentListSnapshot:$agent_list_deny,metricsRead:$metrics_allow,metricsPodRead:$metrics_pods_deny}}'
