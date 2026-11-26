//go:build linux && (amd64 || arm64)

package testworker

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Baseline adds a test-only getppid denial in a dedicated child process. It
// never replaces/removes inherited filters. This supplies a real existing filter
// on bare CI runners and lets tests prove production restrictions compose with it.
func Baseline() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
		return errors.New("test baseline unavailable")
	}
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_GETPPID, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	programme := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	result, _, errno := unix.RawSyscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&programme)))
	runtime.KeepAlive(filter)
	if result != 0 || errno != 0 {
		return errors.New("test baseline unavailable")
	}
	return CheckBaseline()
}

func CheckBaseline() error {
	_, _, errno := unix.RawSyscall(unix.SYS_GETPPID, 0, 0, 0)
	if errno != unix.EPERM {
		return errors.New("existing baseline restriction lost")
	}
	return nil
}
