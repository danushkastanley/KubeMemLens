package main

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/cgroup"
)

// cgroupVersionNotice returns one log line when the node's cgroup version
// differs from the one last logged, and "" otherwise, so a cgroup v1 node is
// reported once rather than on every scan. Start previous at
// cgroup.VersionV2: a supported node logs nothing new.
func cgroupVersionNotice(node, previous, current string) string {
	if current == previous {
		return ""
	}
	return fmt.Sprintf("cgroup version node=%s version=%s supported=%t\n", node, current, current == cgroup.VersionV2)
}
