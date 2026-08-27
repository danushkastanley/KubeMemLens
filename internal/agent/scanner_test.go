package agent

import (
	"context"
	"errors"
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
	appRef := kube.PodRef{
		Namespace:     "default",
		PodName:       "api",
		PodUID:        podUID,
		ContainerName: "app",
		ContainerID:   appID,
		NodeName:      "node-a",
		Runtime:       "containerd",
		Running:       true,
	}
	index.ByContainerID[appID] = appRef
	index.ByPodUID[podUID] = []kube.PodRef{appRef}
	index.NodeContext = api.NodeContext{Available: true, MemoryPressureStatus: "False", MemoryAllocatableKnown: true, MemoryAllocatableBytes: 8 << 30}

	scanner := Scanner{CgroupRoot: root, NodeName: "node-a"}
	result, err := scanner.Scan(context.Background(), index)
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

func TestScannerClassifiesSandboxAfterCompletedInitCgroupIsRemoved(t *testing.T) {
	root := t.TempDir()
	podUID := "12345678-1234-1234-1234-123456789abc"
	appID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	initID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sandboxID := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	podDir := filepath.Join(root, "kubepods", "pod"+podUID)
	writeScannerCgroup(t, filepath.Join(podDir, appID), 1024)
	writeScannerCgroup(t, filepath.Join(podDir, sandboxID), 128)
	index := kube.EmptyPodIndex()
	appRef := kube.PodRef{
		Namespace: "kube-system", PodName: "network-agent", PodUID: podUID,
		ContainerName: "app", ContainerID: appID, NodeName: "node-a", Running: true,
	}
	completedInitRef := kube.PodRef{
		Namespace: "kube-system", PodName: "network-agent", PodUID: podUID,
		ContainerName: "install", ContainerID: initID, NodeName: "node-a",
	}
	index.ByContainerID[appID] = appRef
	index.ByContainerID[initID] = completedInitRef
	index.ByPodUID[podUID] = []kube.PodRef{appRef, completedInitRef}

	result, err := (&Scanner{CgroupRoot: root, NodeName: "node-a"}).Scan(context.Background(), index)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if result.Mapped != 1 || result.Unmapped != 0 || result.InfrastructureCgroups != 1 {
		t.Fatalf("completed init scan counts = %#v", result)
	}
}

func TestScannerDoesNotHideUnknownCgroupWhenRunningSiblingIsMissing(t *testing.T) {
	root := t.TempDir()
	podUID := "12345678-1234-1234-1234-123456789abc"
	appID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	missingID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	unknownID := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	podDir := filepath.Join(root, "kubepods", "pod"+podUID)
	writeScannerCgroup(t, filepath.Join(podDir, appID), 1024)
	writeScannerCgroup(t, filepath.Join(podDir, unknownID), 128)
	index := kube.EmptyPodIndex()
	appRef := kube.PodRef{PodUID: podUID, ContainerID: appID, Running: true}
	missingRef := kube.PodRef{PodUID: podUID, ContainerID: missingID, Running: true}
	index.ByContainerID[appID] = appRef
	index.ByContainerID[missingID] = missingRef
	index.ByPodUID[podUID] = []kube.PodRef{appRef, missingRef}

	result, err := (&Scanner{CgroupRoot: root, NodeName: "node-a"}).Scan(context.Background(), index)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if result.Mapped != 1 || result.Unmapped != 1 || result.InfrastructureCgroups != 0 {
		t.Fatalf("missing running sibling scan counts = %#v", result)
	}
}

func TestScannerClassifiesCompactUIDStaticPodSandbox(t *testing.T) {
	root := t.TempDir()
	cgroupPodUID := "c2df6c2eedc06b1195677a320a366f5e"
	mirrorPodUID := "12345678-1234-1234-1234-123456789abc"
	containerID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sandboxID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	podDir := filepath.Join(root, "kubepods", "pod"+cgroupPodUID)
	writeScannerCgroup(t, filepath.Join(podDir, containerID), 1024)
	writeScannerCgroup(t, filepath.Join(podDir, sandboxID), 128)
	index := kube.EmptyPodIndex()
	ref := kube.PodRef{Namespace: "kube-system", PodName: "static", PodUID: mirrorPodUID,
		ContainerName: "component", ContainerID: containerID, NodeName: "node-a", Running: true}
	index.ByContainerID[containerID] = ref
	index.ByPodUID[mirrorPodUID] = []kube.PodRef{ref}

	scanner := Scanner{CgroupRoot: root, NodeName: "node-a"}
	result, err := scanner.Scan(context.Background(), index)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if result.Mapped != 1 || result.Unmapped != 0 || result.InfrastructureCgroups != 1 {
		t.Fatalf("compact UID scan counts = %#v", result)
	}
}

func TestScannerRetainsSandboxClassificationDuringPodTeardown(t *testing.T) {
	root := t.TempDir()
	podUID := "12345678-1234-1234-1234-123456789abc"
	appID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sandboxID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	unknownID := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	podDir := filepath.Join(root, "kubepods", "pod"+podUID)
	appDir := filepath.Join(podDir, appID)
	writeScannerCgroup(t, appDir, 1024)
	writeScannerCgroup(t, filepath.Join(podDir, sandboxID), 128)
	index := kube.EmptyPodIndex()
	appRef := kube.PodRef{
		Namespace: "default", PodName: "api", PodUID: podUID,
		ContainerName: "app", ContainerID: appID, NodeName: "node-a", Running: true,
	}
	index.ByContainerID[appID] = appRef
	index.ByPodUID[podUID] = []kube.PodRef{appRef}

	scanner := Scanner{CgroupRoot: root, NodeName: "node-a"}
	first, err := scanner.Scan(context.Background(), index)
	if err != nil {
		t.Fatalf("first Scan returned error: %v", err)
	}
	if first.Mapped != 1 || first.Unmapped != 0 || first.InfrastructureCgroups != 1 {
		t.Fatalf("first scan counts = %#v", first)
	}

	if err := os.WriteFile(filepath.Join(podDir, sandboxID, "memory.current"), []byte("invalid\n"), 0o644); err != nil {
		t.Fatalf("corrupt sandbox cgroup: %v", err)
	}
	partial, err := scanner.Scan(context.Background(), kube.EmptyPodIndex())
	if err != nil {
		t.Fatalf("partial Scan returned error: %v", err)
	}
	if partial.WalkError == nil {
		t.Fatal("partial Scan did not retain its walk error")
	}
	if err := os.WriteFile(filepath.Join(podDir, sandboxID, "memory.current"), []byte("128\n"), 0o644); err != nil {
		t.Fatalf("restore sandbox cgroup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(podDir, appID, "memory.current"), []byte("invalid\n"), 0o644); err != nil {
		t.Fatalf("corrupt mapped cgroup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(podDir, sandboxID, "memory.current"), []byte("invalid\n"), 0o644); err != nil {
		t.Fatalf("corrupt sandbox cgroup for empty partial walk: %v", err)
	}
	if _, err := scanner.Scan(context.Background(), kube.EmptyPodIndex()); err == nil {
		t.Fatal("zero-entry partial Scan returned no error")
	}
	if err := os.WriteFile(filepath.Join(podDir, sandboxID, "memory.current"), []byte("128\n"), 0o644); err != nil {
		t.Fatalf("restore sandbox after empty partial walk: %v", err)
	}
	if err := os.RemoveAll(appDir); err != nil {
		t.Fatalf("remove mapped cgroup: %v", err)
	}
	second, err := scanner.Scan(context.Background(), kube.EmptyPodIndex())
	if err != nil {
		t.Fatalf("second Scan returned error: %v", err)
	}
	if second.Mapped != 0 || second.Unmapped != 0 || second.InfrastructureCgroups != 1 {
		t.Fatalf("teardown scan counts = %#v", second)
	}

	unknownPodDir := filepath.Join(root, "kubepods", "pod-new")
	unknownAppID := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	writeScannerCgroup(t, filepath.Join(unknownPodDir, unknownAppID), 512)
	writeScannerCgroup(t, filepath.Join(unknownPodDir, sandboxID), 128)
	writeScannerCgroup(t, filepath.Join(unknownPodDir, unknownID), 256)
	unknownIndex := kube.EmptyPodIndex()
	unknownRef := kube.PodRef{Namespace: "default", PodName: "unknown", PodUID: "new", ContainerName: "app", ContainerID: unknownAppID, NodeName: "node-a", Running: true}
	unknownIndex.ByContainerID[unknownAppID] = unknownRef
	unknownIndex.ByPodUID["new"] = []kube.PodRef{unknownRef}
	third, err := scanner.Scan(context.Background(), unknownIndex)
	if err != nil {
		t.Fatalf("third Scan returned error: %v", err)
	}
	if third.Unmapped != 2 || third.InfrastructureCgroups != 1 {
		t.Fatalf("unknown cgroup was hidden: %#v", third)
	}
}

func TestScannerHonoursCancelledContextBeforeWalking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scanner := Scanner{CgroupRoot: t.TempDir(), NodeName: "node-a"}
	_, err := scanner.Scan(ctx, kube.EmptyPodIndex())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan error = %v, want context cancellation", err)
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
