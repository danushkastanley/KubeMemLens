package main

import (
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func markerSourceRules() []any {
	return []any{
		rule("", []any{"events"}, []any{"list"}),
		rule("", []any{"replicationcontrollers"}, []any{"get"}),
		rule("apps", []any{"deployments", "replicasets", "statefulsets", "daemonsets"}, []any{"get"}),
		rule("batch", []any{"jobs", "cronjobs"}, []any{"get"}),
	}
}
func verifyMarkers(plain, full, marked, markedFull map[string]map[string]any) {
	for _, pair := range [][2]map[string]map[string]any{{plain, marked}, {full, markedFull}} {
		before, after := pair[0], pair[1]
		for key, object := range before {
			if strings.HasPrefix(key, "ClusterRole/") || strings.HasPrefix(key, "Role/") || strings.HasPrefix(key, "RoleBinding/") || strings.HasPrefix(key, "ClusterRoleBinding/") {
				if !reflect.DeepEqual(object, after[key]) {
					fail("markers widened an existing role or binding")
				}
			}
			if strings.Contains(key, "marker-") {
				fail("marker resources present when disabled")
			}
		}
		for _, ns := range []string{"team-a", "team-b"} {
			expectRules(after["Role/"+ns+"/kube-memlens-history-marker-source"], markerSourceRules())
			binding := after["RoleBinding/"+ns+"/kube-memlens-history-marker-source"]
			subjects, _, _ := unstructured.NestedSlice(binding, "subjects")
			expected := []any{map[string]any{"kind": "ServiceAccount", "name": "kube-memlens-collector", "namespace": "kube-memlens"}}
			if !reflect.DeepEqual(subjects, expected) {
				fail("unexpected marker acquisition identity")
			}
			ref, _, _ := unstructured.NestedString(binding, "roleRef", "name")
			if ref != "kube-memlens-history-marker-source" {
				fail("unexpected marker role binding")
			}
		}
		for key, object := range after {
			if strings.Contains(key, "marker-") && strings.HasPrefix(key, "ClusterRoleBinding/") {
				fail("marker source has cluster-wide binding")
			}
			ref, _, _ := unstructured.NestedString(object, "roleRef", "name")
			if strings.HasSuffix(ref, "marker-viewer") {
				fail("marker viewer automatically bound")
			}
		}
		deployment := after["Deployment/kube-memlens/kube-memlens-collector"]
		expectArg(deployment, "--memory-change-markers=true")
		a, _, _ := unstructured.NestedSlice(before["Deployment/kube-memlens/kube-memlens-collector"], "spec", "template", "spec", "containers")
		b, _, _ := unstructured.NestedSlice(deployment, "spec", "template", "spec", "containers")
		if !reflect.DeepEqual(a[0].(map[string]any)["securityContext"], b[0].(map[string]any)["securityContext"]) {
			fail("markers changed process privileges")
		}
	}
	expectRules(marked["ClusterRole//kube-memlens-history-marker-viewer"], append([]any{rule("memory.kubememlens.io", []any{"pods/trends-context"}, []any{"get"})}, markerSourceRules()...))
	expectRules(markedFull["ClusterRole//kube-memlens-history-marker-viewer"], append([]any{rule("memory.kubememlens.io", []any{"pods/trends-context", "workloads/trends-context"}, []any{"get"})}, markerSourceRules()...))
}
