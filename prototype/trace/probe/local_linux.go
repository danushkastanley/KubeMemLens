package probe

import (
	"context"
	"errors"
	"io"
	"os"

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"golang.org/x/sys/unix"
)

// Local observes this process's node view. It never mounts filesystems, attaches
// programmes, pins objects, or removes BPF state. The caller owns the deadline.
type Local struct {
	bundle string
	read   func(string, int64) ([]byte, error)
	statfs func(string) (int64, error)
	uname  func() (string, string, error)
	before map[int]object
}

func New(bundle string) *Local {
	return &Local{bundle: bundle, read: readBounded, statfs: filesystemType, uname: kernelRelease}
}

func (l *Local) Probe(ctx context.Context, id p.ID) p.Check {
	if ctx.Err() != nil {
		return result(id, p.Degraded, p.ProbeTimeout, "")
	}
	switch id {
	case p.Platform:
		return result(id, p.Supported, p.Available, "linux")
	case p.Architecture:
		return l.architecture()
	case p.Kernel:
		return l.kernel()
	case p.CgroupV2:
		return l.cgroup()
	case p.BTF:
		return l.btf()
	case p.Capabilities:
		return l.capabilities()
	case p.SecurityPolicy:
		return l.policy()
	case p.LSM:
		return l.lsm()
	case p.BPFFS:
		return l.bpffs()
	case p.EngineIdentity:
		return l.identity()
	case p.Ownership:
		return l.ownership(ctx)
	case p.ProbeCleanup:
		return l.cleanup(ctx)
	case p.FileHooks, p.CacheHooks, p.OOMHook:
		return l.hooks(id)
	default:
		return feature(id)
	}
}

func result(id p.ID, state p.State, reason p.Reason, value string) p.Check {
	return p.Check{ID: id, State: state, Reason: reason, Value: value}
}

func readBounded(path string, maxBytes int64) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("preflight input is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("preflight input exceeds bound")
	}
	return data, nil
}

func filesystemType(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Type), nil
}

func kernelRelease() (string, string, error) {
	var value unix.Utsname
	if err := unix.Uname(&value); err != nil {
		return "", "", err
	}
	return unix.ByteSliceToString(value.Release[:]), unix.ByteSliceToString(value.Machine[:]), nil
}
