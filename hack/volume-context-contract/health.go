package main

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"reflect"
)

func checkHealth(usage, health map[string]map[string]any) {
	for _, name := range []string{"kube-memlens-volume-viewer", "kube-memlens-node-context-viewer", "kube-memlens-agent"} {
		key := "ClusterRole//" + name
		if !reflect.DeepEqual(usage[key], health[key]) {
			fail("health changed an existing role: " + name)
		}
	}
	if usage["ClusterRole//kube-memlens-volume-health-source"] != nil || usage["ClusterRole//kube-memlens-volume-backend-viewer"] != nil {
		fail("usage-only profile granted health rights")
	}
	expectRules(health["ClusterRole//kube-memlens-volume-health-source"], []any{rule("storage.k8s.io", []any{"csinodes"})})
	expectRules(health["ClusterRole//kube-memlens-volume-backend-viewer"], []any{rule("", []any{"persistentvolumes"}), rule("storage.k8s.io", []any{"csinodes"})})
	for _, object := range health {
		ref, _, _ := unstructured.NestedString(object, "roleRef", "name")
		if ref == "kube-memlens-volume-backend-viewer" {
			fail("backend viewer must remain unbound")
		}
	}
	expectArg(health["Deployment/kube-memlens/kube-memlens-collector"], "--volume-health-enabled=true")
}
