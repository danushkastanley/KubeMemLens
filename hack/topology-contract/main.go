// Check opt-in topology permissions and fixed read-only mounts without installing resources.
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
		fail("expected ordinary and topology-enabled Node profile renders")
	}
	baseline, enabled := read(os.Args[1]), read(os.Args[2])
	for _, key := range []string{"DaemonSet/kube-memlens-agent", "ClusterRole/kube-memlens-node-context-producer", "ClusterRole/kube-memlens-node-context-viewer", "NetworkPolicy/kube-memlens-node-context"} {
		if baseline[key] == nil || !reflect.DeepEqual(baseline[key], enabled[key]) {
			fail("ordinary boundary changed: " + key)
		}
	}
	if baseline["ClusterRole/kube-memlens-node-topology-viewer"] != nil || enabled["ClusterRoleBinding/kube-memlens-node-topology-viewer"] != nil {
		fail("topology viewer is default or bound")
	}
	role := enabled["ClusterRole/kube-memlens-node-topology-viewer"]
	rules, _, _ := unstructured.NestedSlice(role, "rules")
	want := []any{map[string]any{"apiGroups": []any{"memory.kubememlens.io"}, "resources": []any{"nodecontexts/topology"}, "verbs": []any{"get"}}}
	if !reflect.DeepEqual(rules, want) {
		fail("topology role is broader than a named read")
	}
	pod := enabled["DaemonSet/kube-memlens-node-context"]
	spec, _, _ := unstructured.NestedMap(pod, "spec", "template", "spec")
	for _, key := range []string{"hostPID", "hostNetwork", "hostIPC", "automountServiceAccountToken"} {
		if v, ok := spec[key]; ok && v != false {
			fail("unsafe Pod field " + key)
		}
	}
	containers, _, _ := unstructured.NestedSlice(spec, "containers")
	if len(containers) != 1 {
		fail("unexpected producer containers")
	}
	c := containers[0].(map[string]any)
	security, _, _ := unstructured.NestedMap(c, "securityContext")
	if security["privileged"] != false || security["readOnlyRootFilesystem"] != true || security["allowPrivilegeEscalation"] != false {
		fail("unsafe producer security")
	}
	drop, _, _ := unstructured.NestedStringSlice(security, "capabilities", "drop")
	add, _, _ := unstructured.NestedSlice(security, "capabilities", "add")
	if !reflect.DeepEqual(drop, []string{"ALL"}) || len(add) > 0 {
		fail("added capabilities")
	}
	volumes, _, _ := unstructured.NestedSlice(spec, "volumes")
	paths := map[string]string{}
	for _, v := range volumes {
		m := v.(map[string]any)
		if h, ok := m["hostPath"].(map[string]any); ok {
			if h["type"] != "Directory" {
				fail("hostPath can create directories")
			}
			paths[m["name"].(string)] = h["path"].(string)
		}
	}
	if !reflect.DeepEqual(paths, map[string]string{"topology-system": "/sys/devices/system", "topology-mm": "/sys/kernel/mm", "topology-cgroup": "/sys/fs/cgroup"}) {
		fail("unexpected host path")
	}
	mounts, _, _ := unstructured.NestedSlice(c, "volumeMounts")
	for _, v := range mounts {
		m := v.(map[string]any)
		if _, ok := paths[m["name"].(string)]; ok && m["readOnly"] != true {
			fail("writable topology mount")
		}
	}
	fmt.Println("Topology opt-in mounts and unbound Node-only read role passed")
}
func read(path string) map[string]map[string]any {
	file, err := os.Open(path)
	if err != nil {
		fail(err.Error())
	}
	defer file.Close()
	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	result := map[string]map[string]any{}
	for {
		var object map[string]any
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(err.Error())
		}
		if len(object) == 0 {
			continue
		}
		kind, _ := object["kind"].(string)
		name, _, _ := unstructured.NestedString(object, "metadata", "name")
		result[kind+"/"+name] = object
	}
	return result
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
