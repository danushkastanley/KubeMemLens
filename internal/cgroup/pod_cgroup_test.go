package cgroup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestReadPodCgroupReportsEachFileOrUnknown(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join("kubepods.slice", "kubepods-pod"+testPodUIDSystemd+".slice")
	writeFile(t, mkdir(t, root, full), "memory.current", "300\n")
	writeFile(t, filepath.Join(root, full), "memory.max", "268435456\n")
	writeFile(t, filepath.Join(root, full), "memory.events.local", "low 0\nhigh 0\nmax 3\noom 2\noom_kill 1\noom_group_kill 1\n")
	unlimited := filepath.Join("kubepods.slice", "kubepods-burstable.slice")
	writeFile(t, mkdir(t, root, unlimited), "memory.max", "max\n")

	cgroupRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cgroupRoot.Close()
	for dir, want := range map[string]model.PodCgroupMemory{
		full: {CurrentBytes: 300, CurrentKnown: true, MaxBytes: 256 << 20, MaxKnown: true,
			LocalEventsKnown: true, LocalOOMEvents: 2, LocalOOMKillEvents: 1, LocalMaxEvents: 3},
		unlimited: {MaxKnown: true, MaxUnlimited: true},
	} {
		got, err := ReadPodCgroup(cgroupRoot, dir)
		if err != nil || got != want {
			t.Fatalf("ReadPodCgroup(%s) = %#v, %v; want %#v", dir, got, err, want)
		}
	}
}

// A Pod cgroup that disappears between the walk and the read is unknown, not
// a read error: there is nothing left to read. A live cgroup that cannot be
// read is still an error.
func TestReadPodCgroupTreatsAVanishedPodAsUnknown(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join("kubepods", "pod"+testPodUID)
	writeFile(t, mkdir(t, root, dir), "memory.current", "300\n")
	cgroupRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cgroupRoot.Close()
	if err := os.RemoveAll(filepath.Join(root, dir)); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadPodCgroup(cgroupRoot, dir); err != nil || got != (model.PodCgroupMemory{}) {
		t.Fatalf("removed Pod cgroup = %#v, %v; want unknown", got, err)
	}

	removedDuringRead := failingFS{MapFS: fstest.MapFS{"memory.current": {Data: []byte("300\n")}}, name: "memory.max", err: syscall.ENODEV}
	if got, err := readPodCgroupFS(removedDuringRead); err != nil || got != (model.PodCgroupMemory{}) {
		t.Fatalf("Pod cgroup removed during the read = %#v, %v; want unknown", got, err)
	}
	unreadable := failingFS{MapFS: fstest.MapFS{"memory.current": {Data: []byte("300\n")}}, name: "memory.max", err: fs.ErrPermission}
	if _, err := readPodCgroupFS(unreadable); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("unreadable Pod cgroup error = %v, want the permission error", err)
	}
}

func TestParseFSReadsZswapAndGroupKillOnlyWhenReported(t *testing.T) {
	reported := fstest.MapFS{
		"memory.current":      {Data: []byte("100\n")},
		"memory.stat":         {Data: []byte("anon 60\nzswap 4\nzswapped 12\n")},
		"memory.events":       {Data: []byte("oom 1\noom_kill 1\noom_group_kill 2\n")},
		"memory.events.local": {Data: []byte("oom 1\noom_kill 1\noom_group_kill 1\n")},
	}
	memory, err := ParseFS("reported", reported)
	if err != nil {
		t.Fatal(err)
	}
	if memory.Zswap != (model.Zswap{Known: true, PoolBytes: 4, SwappedBytes: 12}) || memory.OOMGroupKill != (model.OOMGroupKill{Known: true, Events: 2, LocalKnown: true, LocalEvents: 1}) {
		t.Fatalf("reported values = %#v / %#v", memory.Zswap, memory.OOMGroupKill)
	}

	older := fstest.MapFS{
		"memory.current": {Data: []byte("100\n")},
		"memory.stat":    {Data: []byte("anon 60\nzswap 4\n")},
		"memory.events":  {Data: []byte("oom 1\noom_kill 1\n")},
	}
	memory, err = ParseFS("older", older)
	if err != nil {
		t.Fatal(err)
	}
	if memory.Zswap != (model.Zswap{}) || memory.OOMGroupKill != (model.OOMGroupKill{}) {
		t.Fatalf("an older kernel's missing keys read as %#v / %#v, want unknown", memory.Zswap, memory.OOMGroupKill)
	}
}

type failingFS struct {
	fstest.MapFS
	name string
	err  error
}

func (f failingFS) Open(name string) (fs.File, error) {
	if name == f.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: f.err}
	}
	return f.MapFS.Open(name)
}

func mkdir(t *testing.T, root, dir string) string {
	t.Helper()
	path := filepath.Join(root, dir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
