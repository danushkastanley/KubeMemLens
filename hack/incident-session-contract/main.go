package main

import (
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"os"
	"reflect"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fail("expected default and incident renders")
	}
	base, enabled := read(os.Args[1]), read(os.Args[2])
	expectRules(enabled["ClusterRole//kube-memlens-incident-creator"], []any{rule("memory.kubememlens.io", []any{"incidentsessions"}, []any{"create"})})
	expectRules(enabled["ClusterRole//kube-memlens-incident-operator"], []any{
		rule("memory.kubememlens.io", []any{"incidentsessions"}, []any{"get", "delete"}),
		rule("memory.kubememlens.io", []any{"incidentsessions/entries", "incidentsessions/capture", "incidentsessions/compare", "incidentsessions/markers", "incidentsessions/trace-references", "incidentsessions/close"}, []any{"create"}),
		rule("memory.kubememlens.io", []any{"incidentsessions/export", "pods"}, []any{"get"}),
		rule("", []any{"pods"}, []any{"get"}),
	})
	expectRules(enabled["ClusterRole//kube-memlens-incident-exporter"], []any{rule("memory.kubememlens.io", []any{"incidentsessions/export-sensitive"}, []any{"get"})})
	identity := rule("", []any{"namespaces"}, []any{"get"})
	identity["resourceNames"] = []any{"team-a", "team-b"}
	expectRules(enabled["ClusterRole//kube-memlens-incident-identity"], []any{identity})
	expectBinding(enabled["ClusterRoleBinding//kube-memlens-incident-identity"], "ClusterRole", "kube-memlens-incident-identity")
	for _, namespace := range []string{"team-a", "team-b"} {
		expectRules(enabled["Role/"+namespace+"/kube-memlens-incident-source"], []any{rule("", []any{"pods"}, []any{"get"})})
		expectBinding(enabled["RoleBinding/"+namespace+"/kube-memlens-incident-source"], "Role", "kube-memlens-incident-source")
	}
	added := 0
	for key, object := range enabled {
		if base[key] == nil {
			added++
		}
		ref, _, _ := unstructured.NestedString(object, "roleRef", "name")
		if ref == "kube-memlens-incident-creator" || ref == "kube-memlens-incident-operator" || ref == "kube-memlens-incident-exporter" {
			fail("user permissions were bound automatically")
		}
	}
	if added != 9 {
		fail("unexpected incident profile resources")
	}
	for key, object := range base {
		if strings.Contains(key, "incident-") {
			fail("default profile exposes incident permissions")
		}
		if strings.HasPrefix(key, "ClusterRole/") || strings.HasPrefix(key, "ClusterRoleBinding/") || strings.HasPrefix(key, "Role/") || strings.HasPrefix(key, "RoleBinding/") {
			if !reflect.DeepEqual(object, enabled[key]) {
				fail("existing permissions changed")
			}
		}
	}
	key := "Deployment/kube-memlens/kube-memlens-collector"
	expectArg(enabled[key], "--incident-sessions=true")
	expectArg(enabled[key], "--incident-session-namespaces=team-a,team-b")
	a, _, _ := unstructured.NestedSlice(base[key], "spec", "template", "spec", "containers")
	b, _, _ := unstructured.NestedSlice(enabled[key], "spec", "template", "spec", "containers")
	original, current := a[0].(map[string]any), b[0].(map[string]any)
	args, _, _ := unstructured.NestedStringSlice(current, "args")
	for _, arg := range args {
		if strings.HasPrefix(arg, "--remote-history") {
			fail("sessions acquired an implicit remote provider")
		}
	}
	delete(original, "args")
	delete(current, "args")
	if !reflect.DeepEqual(original, current) {
		fail("incident profile changed process privileges, mounts or resources")
	}
	fmt.Println("incident opt-in, exact source scopes, separate disclosure permission and unchanged process privileges verified")
}

func expectBinding(object map[string]any, kind, name string) {
	subjects, _, _ := unstructured.NestedSlice(object, "subjects")
	if !reflect.DeepEqual(subjects, []any{map[string]any{"kind": "ServiceAccount", "name": "kube-memlens-collector", "namespace": "kube-memlens"}}) {
		fail("unexpected collector binding")
	}
	ref, _, _ := unstructured.NestedMap(object, "roleRef")
	if !reflect.DeepEqual(ref, map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": kind, "name": name}) {
		fail("unexpected role binding")
	}
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
