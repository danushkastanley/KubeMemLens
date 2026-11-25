package cgroup

import (
	"os"
	"path/filepath"
	"strings"
)

// kubeletSubtreeDepth is how far below the cgroup root the kubelet's own
// subtree is looked for. It covers /kubepods and /kubepods.slice, a kind
// node's /kubelet.slice/kubelet-kubepods.slice, and a kubelet started with a
// one-level --cgroup-root.
const kubeletSubtreeDepth = 2

// kubeletSubtrees returns the outermost directories near root whose name
// contains "kubepods". Pods live only below them, so the walk can leave the
// rest of the tree alone: system and user slices change constantly and can
// hold container-looking cgroups that belong to no Pod. An empty result means
// the layout is not one this recognises and the caller walks everything.
func kubeletSubtrees(root string) []string {
	var found []string
	var search func(dir string, depth int)
	search = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// A directory that vanished or cannot be listed holds no subtree
			// this can use. The walk reports unreadable live cgroups itself.
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if strings.Contains(entry.Name(), "kubepods") {
				found = append(found, path)
				continue
			}
			if depth < kubeletSubtreeDepth {
				search(path, depth+1)
			}
		}
	}
	search(root, 1)
	return found
}
