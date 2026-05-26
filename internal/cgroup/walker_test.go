package cgroup

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const (
	testPodUID        = "12345678-1234-1234-1234-123456789abc"
	testPodUIDSystemd = "12345678_1234_1234_1234_123456789abc"
	testContainerID   = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
)

func TestExtractPodUIDFromPathSystemd(t *testing.T) {
	path := "/sys/fs/cgroup/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod" + testPodUIDSystemd + ".slice/cri-containerd-" + testContainerID + ".scope"
	if got := ExtractPodUIDFromPath(path); got != testPodUID {
		t.Fatalf("ExtractPodUIDFromPath = %q, want %q", got, testPodUID)
	}
}

func TestExtractPodUIDFromPathCgroupFS(t *testing.T) {
	path := "/sys/fs/cgroup/kubepods/burstable/pod" + testPodUID + "/" + testContainerID
	if got := ExtractPodUIDFromPath(path); got != testPodUID {
		t.Fatalf("ExtractPodUIDFromPath = %q, want %q", got, testPodUID)
	}
}

func TestExtractContainerIDFromPath(t *testing.T) {
	tests := map[string]string{
		"/sys/fs/cgroup/pod" + testPodUID + "/cri-containerd-" + testContainerID + ".scope": testContainerID,
		"/sys/fs/cgroup/pod" + testPodUID + "/docker-" + testContainerID + ".scope":         testContainerID,
		"/sys/fs/cgroup/pod" + testPodUID + "/crio-" + testContainerID + ".scope":           testContainerID,
		"/sys/fs/cgroup/pod" + testPodUID + "/" + testContainerID:                           testContainerID,
	}

	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			if got := ExtractContainerIDFromPath(path); got != want {
				t.Fatalf("ExtractContainerIDFromPath = %q, want %q", got, want)
			}
		})
	}
}

func TestWalkerFindsContainerCgroups(t *testing.T) {
	root := t.TempDir()
	podDir := filepath.Join(root, "kubepods", "burstable", "pod"+testPodUID)
	containerDir := filepath.Join(podDir, testContainerID)
	writeCgroupFiles(t, podDir, 200, 100)
	writeCgroupFiles(t, containerDir, 100, 60)

	entries, err := (Walker{Root: root}).Walk()
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1: %#v", len(entries), entries)
	}
	if entries[0].PodUID != testPodUID {
		t.Fatalf("PodUID = %q, want %q", entries[0].PodUID, testPodUID)
	}
	if entries[0].ContainerID != testContainerID {
		t.Fatalf("ContainerID = %q, want %q", entries[0].ContainerID, testContainerID)
	}
	if entries[0].Memory.TotalBytes != 100 {
		t.Fatalf("TotalBytes = %d, want 100", entries[0].Memory.TotalBytes)
	}
}

func TestWalkerSkipsParentPodCgroup(t *testing.T) {
	root := t.TempDir()
	podDir := filepath.Join(root, "kubepods.slice", "kubepods-pod"+testPodUIDSystemd+".slice")
	writeCgroupFiles(t, podDir, 500, 100)

	entries, err := (Walker{Root: root}).Walk()
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}

func writeCgroupFiles(t *testing.T, dir string, current, anon uint64) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	writeFile(t, dir, "memory.current", uintString(current)+"\n")
	writeFile(t, dir, "memory.stat", "anon "+uintString(anon)+"\nfile 10\nslab 5\n")
	writeFile(t, dir, "memory.events", "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\noom_group_kill 0\n")
}

func uintString(value uint64) string {
	return strconv.FormatUint(value, 10)
}
