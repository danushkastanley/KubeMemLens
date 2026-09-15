//go:build linux && (amd64 || arm64)

package main

import (
	"os"

	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
	"golang.org/x/sys/unix"
)

// Inherited standard descriptors are normally blocking and os.NewFile does not
// register them with Go's poller. A nonblocking duplicate makes deadlines real.
// Use positional roles 3..6 only for the target/image/bundle/policy descriptors.
func pollablePipe(source *os.File) (*os.File, error) {
	if source == nil || !isPipe(source) {
		return nil, workeripc.ErrProtocol
	}
	fd, err := unix.FcntlInt(source.Fd(), unix.F_DUPFD_CLOEXEC, 7)
	if err != nil {
		return nil, workeripc.ErrProtocol
	}
	if unix.SetNonblock(fd, true) != nil {
		_ = unix.Close(fd)
		return nil, workeripc.ErrProtocol
	}
	return os.NewFile(uintptr(fd), "private-worker-pipe"), nil
}
