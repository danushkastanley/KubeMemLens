package targetfs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"golang.org/x/sys/unix"
)

func TestCgroupRootSymlinkRejected(t *testing.T) {
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink("/sys/fs/cgroup", alias); err != nil {
		t.Fatal(err)
	}
	fd, err := openDirectory(alias, ".")
	if fd >= 0 {
		unix.Close(fd)
		t.Fatal("symlink root accepted")
	}
	if !errors.Is(err, unix.ELOOP) {
		t.Fatalf("symlink traversal not rejected at open: %v", err)
	}
}

// This administrator-only fixture creates two owned synthetic cgroup trees in
// a disposable kind node. It never moves existing processes or Pod cgroups.
// The actual runtime's capability-free read-only proof is TestLiveExactBinding.
func TestLiveAmbiguousAndReplacedCgroups(t *testing.T) {
	if os.Getenv("KML_TARGETFS_MUTATION") != "owned-local-kind-node-admin" {
		t.Skip("requires owned disposable kind node")
	}
	var fs unix.Statfs_t
	if unix.Statfs("/sys/fs/cgroup", &fs) != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC {
		t.Fatal("cgroup v2 required")
	}
	config := Config{MountPoint: "/sys/fs/cgroup", KubeletRoot: "/kml-binding-fixture"}
	workload := admission.Workload{Target: trace.TargetIdentity{Namespace: "fixture", PodName: "target", PodUID: "01234567-0123-4567-8901-012345678901", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Now().UTC(), NodeUID: "fixture-node"}, NodeName: "fixture-node", QoS: "Guaranteed"}
	paths, err := candidates(config, workload)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"kml-binding-fixture", "kml_binding_fixture.slice"} {
		if _, err := os.Lstat(filepath.Join(config.MountPoint, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("fixture root already exists or is inaccessible")
		}
	}
	var created []string
	t.Cleanup(func() {
		for i := len(created) - 1; i >= 0; i-- {
			if err := os.Remove(created[i]); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Error("fixture cgroup cleanup failed")
			}
		}
	})
	for _, relative := range paths {
		current := config.MountPoint
		for _, part := range strings.Split(relative, "/") {
			current = filepath.Join(current, part)
			if err := os.Mkdir(current, 0755); err != nil {
				t.Fatal("create owned fixture cgroup failed")
			}
			created = append(created, current)
		}
	}
	if h, err := Resolve(context.Background(), config, workload); !errors.Is(err, admission.ErrTargetChanged) {
		if h != nil {
			h.Close()
		}
		t.Fatalf("ambiguous layouts accepted: %v", err)
	}
	second := filepath.Join(config.MountPoint, paths[1])
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(config.MountPoint, paths[0])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, "sleep", "4")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	if err := os.WriteFile(filepath.Join(first, "cgroup.procs"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
		t.Fatal("populate owned fixture cgroup failed")
	}
	h, err := Resolve(ctx, config, workload)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	oldID := h.Target().CgroupID
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("fixture child unexpectedly succeeded")
	}
	if err := os.Remove(first); err != nil {
		t.Fatal("remove empty fixture cgroup failed")
	}
	if err := os.Mkdir(first, 0755); err != nil {
		t.Fatal("replace fixture cgroup failed")
	}
	fd, err := openDirectory(config.MountPoint, paths[0])
	if err != nil {
		t.Fatal(err)
	}
	newID, err := directoryID(fd)
	unix.Close(fd)
	if err != nil || newID == oldID {
		t.Fatal("kernel reused identity while old handle retained")
	}
	if err := h.Check(ctx); !errors.Is(err, admission.ErrTargetChanged) {
		t.Fatalf("replacement accepted: %v", err)
	}
	t.Log("ambiguous layouts and same-path replacement rejected; retained old identity distinct")
}
