// Check the rendered history profile's permission and credential boundaries.
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
	if len(os.Args) != 6 {
		fail("expected default, namespace, full and two marker history renders")
	}
	base, namespace, full := read(os.Args[1]), read(os.Args[2]), read(os.Args[3])
	verifyMarkers(namespace, full, read(os.Args[4]), read(os.Args[5]))
	for key, object := range base {
		if strings.Contains(key, "history-") {
			fail("default history resource present")
		}
		if strings.HasPrefix(key, "ClusterRole/") && !reflect.DeepEqual(object, full[key]) {
			fail("existing role changed")
		}
	}
	for _, objects := range []map[string]map[string]any{namespace, full} {
		for _, ns := range []string{"team-a", "team-b"} {
			expectRules(objects["Role/"+ns+"/kube-memlens-history-source"], []any{rule("", []any{"pods"}, []any{"get"})})
			binding := objects["RoleBinding/"+ns+"/kube-memlens-history-source"]
			subjects, _, _ := unstructured.NestedSlice(binding, "subjects")
			if !reflect.DeepEqual(subjects, []any{map[string]any{"kind": "ServiceAccount", "name": "kube-memlens-collector", "namespace": "kube-memlens"}}) {
				fail("unexpected history acquisition subject")
			}
		}
		for _, object := range objects {
			ref, _, _ := unstructured.NestedString(object, "roleRef", "name")
			if strings.Contains(ref, "history") && strings.HasSuffix(ref, "viewer") {
				fail("history viewer automatically bound")
			}
		}
		deployment := objects["Deployment/kube-memlens/kube-memlens-collector"]
		expectArg(deployment, "--remote-history-enabled=true")
		expectArg(deployment, "--remote-history-namespaces=team-a,team-b")
		expectArg(deployment, "--remote-history-url=https://history.example")
		expectArg(deployment, "--remote-history-ca-file=/var/run/memory-history/ca.crt")
		expectArg(deployment, "--remote-history-token-file=/var/run/memory-history/token")
		containers, _, _ := unstructured.NestedSlice(deployment, "spec", "template", "spec", "containers")
		container := containers[0].(map[string]any)
		baseContainers, _, _ := unstructured.NestedSlice(base["Deployment/kube-memlens/kube-memlens-collector"], "spec", "template", "spec", "containers")
		if !reflect.DeepEqual(container["securityContext"], baseContainers[0].(map[string]any)["securityContext"]) {
			fail("history changed process privileges")
		}
		mounts, _, _ := unstructured.NestedSlice(container, "volumeMounts")
		found := false
		for _, raw := range mounts {
			mount := raw.(map[string]any)
			if mount["name"] == "memory-history-trust" {
				found = true
				if mount["readOnly"] != true || mount["mountPath"] != "/var/run/memory-history" {
					fail("history trust mount is not private and read-only")
				}
			}
		}
		if !found {
			fail("history trust mount missing")
		}
		volumes, _, _ := unstructured.NestedSlice(deployment, "spec", "template", "spec", "volumes")
		found = false
		for _, raw := range volumes {
			v := raw.(map[string]any)
			if v["name"] == "memory-history-trust" {
				found = true
				name, _, _ := unstructured.NestedString(v, "secret", "secretName")
				if name != "history-access" {
					fail("unexpected history Secret")
				}
			}
		}
		if !found {
			fail("history trust Secret missing")
		}
	}
	for key := range namespace {
		if strings.Contains(key, "history-node-") || strings.Contains(key, "workload-history-") {
			fail("namespace profile widened acquisition")
		}
	}
	expectRules(full["ClusterRole//kube-memlens-history-node-source"], []any{rule("", []any{"nodes"}, []any{"get"})})
	expectRules(full["ClusterRole//kube-memlens-history-node-viewer"], []any{rule("memory.kubememlens.io", []any{"nodes/trends"}, []any{"get"}), rule("", []any{"nodes"}, []any{"get"})})
	expectRules(full["ClusterRole//kube-memlens-history-viewer"], []any{rule("memory.kubememlens.io", []any{"pods", "pods/trends"}, []any{"get"}), rule("", []any{"pods"}, []any{"get"})})
	for _, ns := range []string{"team-a", "team-b"} {
		expectRules(full["Role/"+ns+"/kube-memlens-workload-history-source"], []any{
			rule("", []any{"pods"}, []any{"get", "list"}), rule("", []any{"replicationcontrollers"}, []any{"get"}),
			rule("apps", []any{"deployments", "replicasets", "statefulsets", "daemonsets"}, []any{"get"}),
			rule("batch", []any{"jobs"}, []any{"get", "list"}), rule("batch", []any{"cronjobs"}, []any{"get"}),
		})
	}
	fmt.Println("optional history roles, scopes and credentials match the read contract")
}

func rule(group string, resources, verbs []any) map[string]any {
	return map[string]any{"apiGroups": []any{group}, "resources": resources, "verbs": verbs}
}
func expectRules(object map[string]any, want []any) {
	got, _, _ := unstructured.NestedSlice(object, "rules")
	if !reflect.DeepEqual(got, want) {
		fail("unexpected history permissions")
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
	fail("missing history argument")
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
