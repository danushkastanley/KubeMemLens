// Validate the separate trace chart's rendered privilege and identity boundaries.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func main() {
	if len(os.Args) != 5 {
		fail("expected disabled, enabled, standard chart renders and namespace")
	}
	if len(read(os.Args[1])) != 0 {
		fail("disabled trace chart created resources")
	}
	for _, data := range read(os.Args[3]) {
		if strings.Contains(string(data), "tracing.kubememlens.io") {
			fail("standard chart contains trace resources")
		}
	}
	api, nodes, bindings, preflight, hostPreflight, registries := 0, 0, 0, 0, 0, 0
	for _, data := range read(os.Args[2]) {
		var kind struct{ Kind string }
		decode(data, &kind)
		switch kind.Kind {
		case "ConfigMap":
			var config corev1.ConfigMap
			decode(data, &config)
			if raw, ok := config.Data["check.json"]; ok {
				var spec struct {
					AuditReferenceKeySHA256 string `json:"auditReferenceKeySHA256"`
				}
				decode([]byte(raw), &spec)
				if spec.AuditReferenceKeySHA256 != strings.Repeat("e", 64) {
					fail("preflight audit key is not pinned")
				}
			}
			if raw, ok := config.Data["nodes.json"]; ok {
				verifyRegistry(raw, os.Args[4])
				registries++
			}
		case "Job":
			var job batchv1.Job
			decode(data, &job)
			if job.Spec.Template.Labels["app.kubernetes.io/component"] == "trace-host-preflight" {
				verifyHostPreflight(job)
				hostPreflight++
			} else {
				verifyPreflight(job)
				preflight++
			}
		case "Secret", "Namespace", "DaemonSet", "PersistentVolumeClaim", "CustomResourceDefinition":
			fail("chart owns unexpected state or trust material")
		case "Deployment":
			var deployment appsv1.Deployment
			decode(data, &deployment)
			verifyPod(deployment)
			if deployment.Spec.Template.Labels["app.kubernetes.io/component"] == "trace-api" {
				api++
			} else {
				nodes++
			}
		case "ClusterRole":
			var role rbacv1.ClusterRole
			decode(data, &role)
			verifyRole(role)
		case "RoleBinding", "ClusterRoleBinding":
			var binding rbacv1.ClusterRoleBinding
			decode(data, &binding)
			if len(binding.Subjects) != 1 || binding.Subjects[0].Kind != "ServiceAccount" || !strings.HasSuffix(binding.Subjects[0].Name, "-api") || binding.Subjects[0].Namespace != os.Args[4] || strings.HasSuffix(binding.RoleRef.Name, "-operator") {
				fail("unexpected trace binding or tenant grant")
			}
			bindings++
		}
	}
	if api != 1 || nodes != 2 || bindings != 2 || preflight != 1 || hostPreflight != 2 || registries != 1 {
		fail("missing or unexpected workloads/bindings")
	}
	fmt.Println("disabled default, separate identities, bounded node scope and unchanged process privileges verified")
}

