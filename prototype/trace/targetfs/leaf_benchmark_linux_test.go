package targetfs

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// Read-only component measurement on an explicitly supplied local cgroup.
// It neither creates a cgroup nor qualifies whole-installation overhead.
func BenchmarkLeafCheck(b *testing.B) {
	path := os.Getenv("KML_TARGETFS_BENCHMARK")
	if path == "" {
		b.Skip("requires an explicitly selected local cgroup")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		b.Fatal("benchmark cgroup unavailable")
	}
	defer unix.Close(fd)
	var fs unix.Statfs_t
	if unix.Fstatfs(fd, &fs) != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC || leaf(fd) != nil {
		b.Fatal("benchmark requires a live cgroup-v2 leaf")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if leaf(fd) != nil {
			b.Fatal("benchmark cgroup ceased to be a leaf")
		}
	}
}
