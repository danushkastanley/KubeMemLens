package main

import (
	"reflect"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func verifyPreflight(job batchv1.Job, expectedImage string) {
	if job.Annotations["helm.sh/hook"] != "pre-install,pre-upgrade" || job.Annotations["helm.sh/hook-delete-policy"] != "before-hook-creation,hook-succeeded,hook-failed" || job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 || job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 60 {
		fail("preflight lifecycle is not bounded")
	}
	p := job.Spec.Template.Spec
	if p.ServiceAccountName != "trace-installer" || p.RestartPolicy != corev1.RestartPolicyNever || len(p.Containers) != 1 || len(p.InitContainers) != 0 || p.HostNetwork || p.HostPID || p.HostIPC || p.SecurityContext == nil || p.SecurityContext.RunAsNonRoot == nil || !*p.SecurityContext.RunAsNonRoot || p.SecurityContext.SeccompProfile == nil || p.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		fail("unexpected preflight identity or host access")
	}
	verifyAuditKey(p)
	verifyImage(p, expectedImage)
	c := p.Containers[0]
	s := c.SecurityContext
	if s == nil || s.Capabilities == nil || len(s.Capabilities.Add) != 0 || !reflect.DeepEqual(s.Capabilities.Drop, []corev1.Capability{"ALL"}) || s.AllowPrivilegeEscalation == nil || *s.AllowPrivilegeEscalation || s.ReadOnlyRootFilesystem == nil || !*s.ReadOnlyRootFilesystem || !reflect.DeepEqual(c.Args, []string{"_install-check"}) || len(c.Resources.Limits) != 2 {
		fail("preflight gained privilege or arbitrary execution")
	}
	for _, volume := range p.Volumes {
		if volume.HostPath != nil {
			fail("preflight mounted host state")
		}
		if strings.HasPrefix(volume.Name, "node-") {
			if volume.Secret == nil || !reflect.DeepEqual(volume.Secret.Items, []corev1.KeyToPath{{Key: "tls.crt", Path: "tls.crt"}, {Key: "control-ca.crt", Path: "control-ca.crt"}}) {
				fail("preflight reads node private keys")
			}
		}
	}
	for _, mount := range c.VolumeMounts {
		if !mount.ReadOnly || mount.MountPropagation != nil {
			fail("preflight has a writable trust mount")
		}
	}
}