func verifyPod(d appsv1.Deployment) {
	p := d.Spec.Template.Spec
	if d.Spec.Replicas == nil || *d.Spec.Replicas != 1 || d.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || len(p.Containers) != 1 || len(p.InitContainers) != 0 || p.HostNetwork || p.HostPID || p.HostIPC {
		fail("unexpected deployment lifetime or host namespace")
	}
	c := p.Containers[0]
	policyFlags := 0
	for _, argument := range c.Args {
		if argument == "--acceptance-policy-sha256="+strings.Repeat("d", 64) {
			policyFlags++
		}
	}
	if policyFlags != 1 {
		fail("workload is not bound to exact policy bytes")
	}
	s := c.SecurityContext
	if s == nil || s.AllowPrivilegeEscalation == nil || *s.AllowPrivilegeEscalation || s.ReadOnlyRootFilesystem == nil || !*s.ReadOnlyRootFilesystem || s.Privileged != nil && *s.Privileged || s.Capabilities == nil || !reflect.DeepEqual(s.Capabilities.Drop, []corev1.Capability{"ALL"}) {
		fail("unexpected container privilege")
	}
	if !strings.Contains(c.Image, "@sha256:") || len(c.Resources.Limits) != 2 || c.ReadinessProbe == nil {
		fail("image, resource or readiness bound missing")
	}
	for _, m := range c.VolumeMounts {
		if !m.ReadOnly || m.MountPropagation != nil {
			fail("writable or propagated mount")
		}
	}
	if p.AutomountServiceAccountToken == nil || p.SecurityContext == nil || p.SecurityContext.SeccompProfile == nil {
		fail("token or seccomp policy absent")
	}
	profile := p.SecurityContext.SeccompProfile
	if d.Spec.Template.Labels["app.kubernetes.io/component"] == "trace-api" {
		verifyAuditAPI(p)
		if !slices.Contains(c.Args, "--node-profile-mode=pinned") {
			fail("API does not require runtime profile checks")
		}
		if !*p.AutomountServiceAccountToken || len(s.Capabilities.Add) != 0 || s.RunAsUser == nil || *s.RunAsUser != 65532 || profile.Type != corev1.SeccompProfileTypeRuntimeDefault {
			fail("API identity or capabilities changed")
		}
		for _, v := range p.Volumes {
			if v.HostPath != nil {
				fail("API acquired a host mount")
			}
		}
		return
	}
	rejectNodeAuditKey(p)
	if !slices.Contains(c.Args, "--expected-kernel-release=6.12.0") {
		fail("node startup is not bound to the configured kernel")
	}
	if *p.AutomountServiceAccountToken || p.NodeName == "" || p.NodeSelector["kubernetes.io/os"] != "linux" || !reflect.DeepEqual(s.Capabilities.Add, []corev1.Capability{"BPF", "PERFMON"}) || profile.Type != corev1.SeccompProfileTypeLocalhost || profile.LocalhostProfile == nil || *profile.LocalhostProfile != "kube-memlens-trace/filecache-node.json" {
		fail("node scope, token, capability or seccomp boundary changed")
	}
	var paths []string
	for _, v := range p.Volumes {
		if v.HostPath != nil {
			paths = append(paths, v.HostPath.Path)
		}
	}
	if !reflect.DeepEqual(paths, []string{"/sys/kernel/btf", "/sys/kernel/tracing", "/sys/kernel/security", "/sys/fs/bpf", "/sys/fs/cgroup"}) {
		fail("unexpected node host access")
	}
}

func verifyRole(role rbacv1.ClusterRole) {
	var expected []rbacv1.PolicyRule
	switch {
	case strings.HasSuffix(role.Name, "-api"):
		expected = []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
			{APIGroups: []string{""}, Resources: []string{"nodes"}, ResourceNames: []string{"node-one", "node-two"}, Verbs: []string{"get"}},
			{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"subjectaccessreviews"}, Verbs: []string{"create"}},
		}
	case strings.HasSuffix(role.Name, "-operator"):
		expected = []rbacv1.PolicyRule{
			{APIGroups: []string{"tracing.kubememlens.io"}, Resources: []string{"traces"}, Verbs: []string{"create", "get", "delete"}},
			{APIGroups: []string{"tracing.kubememlens.io"}, Resources: []string{"traces/stream"}, Verbs: []string{"get"}},
			{APIGroups: []string{"tracing.kubememlens.io"}, Resources: []string{"tracepreflights"}, Verbs: []string{"create"}},
			{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"get"}},
		}
	default:
		fail("unexpected trace role")
	}
	if !reflect.DeepEqual(role.Rules, expected) {
		fail("trace role does not match reviewed permissions")
	}
}

func read(path string) []json.RawMessage {
	f, err := os.Open(path)
	if err != nil {
		fail("cannot read render")
	}
	defer f.Close()
	decoder := yaml.NewYAMLToJSONDecoder(f)
	var result []json.RawMessage
	for {
		var data json.RawMessage
		err := decoder.Decode(&data)
		if err == io.EOF {
			return result
		}
		if err != nil {
			fail("invalid rendered YAML")
		}
		if len(data) != 0 && string(data) != "null" {
			result = append(result, data)
		}
	}
}

func decode(data []byte, target any) {
	if json.Unmarshal(data, target) != nil {
		fail("invalid resource")
	}
}
func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
