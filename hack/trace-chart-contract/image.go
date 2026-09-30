package main

import (
	"regexp"

	corev1 "k8s.io/api/core/v1"
)

var imageReference = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]*@sha256:[0-9a-f]{64}$`)

func verifyImage(p corev1.PodSpec, expected string) {
	if !imageReference.MatchString(expected) || len(p.Containers) != 1 || len(p.InitContainers) != 0 || len(p.EphemeralContainers) != 0 || p.Containers[0].Image != expected {
		fail("workload image does not match the exact release image")
	}
}
