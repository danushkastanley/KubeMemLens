package cgroup

import (
	"path/filepath"
	"testing"
)

func TestWalkerCountsContainerAtWalkRootOnce(t *testing.T) {
	for _, suffix := range []string{"", "/", "/."} {
		t.Run("root"+suffix, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "cri-containerd-"+testContainerID+".scope")
			writeCgroupFiles(t, root, 300, 200)
			writeCgroupFiles(t, filepath.Join(root, "init.scope"), 50, 40)
			writeCgroupFiles(t, filepath.Join(root, "system.slice", "app.service"), 200, 150)

			entries, err := (Walker{Root: root + suffix}).Walk()
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("entries = %d, want one container including its descendants", len(entries))
			}
			if entries[0].Memory.TotalBytes != 300 || entries[0].RelativePath != "." {
				t.Fatalf("root entry = %#v, want 300 bytes at relative path .", entries[0])
			}
		})
	}
}

func TestWalkerFindsDistinctContainerBelowSkippedDescendant(t *testing.T) {
	root := t.TempDir()
	container := filepath.Join(root, "cri-containerd-0123456789ab.scope")
	child := filepath.Join(container, "system.slice")
	writeCgroupFiles(t, container, 300, 200)
	writeCgroupFiles(t, child, 200, 150)
	writeCgroupFiles(t, filepath.Join(child, "cri-containerd-"+testContainerID+".scope"), 50, 40)
	entries, err := (Walker{Root: root}).Walk()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ContainerID != "0123456789ab" || entries[1].ContainerID != testContainerID {
		t.Fatalf("entries = %#v, want the two distinct container IDs", entries)
	}
}
