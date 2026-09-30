package main

import (
	"encoding/json"
	"reflect"

	"k8s.io/apimachinery/pkg/util/validation"
)

// A chart must not hide an additional workload or grant in an unrecognised kind,
// namespace or duplicate YAML document outside the per-resource checks.
func verifyResourceInventory(documents []json.RawMessage, namespace string) {
	versions := map[string]string{
		"ConfigMap": "v1", "Service": "v1", "ServiceAccount": "v1",
		"Job": "batch/v1", "Deployment": "apps/v1",
		"ClusterRole": "rbac.authorization.k8s.io/v1",
		"RoleBinding": "rbac.authorization.k8s.io/v1", "ClusterRoleBinding": "rbac.authorization.k8s.io/v1",
		"NetworkPolicy": "networking.k8s.io/v1", "APIService": "apiregistration.k8s.io/v1",
	}
	expected := map[string]int{"ConfigMap": 2, "Service": 3, "ServiceAccount": 2, "Job": 3,
		"Deployment": 3, "ClusterRole": 2, "RoleBinding": 1, "ClusterRoleBinding": 1,
		"NetworkPolicy": 3, "APIService": 1}
	counts := make(map[string]int)
	identities := make(map[string]bool)
	for _, data := range documents {
		var resource struct {
			APIVersion string
			Kind       string
			Metadata   struct{ Name, Namespace string }
		}
		decode(data, &resource)
		version, allowed := versions[resource.Kind]
		if !allowed || resource.APIVersion != version || len(validation.IsDNS1123Subdomain(resource.Metadata.Name)) != 0 {
			fail("unexpected rendered resource identity")
		}
		wantedNamespace := namespace
		switch resource.Kind {
		case "ClusterRole", "ClusterRoleBinding", "APIService":
			wantedNamespace = ""
		case "RoleBinding":
			wantedNamespace = "kube-system"
		}
		key := resource.Kind + "/" + resource.Metadata.Namespace + "/" + resource.Metadata.Name
		if resource.Metadata.Namespace != wantedNamespace || identities[key] {
			fail("unexpected rendered resource scope or duplicate")
		}
		identities[key] = true
		counts[resource.Kind]++
	}
	if !reflect.DeepEqual(counts, expected) {
		fail("rendered resource inventory differs from contract")
	}
}
