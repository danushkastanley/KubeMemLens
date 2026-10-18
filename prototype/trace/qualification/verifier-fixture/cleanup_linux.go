package verifierfixture

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Absent checks only a programme ID returned by this fixture. A surviving object
// is not deleted: any newly opened inspection FD is closed and absence fails.
func Absent(id uint32) error {
	if id == 0 {
		return ErrFixture
	}
	attr := struct{ ID, Next, Flags uint32 }{ID: id}
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_GET_FD_BY_ID, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	runtime.KeepAlive(&attr)
	if errno == unix.ENOENT {
		return nil
	}
	if errno == 0 {
		return errors.Join(ErrFixture, unix.Close(int(fd)))
	}
	return errors.Join(ErrFixture, errno)
}
