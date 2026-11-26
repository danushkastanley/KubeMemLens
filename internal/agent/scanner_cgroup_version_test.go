package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/kube"
)

// cgroupV2Root returns an empty cgroup root carrying the cgroup.controllers
// file every unified hierarchy has, so the scanner walks it.
func cgroupV2Root(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpuset cpu io memory pids\n"), 0o644); err != nil {
		t.Fatalf("write cgroup.controllers: %v", err)
	}
	return root
}

// A cgroup v1 node has no v2 memory files to read. The agent must say which
// version it found instead of failing every scan or reporting cgroup v2 with
// no containers, so doctor and status can explain the empty node.
func TestScannerReportsCgroupV1NodeWithoutWalking(t *testing.T) {
	root := t.TempDir()
	containerDir := filepath.Join(root, "memory", "kubepods", "pod"+waitingPodUID, sandboxCgroupID)
	if err := os.MkdirAll(containerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"memory.usage_in_bytes": "1024\n",
		"memory.stat":           "cache 128\nrss 512\n",
	} {
		if err := os.WriteFile(filepath.Join(containerDir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "cpu,cpuacct"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := (&Scanner{CgroupRoot: root, NodeName: "node-a"}).Scan(context.Background(), kube.EmptyPodIndex())
	if err != nil {
		t.Fatalf("Scan of a cgroup v1 tree returned error: %v", err)
	}
	environment := result.Snapshot.Environment
	if environment.CgroupVersion != "v1" {
		t.Fatalf("cgroupVersion = %q, want v1", environment.CgroupVersion)
	}
	if len(result.Snapshot.Containers) != 0 || result.ContainersFound != 0 {
		t.Fatalf("cgroup v1 scan reported containers: found=%d snapshot=%d", result.ContainersFound, len(result.Snapshot.Containers))
	}
	if result.WalkError != nil || environment.CgroupReadErrors != 0 {
		t.Fatalf("cgroup v1 scan reported a read error: walk=%v readErrors=%d", result.WalkError, environment.CgroupReadErrors)
	}
	if result.Snapshot.NodeName != "node-a" || result.Snapshot.SchemaVersion == 0 {
		t.Fatalf("cgroup v1 snapshot is not publishable: %#v", result.Snapshot)
	}
}

// A root that cannot be checked, or that is neither hierarchy (a wrong or
// empty mount), stays a scan failure: publishing an empty node would turn a
// misconfiguration into a node that merely looks idle.
func TestScannerFailsForARootThatIsNotACgroupHierarchy(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "controllers")
	if err := os.WriteFile(outside, []byte("memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(t *testing.T) string{
		"empty root": func(t *testing.T) string { return t.TempDir() },
		"missing root": func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "absent")
		},
		"unreadable version marker": func(t *testing.T) string {
			root := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(root, "cgroup.controllers")); err != nil {
				t.Fatal(err)
			}
			return root
		},
	}
	for name, prepare := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := (&Scanner{CgroupRoot: prepare(t), NodeName: "node-a"}).Scan(context.Background(), kube.EmptyPodIndex())
			if err == nil {
				t.Fatalf("Scan of %s returned no error and snapshot %#v", name, result.Snapshot)
			}
		})
	}
}
