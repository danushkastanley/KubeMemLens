package cgroup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

const (
	exitingContainerID   = "1111111111111111111111111111111111111111111111111111111111111111"
	remainingContainerID = "2222222222222222222222222222222222222222222222222222222222222222"
)

// churnFixture has one container that the test disturbs and one that must
// always be reported. The disturbed container sorts first so it is read first.
func churnFixture(t *testing.T) (root, exiting, remaining string) {
	t.Helper()
	root = t.TempDir()
	exiting = filepath.Join(root, "kubepods", "burstable", "pod"+testPodUID, exitingContainerID)
	remaining = filepath.Join(root, "kubepods", "burstable", "pod"+testPodUID, remainingContainerID)
	for _, dir := range []string{exiting, remaining} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeCgroupFiles(t, dir, 4096, 2048)
	}
	return root, exiting, remaining
}

func assertOnlyRemainingContainer(t *testing.T, entries []Entry, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("a cgroup that no longer exists was reported as a scan error: %v", err)
	}
	if len(entries) != 1 || entries[0].ContainerID != remainingContainerID || entries[0].Memory.TotalBytes != 4096 {
		t.Fatalf("entries = %+v, want only the remaining container", entries)
	}
}

// A container can exit between being listed and being read. That is ordinary
// churn, not an incomplete read of live state.
func TestWalkerSkipsContainerThatExitsBeforeItIsRead(t *testing.T) {
	root, exiting, _ := churnFixture(t)
	walker := Walker{Root: root, parse: func(name, dir string) (model.MemoryBreakdown, error) {
		if dir == exiting {
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
		}
		return ParseDirectory(name, dir)
	}}
	entries, err := walker.Walk()
	assertOnlyRemainingContainer(t, entries, err)
}

// The kernel returns ENODEV when a file of a removed cgroup is read through a
// handle that was opened while the cgroup still existed.
func TestWalkerSkipsContainerWhoseCgroupWasRemovedWhileOpen(t *testing.T) {
	root, exiting, _ := churnFixture(t)
	walker := Walker{Root: root, parse: func(name, dir string) (model.MemoryBreakdown, error) {
		if dir == exiting {
			return model.MemoryBreakdown{}, fmt.Errorf("read memory.stat: %w", &fs.PathError{Op: "read", Path: "memory.stat", Err: syscall.ENODEV})
		}
		return ParseDirectory(name, dir)
	}}
	entries, err := walker.Walk()
	assertOnlyRemainingContainer(t, entries, err)
}

// A directory listed by its parent can be gone by the time the walk descends
// into it, for example a systemd scope that has just stopped.
func TestWalkerSkipsDirectoryRemovedDuringTheWalk(t *testing.T) {
	root, exiting, _ := churnFixture(t)
	stopping := filepath.Join(root, "run-stopping.scope")
	if err := os.MkdirAll(filepath.Join(stopping, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(exiting, "memory.stat")); err != nil {
		t.Fatal(err)
	}
	removed := false
	walker := Walker{Root: root, parse: func(name, dir string) (model.MemoryBreakdown, error) {
		// The walk listed the root's entries before descending into kubepods,
		// so it still expects to find the scope when it gets there.
		if !removed {
			removed = true
			if err := os.RemoveAll(stopping); err != nil {
				t.Fatal(err)
			}
		}
		return ParseDirectory(name, dir)
	}}
	entries, err := walker.Walk()
	if !removed {
		t.Fatal("the fixture did not remove the directory during the walk")
	}
	assertOnlyRemainingContainer(t, entries, err)
}

// A cgroup that is still present but cannot be read is a real gap in the
// evidence and must keep failing the scan.
func TestWalkerStillReportsCgroupsItCannotRead(t *testing.T) {
	tests := map[string]func(t *testing.T, exiting string) func(name, dir string) (model.MemoryBreakdown, error){
		"unparseable": func(t *testing.T, exiting string) func(name, dir string) (model.MemoryBreakdown, error) {
			if err := os.WriteFile(filepath.Join(exiting, "memory.current"), []byte("not-a-number\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return ParseDirectory
		},
		"permission denied": func(_ *testing.T, exiting string) func(name, dir string) (model.MemoryBreakdown, error) {
			return func(name, dir string) (model.MemoryBreakdown, error) {
				if dir == exiting {
					return model.MemoryBreakdown{}, fmt.Errorf("read memory.current: %w", fs.ErrPermission)
				}
				return ParseDirectory(name, dir)
			}
		},
	}
	for name, prepare := range tests {
		t.Run(name, func(t *testing.T) {
			root, exiting, _ := churnFixture(t)
			entries, err := Walker{Root: root, parse: prepare(t, exiting)}.Walk()
			if err == nil || !strings.Contains(err.Error(), exitingContainerID) {
				t.Fatalf("unreadable cgroup was not reported: %v", err)
			}
			if len(entries) != 1 || entries[0].ContainerID != remainingContainerID {
				t.Fatalf("entries = %+v, want the readable container to be kept", entries)
			}
		})
	}
}
