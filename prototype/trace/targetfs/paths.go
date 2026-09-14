// Package targetfs binds a trusted Kubernetes container lifetime to a retained
// cgroup-v2 directory. It does not enumerate processes or load BPF programmes.
package targetfs

import (
	"path"
	"strings"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

// Config is trusted node installation configuration, never a request field.
type Config struct {
	MountPoint  string
	KubeletRoot string
}

func (c Config) rootParts() ([]string, error) {
	if !path.IsAbs(c.MountPoint) || path.Clean(c.MountPoint) != c.MountPoint || len(c.MountPoint) > 4096 {
		return nil, admission.ErrUnavailable
	}
	if !path.IsAbs(c.KubeletRoot) || path.Clean(c.KubeletRoot) != c.KubeletRoot {
		return nil, admission.ErrUnavailable
	}
	if c.KubeletRoot == "/" {
		return nil, nil
	}
	parts := strings.Split(strings.TrimPrefix(c.KubeletRoot, "/"), "/")
	if len(parts) > 4 {
		return nil, admission.ErrUnavailable
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return nil, admission.ErrUnavailable
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return nil, admission.ErrUnavailable
			}
		}
	}
	return parts, nil
}

// candidates covers the two reviewed containerd layouts under the configured
// kubelet root. No prefix scan or caller-supplied path is accepted.
func candidates(config Config, w admission.Workload) ([]string, error) {
	parts, err := config.rootParts()
	if err != nil {
		return nil, err
	}
	t := w.Target
	if t.ValidateLifetime() != nil || t.CgroupID != 0 || !podUID(t.PodUID) {
		return nil, admission.ErrTargetChanged
	}
	parts = append(parts, "kubepods")
	switch w.QoS {
	case "Guaranteed":
	case "Burstable":
		parts = append(parts, "burstable")
	case "BestEffort":
		parts = append(parts, "besteffort")
	default:
		return nil, admission.ErrUnavailable
	}
	parts = append(parts, "pod"+t.PodUID)
	filesystem := strings.Join(parts, "/") + "/" + t.ContainerID
	// Kubernetes maps hyphens within one cgroup-name component to underscores,
	// then expands each cumulative component into a systemd slice directory.
	escaped := make([]string, len(parts))
	slices := make([]string, len(parts))
	for i, part := range parts {
		escaped[i] = strings.ReplaceAll(part, "-", "_")
		slices[i] = strings.Join(escaped[:i+1], "-") + ".slice"
	}
	systemd := strings.Join(slices, "/") + "/cri-containerd-" + t.ContainerID + ".scope"
	return []string{systemd, filesystem}, nil
}

func podUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}
