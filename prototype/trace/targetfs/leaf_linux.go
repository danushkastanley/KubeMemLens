package targetfs

import (
	"io"
	"os"
	"strings"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"golang.org/x/sys/unix"
)

const maxLeafStatBytes = 4096

// Callers have verified the retained cgroup-v2 descriptor's identity and
// population. nr_descendants counts visible descendant cgroups, so zero is the
// leaf condition without a stat syscall for every controller file. Already
// unlinked dying groups cannot receive processes and are not visible children.
func leaf(fd int) error {
	stat, err := unix.Openat2(fd, "cgroup.stat", &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return admission.ErrTargetChanged
	}
	file := os.NewFile(uintptr(stat), "cgroup.stat")
	data, err := io.ReadAll(io.LimitReader(file, maxLeafStatBytes+1))
	closed := file.Close()
	if err != nil || closed != nil {
		return admission.ErrUnavailable
	}
	return leafStat(data)
}

func leafStat(data []byte) error {
	if len(data) == 0 || len(data) > maxLeafStatBytes {
		return admission.ErrUnavailable
	}
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok || key == "" || value == "" || strings.ContainsAny(key, "\t\r\x00") || strings.ContainsAny(value, " \t\r\x00") {
			return admission.ErrUnavailable
		}
		if key != "nr_descendants" {
			continue
		}
		if found || value != "0" {
			return admission.ErrUnavailable
		}
		found = true
	}
	if !found {
		return admission.ErrUnavailable
	}
	return nil
}
