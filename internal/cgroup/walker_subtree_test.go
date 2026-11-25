package cgroup

import (
	"path/filepath"
	"strings"
	"testing"
)

const strayContainerID = "feedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedfacefeedface"

func walkedContainerIDs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := (Walker{Root: root}).Walk()
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}
	ids := make([]string, len(entries))
	for index, entry := range entries {
		ids[index] = entry.ContainerID
	}
	return ids
}

// A container runtime or systemd unit outside Kubernetes can have a
// hex-named cgroup with memory files. It is not a Pod's container and must not
// be reported as one when the node has a kubelet subtree.
func TestWalkerIgnoresContainerLookingCgroupsOutsideTheKubeletSubtree(t *testing.T) {
	for name, kubepods := range map[string][]string{
		"systemd driver":      {"kubepods.slice", "kubepods-pod" + testPodUIDSystemd + ".slice", "cri-containerd-" + testContainerID + ".scope"},
		"cgroupfs driver":     {"kubepods", "burstable", "pod" + testPodUID, testContainerID},
		"kind node":           {"kubelet.slice", "kubelet-kubepods.slice", "kubelet-kubepods-pod" + testPodUIDSystemd + ".slice", "cri-containerd-" + testContainerID + ".scope"},
		"custom kubelet root": {"platform", "kubepods", "pod" + testPodUID, testContainerID},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeCgroupFiles(t, filepath.Join(append([]string{root}, kubepods...)...), 100, 60)
			writeCgroupFiles(t, filepath.Join(root, "system.slice", "docker-"+strayContainerID+".scope"), 700, 500)
			writeCgroupFiles(t, filepath.Join(root, "machine.slice", strayContainerID), 700, 500)

			ids := walkedContainerIDs(t, root)
			if len(ids) != 1 || ids[0] != testContainerID {
				t.Fatalf("containers = %s, want only the Pod's container %s", strings.Join(ids, ", "), testContainerID)
			}
		})
	}
}

// A node whose kubelet subtree is not where the walk looks keeps the old
// behaviour: everything is walked, so nothing that was reported is lost.
func TestWalkerWalksEverythingWithoutAKubeletSubtree(t *testing.T) {
	root := t.TempDir()
	writeCgroupFiles(t, filepath.Join(root, "system.slice", "docker-"+strayContainerID+".scope"), 700, 500)
	writeCgroupFiles(t, filepath.Join(root, "a", "b", "c", "kubepods", "pod"+testPodUID, testContainerID), 100, 60)

	ids := walkedContainerIDs(t, root)
	if len(ids) != 2 {
		t.Fatalf("containers = %s, want both", strings.Join(ids, ", "))
	}
}
