package main

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func main() {
	if len(os.Args) != 3 {
		fail("expected default and replica renders")
	}
	base, enabled := read(os.Args[1]), read(os.Args[2])
	sourceRules := []any{
		rule("", []any{"pods"}, []any{"get", "list"}),
		rule("", []any{"events"}, []any{"list"}),
		rule("", []any{"replicationcontrollers"}, []any{"get"}),
		rule("apps", []any{"deployments", "replicasets", "statefulsets", "daemonsets", "controllerrevisions"}, []any{"get"}),
		rule("batch", []any{"jobs"}, []any{"get", "list"}),
		rule("batch", []any{"cronjobs"}, []any{"get"}),
	}
	expectRules(enabled["ClusterRole//kube-memlens-replica-viewer"], append([]any{rule("memory.kubememlens.io", []any{"pods", "pods/replicas", "workloads/replicas"}, []any{"get"})}, sourceRules...))
	for _, ns := range []string{"team-a", "team-b"} {
		expectRules(enabled["Role/"+ns+"/kube-memlens-replica-source"], sourceRules)
		binding := enabled["RoleBinding/"+ns+"/kube-memlens-replica-source"]
		subjects, _, _ := unstructured.NestedSlice(binding, "subjects")
		if !reflect.DeepEqual(subjects, []any{map[string]any{"kind": "ServiceAccount", "name": "kube-memlens-collector", "namespace": "kube-memlens"}}) {
			fail("unexpected replica source identity")
		}
		ref, _, _ := unstructured.NestedMap(binding, "roleRef")
		if !reflect.DeepEqual(ref, map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "kube-memlens-replica-source"}) {
			fail("unexpected source role binding")
		}
	}
	newObjects := 0
	for key, object := range enabled {
		if base[key] == nil {
			newObjects++
		}
		ref, _, _ := unstructured.NestedString(object, "roleRef", "name")
		if ref == "kube-memlens-replica-viewer" {
			fail("viewer was bound automatically")
		}
		if strings.HasPrefix(key, "Role/") && strings.Contains(key, "replica") && !strings.Contains(key, "/team-a/") && !strings.Contains(key, "/team-b/") {
			fail("source role escaped selected namespaces")
		}
	}
	if newObjects != 5 {
		fail("unexpected profile resources")
	}
	for key, object := range base {
		if strings.Contains(key, "replica-") {
			fail("default profile exposes replicas")
		}
		if strings.HasPrefix(key, "ClusterRole/") || strings.HasPrefix(key, "ClusterRoleBinding/") || strings.HasPrefix(key, "Role/") || strings.HasPrefix(key, "RoleBinding/") {
			if !reflect.DeepEqual(object, enabled[key]) {
				fail("existing permissions changed")
			}
		}
	}
	key := "Deployment/kube-memlens/kube-memlens-collector"
	expectArg(enabled[key], "--replica-baselines=true")
	expectArg(enabled[key], "--replica-namespaces=team-a,team-b")
	original, _, _ := unstructured.NestedSlice(base[key], "spec", "template", "spec", "containers")
	current, _, _ := unstructured.NestedSlice(enabled[key], "spec", "template", "spec", "containers")
	a, b := original[0].(map[string]any), current[0].(map[string]any)
	args, _, _ := unstructured.NestedStringSlice(b, "args")
	for _, arg := range args {
		if strings.HasPrefix(arg, "--remote-history") {
			fail("replicas acquired a remote provider dependency")
		}
	}
	delete(a, "args")
	delete(b, "args")
	if !reflect.DeepEqual(a, b) {
		fail("replicas changed collector privileges, mounts or resources")
	}
	fmt.Println("replica chart scopes, opt-in permissions and unchanged process privileges verified")
}

func rule(group string, resources, verbs []any) map[string]any {
	return map[string]any{"apiGroups": []any{group}, "resources": resources, "verbs": verbs}
}
func expectRules(object map[string]any, want []any) {
	got, _, _ := unstructured.NestedSlice(object, "rules")
	if !reflect.DeepEqual(got, want) {
		fail("unexpected replica permissions")
	}
}
func expectArg(object map[string]any, want string) {
	containers, _, _ := unstructured.NestedSlice(object, "spec", "template", "spec", "containers")
	for _, raw := range containers {
		args, _, _ := unstructured.NestedStringSlice(raw.(map[string]any), "args")
		for _, arg := range args {
			if arg == want {
				return
			}
		}
	}
	fail("missing replica argument")
}
func read(path string) map[string]map[string]any {
	f, err := os.Open(path)
	if err != nil {
		fail("cannot read render")
	}
	defer f.Close()
	d := yaml.NewYAMLOrJSONDecoder(f, 4096)
	objects := map[string]map[string]any{}
	for {
		var object map[string]any
		err := d.Decode(&object)
		if err == io.EOF {
			return objects
		}
		if err != nil {
			fail("invalid rendered YAML")
		}
		if len(object) == 0 {
			continue
		}
		u := unstructured.Unstructured{Object: object}
		key := u.GetKind() + "/" + u.GetNamespace() + "/" + u.GetName()
		if objects[key] != nil {
			fail("duplicate rendered object")
		}
		objects[key] = object
	}
}
func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
