package targetfs

import (
	"context"
	"io"
	"os"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"golang.org/x/sys/unix"
)

// ExportForWorker duplicates only this resolver's retained descriptor. The
// caller owns the duplicate and may pass it to one supervised child process.
func ExportForWorker(ctx context.Context, binding Handle) (*os.File, error) {
	h, ok := binding.(*handle)
	if !ok || h.Check(ctx) != nil {
		return nil, admission.ErrTargetChanged
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := VerifyWorkerDescriptor(ctx, h.file, h.target); err != nil {
		return nil, err
	}
	fd, err := unix.FcntlInt(h.file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, admission.ErrUnavailable
	}
	return os.NewFile(uintptr(fd), "trace-target"), nil
}

// VerifyWorkerDescriptor checks kernel filesystem/handle identity, liveness and
// leaf status. The parent retains responsibility for Kubernetes reauthorisation.
func VerifyWorkerDescriptor(ctx context.Context, file *os.File, target trace.TargetIdentity) error {
	if ctx.Err() != nil || file == nil || target.ValidateLifetime() != nil || target.CgroupID == 0 {
		return admission.ErrTargetChanged
	}
	raw := file.Fd()
	if raw > 1<<31-1 {
		return admission.ErrTargetChanged
	}
	fd := int(raw)
	var fs unix.Statfs_t
	var stat unix.Stat_t
	if unix.Fstatfs(fd, &fs) != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC || unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Nlink == 0 {
		return admission.ErrTargetChanged
	}
	id, err := directoryID(fd)
	if err != nil || id != target.CgroupID || populated(fd) != nil {
		return admission.ErrTargetChanged
	}
	return leaf(fd)
}

// ReadWorkerMemoryStat reads a single fixed file through the verified handle.
// It neither resolves another cgroup nor substitutes zero for unavailable data.
func ReadWorkerMemoryStat(ctx context.Context, file *os.File, target trace.TargetIdentity) ([]byte, error) {
	if err := VerifyWorkerDescriptor(ctx, file, target); err != nil {
		return nil, err
	}
	fd, err := unix.Openat2(int(file.Fd()), "memory.stat", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return nil, admission.ErrUnavailable
	}
	stat := os.NewFile(uintptr(fd), "memory.stat")
	defer stat.Close()
	data, err := io.ReadAll(io.LimitReader(stat, 16385))
	if err != nil || len(data) > 16384 {
		return nil, admission.ErrUnavailable
	}
	if err := VerifyWorkerDescriptor(ctx, file, target); err != nil {
		return nil, err
	}
	return data, nil
}
