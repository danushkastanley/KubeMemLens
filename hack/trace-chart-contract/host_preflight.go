package main

import (
	"reflect"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func verifyHostPreflight(job batchv1.Job) {
	if job.Annotations["helm.sh/hook"] != "pre-install,pre-upgrade" || job.Annotations["helm.sh/hook-weight"] != "10" || job.Annotations["helm.sh/hook-delete-policy"] != "before-hook-creation,hook-succeeded,hook-failed" || job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 || job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 30 {
		fail("host preflight order or lifetime is not bounded")
	}
	p := job.Spec.Template.Spec
	if p.NodeName == "" || p.NodeSelector["kubernetes.io/os"] != "linux" || p.RestartPolicy != corev1.RestartPolicyNever || len(p.Containers) != 1 || len(p.InitContainers) != 0 || p.HostNetwork || p.HostPID || p.HostIPC || p.AutomountServiceAccountToken == nil || *p.AutomountServiceAccountToken || p.SecurityContext == nil || p.SecurityContext.SeccompProfile == nil || p.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeLocalhost || p.SecurityContext.SeccompProfile.LocalhostProfile == nil || *p.SecurityContext.SeccompProfile.LocalhostProfile != "kube-memlens-trace/binding-node.json" {
		fail("host preflight identity or seccomp scope changed")
	}
	c := p.Containers[0]
	s := c.SecurityContext
	if s == nil || s.AllowPrivilegeEscalation == nil || *s.AllowPrivilegeEscalation || s.ReadOnlyRootFilesystem == nil || !*s.ReadOnlyRootFilesystem || s.Privileged != nil && *s.Privileged || s.Capabilities == nil || !reflect.DeepEqual(s.Capabilities.Drop, []corev1.Capability{"ALL"}) || !reflect.DeepEqual(s.Capabilities.Add, []corev1.Capability{"BPF", "PERFMON"}) || !reflect.DeepEqual(c.Args, []string{"doctor", "--json", "--timeout=10s"}) || len(c.Resources.Limits) != 2 {
		fail("host preflight widened privileges or changed the non-attaching probe")
	}
	var paths []string
	for _, volume := range p.Volumes {
		if volume.HostPath == nil || volume.HostPath.Type == nil || *volume.HostPath.Type != corev1.HostPathDirectory {
			fail("host preflight acquired credentials or non-directory host access")
		}
		paths = append(paths, volume.HostPath.Path)
	}
	if !reflect.DeepEqual(paths, []string{"/sys/kernel/btf", "/sys/kernel/tracing", "/sys/kernel/security", "/sys/fs/bpf", "/sys/fs/cgroup"}) || len(c.VolumeMounts) != len(paths) {
		fail("unexpected host preflight mounts")
	}
	for _, mount := range c.VolumeMounts {
		if !mount.ReadOnly || mount.MountPropagation != nil {
			fail("host preflight has writable or propagated host access")
		}
	}
}
