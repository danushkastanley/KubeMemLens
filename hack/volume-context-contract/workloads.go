package main

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func checkWorkloads(usage, workloads map[string]map[string]any) {
	for _, name := range []string{"kube-memlens-volume-viewer", "kube-memlens-node-context-viewer", "kube-memlens-agent", "kube-memlens-namespace-viewer"} {
		key := "ClusterRole//" + name
		if !reflect.DeepEqual(usage[key], workloads[key]) {
			fail("workloads changed an existing role")
		}
	}
	if usage["ClusterRole//kube-memlens-workload-volume-viewer"] != nil {
		fail("workload viewer present without opt-in")
	}
	podList := rule("", []any{"pods"})
	podList["verbs"] = []any{"get", "list"}
	jobs := rule("batch", []any{"jobs"})
	jobs["verbs"] = []any{"get", "list"}
	expectRules(workloads["ClusterRole//kube-memlens-workload-volume-viewer"], []any{rule("memory.kubememlens.io", []any{"workloads/volumes", "pods", "pods/volumes"}), podList, rule("", []any{"persistentvolumeclaims", "replicationcontrollers"}), rule("apps", []any{"deployments", "replicasets", "statefulsets", "daemonsets"}), jobs, rule("batch", []any{"cronjobs"})})
	for _, namespace := range []string{"team-a", "team-b"} {
		listOnly := rule("", []any{"pods"})
		listOnly["verbs"] = []any{"list"}
		expectRules(workloads["Role/"+namespace+"/kube-memlens-workload-volume-source"], []any{listOnly, rule("", []any{"replicationcontrollers"}), rule("apps", []any{"deployments", "replicasets", "statefulsets", "daemonsets"}), jobs, rule("batch", []any{"cronjobs"})})
		binding := workloads["RoleBinding/"+namespace+"/kube-memlens-workload-volume-source"]
		subjects, _, _ := unstructured.NestedSlice(binding, "subjects")
		if !reflect.DeepEqual(subjects, []any{map[string]any{"kind": "ServiceAccount", "name": "kube-memlens-collector", "namespace": "kube-memlens"}}) {
			fail("workload acquisition subject changed")
		}
	}
	for _, object := range workloads {
		ref, _, _ := unstructured.NestedString(object, "roleRef", "name")
		if ref == "kube-memlens-workload-volume-viewer" {
			fail("workload viewer must remain unbound")
		}
	}
	expectArg(workloads["Deployment/kube-memlens/kube-memlens-collector"], "--volume-workloads-enabled=true")
}
