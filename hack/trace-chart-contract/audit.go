package main

import (
	"reflect"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func verifyAuditKey(p corev1.PodSpec) {
	volumes, mounts := 0, 0
	for _, v := range p.Volumes {
		if v.Name != "audit-reference" {
			continue
		}
		volumes++
		if v.Secret == nil || v.Secret.SecretName != "audit-key" || v.Secret.DefaultMode == nil || *v.Secret.DefaultMode != 0o440 || !reflect.DeepEqual(v.Secret.Items, []corev1.KeyToPath{{Key: "reference.key", Path: "reference.key"}}) {
			fail("audit key projection widened")
		}
	}
	for _, m := range p.Containers[0].VolumeMounts {
		if m.Name != "audit-reference" {
			continue
		}
		mounts++
		if m.MountPath != "/audit" || !m.ReadOnly || m.SubPath != "" || m.SubPathExpr != "" {
			fail("audit key mount changed")
		}
	}
	if volumes != 1 || mounts != 1 {
		fail("audit key projection missing or repeated")
	}
}
func verifyAuditAPI(p corev1.PodSpec) {
	verifyAuditKey(p)
	args := p.Containers[0].Args
	if !slices.Contains(args, "--audit-reference-key=/audit/reference.key") || !slices.Contains(args, "--audit-reference-key-sha256="+strings.Repeat("e", 64)) {
		fail("API audit key is not pinned")
	}
}
func rejectNodeAuditKey(p corev1.PodSpec) {
	for _, v := range p.Volumes {
		if v.Name == "audit-reference" || v.Secret != nil && v.Secret.SecretName == "audit-key" {
			fail("node gained audit key access")
		}
	}
}
