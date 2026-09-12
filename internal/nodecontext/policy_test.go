package nodecontext

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

func TestNodeContextPermissionMatrix(t *testing.T) {
	data, err := os.ReadFile("../../docs/security/node-context-rbac.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Items []rbacv1.ClusterRole `json:"items"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	roles := map[string][]rbacv1.PolicyRule{}
	for _, role := range manifest.Items {
		grants := 0
		for _, rule := range role.Rules {
			grants += len(rule.APIGroups) * len(rule.Resources) * len(rule.Verbs)
			if len(rule.NonResourceURLs) > 0 || slices.Contains(rule.APIGroups, "*") || slices.Contains(rule.Resources, "*") || slices.Contains(rule.Verbs, "*") {
				t.Fatalf("broad permission in %s", role.Name)
			}
		}
		expected := map[string]int{"kube-memlens-node-context-producer": 4, "kube-memlens-node-context-viewer": 4}
		if grants != expected[role.Name] {
			t.Fatalf("%s grants %d operations, expected %d", role.Name, grants, expected[role.Name])
		}
		roles[role.Name] = role.Rules
	}
	if len(roles) != 2 {
		t.Fatalf("expected only the producer and viewer contract roles, got %d", len(roles))
	}
	producer := roles["kube-memlens-node-context-producer"]
	viewer := roles["kube-memlens-node-context-viewer"]
	allows := func(rules []rbacv1.PolicyRule, group, resource, verb string) bool {
		for _, rule := range rules {
			if slices.Contains(rule.APIGroups, group) && slices.Contains(rule.Resources, resource) && slices.Contains(rule.Verbs, verb) {
				return true
			}
		}
		return false
	}
	for _, test := range []struct {
		resource string
		verb     string
		allowed  bool
	}{
		{"nodes/stats", "get", true}, {"nodes", "get", true},
		{"nodes", "list", false}, {"nodes", "watch", false},
		{"nodes/metrics", "get", false}, {"nodes/proxy", "get", false},
		{"nodes/log", "get", false}, {"nodes/spec", "get", false},
		{"nodes/configz", "get", false}, {"nodes/pods", "get", false},
		{"nodes/healthz", "get", false}, {"nodes/checkpoint", "create", false},
		{"pods", "get", false}, {"pods", "list", false},
		{"pods/proxy", "get", false}, {"pods/log", "get", false},
		{"pods/exec", "get", false}, {"pods/exec", "create", false},
		{"secrets", "get", false}, {"secrets", "list", false},
		{"serviceaccounts/token", "create", false},
	} {
		t.Run(test.verb+"_"+test.resource, func(t *testing.T) {
			if got := allows(producer, "", test.resource, test.verb); got != test.allowed {
				t.Fatalf("producer permission = %v, want %v", got, test.allowed)
			}
			if allows(viewer, "", test.resource, test.verb) {
				t.Fatal("viewer must have no core resource permission")
			}
		})
	}
	group := "memory.kubememlens.io"
	for _, test := range []struct {
		resource string
		verb     string
		producer bool
		viewer   bool
	}{
		{"ingestionepochs", "get", true, false},
		{"nodesnapshots", "create", true, false},
		{"nodesnapshots", "list", false, false},
		{"nodecontexts", "get", false, true},
		{"nodecontexts", "list", false, true},
		{"nodecontexts/history", "get", false, true},
		{"nodecontexts/analysis", "get", false, true},
		{"pods", "list", false, false},
		{"pods/history", "get", false, false},
		{"metrics", "get", false, false},
	} {
		if allows(producer, group, test.resource, test.verb) != test.producer || allows(viewer, group, test.resource, test.verb) != test.viewer {
			t.Fatalf("incorrect permission for %s %s", test.verb, test.resource)
		}
	}
}
