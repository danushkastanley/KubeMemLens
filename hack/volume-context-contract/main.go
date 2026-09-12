// Validate optional namespace acquisition and viewer roles without installation.
package main

import (
	"fmt"
	"io"
	"os"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func main() {
	if len(os.Args) != 3 {
		fail("expected default and enabled renders")
	}
	base, enabled := read(os.Args[1]), read(os.Args[2])
	for _, name := range []string{"kube-memlens-agent", "kube-memlens-namespace-viewer", "kube-memlens-cluster-viewer", "kube-memlens-metrics-reader"} {
		key := "ClusterRole//" + name
		if base[key] == nil || !reflect.DeepEqual(base[key], enabled[key]) {
			fail("existing role changed: " + name)
		}
	}
	for _, kind := range []string{"ClusterRole", "ClusterRoleBinding"} {
		if base[kind+"//kube-memlens-volume-binding-reader"] != nil {
			fail("default volume role present")
		}
	}
	if base["ClusterRole//kube-memlens-volume-viewer"] != nil {
		fail("default viewer present")
	}
	for _, namespace := range []string{"team-a", "team-b"} {
		role := enabled["Role/"+namespace+"/kube-memlens-volume-source"]
		expectRules(role, []any{rule("", []any{"pods", "persistentvolumeclaims"})})
		binding := enabled["RoleBinding/"+namespace+"/kube-memlens-volume-source"]
		if binding == nil {
			fail("namespace acquisition binding absent")
		}
		subjects, _, _ := unstructured.NestedSlice(binding, "subjects")
		if !reflect.DeepEqual(subjects, []any{map[string]any{"kind": "ServiceAccount", "name": "kube-memlens-collector", "namespace": "kube-memlens"}}) {
			fail("unexpected acquisition subject")
		}
	}
	expectRules(enabled["ClusterRole//kube-memlens-volume-binding-reader"], []any{rule("", []any{"persistentvolumes"})})
	expectRules(enabled["ClusterRole//kube-memlens-volume-viewer"], []any{rule("memory.kubememlens.io", []any{"pods/volumes"}), rule("", []any{"pods", "persistentvolumeclaims"})})
	for _, object := range enabled {
		ref, _, _ := unstructured.NestedString(object, "roleRef", "name")
		if ref == "kube-memlens-volume-viewer" {
			fail("viewer must remain unbound")
		}
	}
	expectArg(enabled["DaemonSet/kube-memlens/kube-memlens-node-context"], "--volume-stats")
	expectArg(enabled["Deployment/kube-memlens/kube-memlens-collector"], "--volume-stats-enabled=true")
	expectArg(enabled["Deployment/kube-memlens/kube-memlens-collector"], "--volume-context-namespaces=team-a,team-b")
	fmt.Println("optional volume roles and flags match the namespace disclosure contract")
}

func rule(group string, resources []any) map[string]any {
	return map[string]any{"apiGroups": []any{group}, "resources": resources, "verbs": []any{"get"}}
}

func expectRules(object map[string]any, want []any) {
	got, _, _ := unstructured.NestedSlice(object, "rules")
	if !reflect.DeepEqual(got, want) {
		fail("unexpected volume role permissions")
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
	fail("missing optional argument: " + want)
}

func read(path string) map[string]map[string]any {
	f, err := os.Open(path)
	if err != nil {
		fail("cannot read rendered chart")
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
			fail("invalid chart document")
		}
		if len(object) == 0 {
			continue
		}
		u := unstructured.Unstructured{Object: object}
		key := u.GetKind() + "/" + u.GetNamespace() + "/" + u.GetName()
		if objects[key] != nil {
			fail("duplicate chart object")
		}
		objects[key] = object
	}
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
