// Verify the rendered optional profile without installing resources.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func main() {
	if len(os.Args) != 4 {
		fail("expected default render, enabled render and RBAC contract")
	}
	baseline := read(os.Args[1])
	enabled := read(os.Args[2])
	for _, key := range []string{"DaemonSet/kube-memlens-agent", "ClusterRole/kube-memlens-agent", "ClusterRole/kube-memlens-namespace-viewer", "ClusterRole/kube-memlens-cluster-viewer", "ClusterRole/kube-memlens-metrics-reader"} {
		if baseline[key] == nil || !reflect.DeepEqual(baseline[key], enabled[key]) {
			fail("standard resource changed: " + key)
		}
	}
	pod := enabled["DaemonSet/kube-memlens-node-context"]
	if pod == nil {
		fail("optional producer missing")
	}
	spec, _, _ := unstructured.NestedMap(pod, "spec", "template", "spec")
	for _, key := range []string{"hostPID", "hostNetwork", "hostIPC", "automountServiceAccountToken"} {
		if value, exists := spec[key]; exists && value != false {
			fail("unsafe producer Pod field: " + key)
		}
	}
	if spec["serviceAccountName"] != "kube-memlens-node-context" {
		fail("producer ServiceAccount differs")
	}
	containers, _, _ := unstructured.NestedSlice(spec, "containers")
	if len(containers) != 1 {
		fail("expected one producer container")
	}
	container := containers[0].(map[string]any)
	security, _, _ := unstructured.NestedMap(container, "securityContext")
	if security["privileged"] != false || security["allowPrivilegeEscalation"] != false || security["readOnlyRootFilesystem"] != true {
		fail("unsafe container security context")
	}
	drop, _, _ := unstructured.NestedStringSlice(security, "capabilities", "drop")
	if !reflect.DeepEqual(drop, []string{"ALL"}) {
		fail("producer capabilities are not dropped")
	}
	volumes, _, _ := unstructured.NestedSlice(spec, "volumes")
	for _, raw := range volumes {
		if raw.(map[string]any)["hostPath"] != nil {
			fail("producer has host mount")
		}
	}
	agent := enabled["DaemonSet/kube-memlens-agent"]
	for _, key := range []string{"nodeSelector", "tolerations"} {
		standard, _, _ := unstructured.NestedFieldNoCopy(agent, "spec", "template", "spec", key)
		optional, _, _ := unstructured.NestedFieldNoCopy(pod, "spec", "template", "spec", key)
		if !reflect.DeepEqual(standard, optional) {
			fail("producer placement differs: " + key)
		}
	}
	contract, err := os.ReadFile(os.Args[3])
	if err != nil {
		fail(err.Error())
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(contract, &list); err != nil {
		fail(err.Error())
	}
	for _, role := range list.Items {
		name, _, _ := unstructured.NestedString(role, "metadata", "name")
		live := enabled["ClusterRole/"+name]
		if live == nil || !reflect.DeepEqual(role["rules"], live["rules"]) {
			fail("role differs from frozen permission contract: " + name)
		}
	}
	if enabled["ClusterRoleBinding/kube-memlens-node-context-viewer"] != nil {
		fail("viewer must not be bound automatically")
	}
	if enabled["NetworkPolicy/kube-memlens-node-context"] == nil {
		fail("optional network boundary missing")
	}
	fmt.Println("optional producer profile matches security and isolation contract")
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
