package main

import (
	"encoding/json"
	"reflect"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
)

func declaredAPIRole(documents []json.RawMessage) string {
	var api, operator string
	for _, data := range documents {
		var resource struct{ Kind string }
		decode(data, &resource)
		if resource.Kind != "ClusterRole" {
			continue
		}
		var role rbacv1.ClusterRole
		decode(data, &role)
		switch {
		case strings.HasSuffix(role.Name, "-api") && api == "":
			api = role.Name
		case strings.HasSuffix(role.Name, "-operator") && operator == "":
			operator = role.Name
		default:
			fail("ambiguous trace role identity")
		}
	}
	if api == "" || operator != strings.TrimSuffix(api, "-api")+"-operator" {
		fail("trace roles do not share the declared installation identity")
	}
	return api
}

func verifyBinding(binding rbacv1.ClusterRoleBinding, kind, namespace, apiRole string) {
	role := rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: apiRole}
	name := apiRole
	if kind == "RoleBinding" {
		role.Kind, role.Name = "Role", "extension-apiserver-authentication-reader"
		name = strings.TrimSuffix(apiRole, "-api") + "-requestheader"
	}
	subjects := []rbacv1.Subject{{Kind: "ServiceAccount", Name: apiRole, Namespace: namespace}}
	if binding.Name != name || binding.RoleRef != role || !reflect.DeepEqual(binding.Subjects, subjects) {
		fail("unexpected trace binding or tenant grant")
	}
}
