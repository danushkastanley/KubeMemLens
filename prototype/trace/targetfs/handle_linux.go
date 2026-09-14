package targetfs

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"golang.org/x/sys/unix"
)

const kernfsHandleType = 0xfe
const maxDirectoryEntries = 256

// Handle's identity is immutable. Close and Check serialise descriptor access,
// so an expiry cannot make a concurrent check inspect a reused descriptor.
type handle struct {
	mu         sync.Mutex
	file       *os.File
	root, path string
	target     trace.TargetIdentity
}

func Resolve(ctx context.Context, config Config, w admission.Workload) (Handle, error) {
	paths, err := candidates(config, w)
	if err != nil {
		return nil, err
	}
	fd := -1
	var selected string
	for _, path := range paths {
		if ctx.Err() != nil {
			if fd >= 0 {
				unix.Close(fd)
			}
			return nil, admission.ErrUnavailable
		}
		candidate, err := openDirectory(config.MountPoint, path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			if fd >= 0 {
				unix.Close(fd)
			}
			return nil, admission.ErrUnavailable
		}
		if fd >= 0 {
			unix.Close(candidate)
			unix.Close(fd)
			return nil, admission.ErrTargetChanged
		}
		fd, selected = candidate, path
	}
	if fd < 0 {
		return nil, admission.ErrTargetChanged
	}
	id, err := directoryID(fd)
	if err != nil {
		unix.Close(fd)
		return nil, admission.ErrUnavailable
	}
	target := w.Target
	target.CgroupID = id
	handle := &handle{file: os.NewFile(uintptr(fd), "cgroup binding"), root: config.MountPoint, path: selected, target: target}
	if err := handle.Check(ctx); err != nil {
		handle.Close()
		return nil, err
	}
	return handle, nil
}

func (h *handle) Target() trace.TargetIdentity { return h.target }
func (h *handle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil {
		return nil
	}
	file := h.file
	h.file = nil
	if err := file.Close(); err != nil {
		return admission.ErrUnavailable
	}
	return nil
}
func (h *handle) Check(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil {
		return admission.ErrExpired
	}
	if ctx.Err() != nil {
		return admission.ErrUnavailable
	}
	current, err := openDirectory(h.root, h.path)
	if err != nil {
		return admission.ErrTargetChanged
	}
	defer unix.Close(current)
	identity, err := directoryID(current)
	if err != nil || identity != h.target.CgroupID {
		return admission.ErrTargetChanged
	}
	var stat unix.Stat_t
	if unix.Fstat(int(h.file.Fd()), &stat) != nil || stat.Nlink == 0 {
		return admission.ErrTargetChanged
	}
	if err := populated(int(h.file.Fd())); err != nil {
		return err
	}
	return leaf(int(h.file.Fd()))
}

func openDirectory(root, path string) (int, error) {
	rootFD, err := unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return -1, err
	}
	defer unix.Close(rootFD)
	var fs unix.Statfs_t
	if err := unix.Fstatfs(rootFD, &fs); err != nil {
		return -1, err
	}
	if fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return -1, admission.ErrUnavailable
	}
	return unix.Openat2(rootFD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
}

func directoryID(fd int) (uint64, error) {
	handle, _, err := unix.NameToHandleAt(fd, "", unix.AT_EMPTY_PATH)
	if err != nil {
		return 0, err
	}
	if handle.Type() != kernfsHandleType || handle.Size() != 8 {
		return 0, admission.ErrUnavailable
	}
	id := binary.NativeEndian.Uint64(handle.Bytes())
	if id == 0 {
		return 0, admission.ErrUnavailable
	}
	return id, nil
}

func populated(fd int) error {
	events, err := unix.Openat2(fd, "cgroup.events", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return admission.ErrTargetChanged
	}
	file := os.NewFile(uintptr(events), "cgroup.events")
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return admission.ErrUnavailable
	}
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, " ")
		if key != "populated" {
			continue
		}
		if found || !ok || value != "1" {
			return admission.ErrTargetChanged
		}
		found = true
	}
	if !found {
		return admission.ErrUnavailable
	}
	return nil
}

func leaf(fd int) error {
	directory, err := unix.Openat2(fd, ".", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return admission.ErrTargetChanged
	}
	file := os.NewFile(uintptr(directory), "cgroup")
	defer file.Close()
	entries, err := file.ReadDir(maxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return admission.ErrUnavailable
	}
	if len(entries) > maxDirectoryEntries {
		return admission.ErrUnavailable
	}
	for _, entry := range entries {
		var stat unix.Stat_t
		if unix.Fstatat(directory, entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW) != nil {
			return admission.ErrUnavailable
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			return admission.ErrUnavailable
		}
	}
	return nil
}

func (*handle) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[trace cgroup binding]") }
func (*handle) MarshalJSON() ([]byte, error) {
	return nil, errors.New("cgroup binding requires private transport")
}
