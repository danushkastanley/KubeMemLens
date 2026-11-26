// Package nodeprofile binds an installation's declared host profile to fresh
// Kubernetes metadata. It does not replace local kernel capability preflight.
package nodeprofile

import (
	"errors"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type Profile struct {
	NodeName       string `json:"nodeName"`
	NodeUID        string `json:"nodeUID"`
	Architecture   string `json:"architecture"`
	KernelVersion  string `json:"kernelVersion"`
	RuntimeVersion string `json:"runtimeVersion"`
}

var ErrInvalid = errors.New("invalid pinned node profile")
var uidPattern = regexp.MustCompile(`^[a-z0-9-]{1,128}$`)
var kernelPattern = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,128}$`)
var runtimePattern = regexp.MustCompile(`^containerd://[A-Za-z0-9._+-]{1,128}$`)

func ValidKernel(value string) bool { return kernelPattern.MatchString(value) }

func (p Profile) Validate() error {
	if len(validation.IsDNS1123Subdomain(p.NodeName)) != 0 || !uidPattern.MatchString(p.NodeUID) ||
		(p.Architecture != "amd64" && p.Architecture != "arm64") ||
		!ValidKernel(p.KernelVersion) || !runtimePattern.MatchString(p.RuntimeVersion) {
		return ErrInvalid
	}
	for _, part := range strings.Split(p.NodeName, ".") {
		if len(part) > 63 {
			return ErrInvalid
		}
	}
	return nil
}

// Matches is used only after the declared profile has passed Validate.
func (p Profile) Matches(node *corev1.Node) bool {
	if node == nil {
		return false
	}
	info := node.Status.NodeInfo
	return node.Name == p.NodeName && string(node.UID) == p.NodeUID && node.DeletionTimestamp == nil &&
		info.OperatingSystem == "linux" && info.Architecture == p.Architecture &&
		info.KernelVersion == p.KernelVersion && info.ContainerRuntimeVersion == p.RuntimeVersion
}
