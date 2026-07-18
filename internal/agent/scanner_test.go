package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/kube"
)

func TestScannerClassifiesUnmappedSiblingAsInfrastructure(t *testing.T) {
	root := t.TempDir()
	podUID := "12345678-1234-1234-1234-123456789abc"
	appID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sandboxID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	podDir := filepath.Join(root, "kubepods", "pod"+podUID)
	writeScannerCgroup(t, filepath.Join(podDir, appID), 1024)
	writeScannerCgroup(t, filepath.Join(podDir, sandboxID), 128)
	index := kube.EmptyPodIndex()
	index.ByContainerID[appID] = kube.PodRef{
		Namespace:     "default",
		PodName:       "api",
		PodUID:        podUID,
		ContainerName: "app",
		ContainerID:   appID,
		NodeName:      "node-a",
		Runtime:       "containerd",
	}
	index.NodeContext = api.NodeContext{Available: true, MemoryPressureStatus: "False", MemoryAllocatableKnown: true, MemoryAllocatableBytes: 8 << 30}

	result, err := (Scanner{CgroupRoot: root, NodeName: "node-a"}).Scan(context.Background(), index)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if result.ContainersFound != 2 || result.Mapped != 1 || result.Unmapped != 0 || result.InfrastructureCgroups != 1 {
		t.Fatalf("unexpected scan counts: %#v", result)
	}
	if len(result.Snapshot.Containers) != 1 || result.Snapshot.Containers[0].ContainerID != appID {
		t.Fatalf("unexpected snapshots: %#v", result.Snapshot.Containers)
	}
	if result.TotalMemory.TotalBytes != 1024 {
		t.Fatalf("TotalMemory = %d, want app-only 1024", result.TotalMemory.TotalBytes)
	}
	if result.Snapshot.Environment.CgroupVersion != "v2" || result.Snapshot.Environment.CgroupDriver != "cgroupfs" {
		t.Fatalf("unexpected cgroup environment: %#v", result.Snapshot.Environment)
	}
	if len(result.Snapshot.Environment.ContainerRuntimes) != 1 || result.Snapshot.Environment.ContainerRuntimes[0] != "containerd" {
		t.Fatalf("unexpected runtimes: %#v", result.Snapshot.Environment.ContainerRuntimes)
	}
	if !result.Snapshot.Environment.NodeContextAvailable || result.Snapshot.Environment.MemoryPressureStatus != "False" {
		t.Fatalf("unexpected node context: %#v", result.Snapshot.Environment)
	}
	if result.Snapshot.Containers[0].Context.NodeMemoryPressure != "False" {
		t.Fatalf("container node pressure = %q, want False", result.Snapshot.Containers[0].Context.NodeMemoryPressure)
	}
	if !result.Snapshot.Environment.MemoryAllocatableKnown || result.Snapshot.Environment.MemoryAllocatableBytes != 8<<30 || !result.Snapshot.Containers[0].Context.NodeMemoryAllocatableKnown {
		t.Fatalf("allocatable memory context was not propagated: %#v", result.Snapshot)
	}
}

func writeScannerCgroup(t *testing.T, dir string, total uint64) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir cgroup: %v", err)
	}
	files := map[string]string{
		"memory.current": "1024\n",
		"memory.stat":    "anon 512\nfile 128\nshmem 0\nslab 32\nkernel 64\n",
		"memory.events":  "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\n",
	}
	files["memory.current"] = formatUint(total) + "\n"
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func formatUint(value uint64) string {
	return fmt.Sprintf("%d", value)
}
