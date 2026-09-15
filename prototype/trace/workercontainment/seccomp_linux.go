//go:build linux && (amd64 || arm64)

// Package workercontainment adds process restrictions after worker bootstrap.
// It does not replace the installed container privilege and seccomp profile.
package workercontainment

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

var ErrContainment = errors.New("trace worker containment unavailable")

// Restrict requires an existing seccomp filter, then adds a synchronised layer
// prohibiting process descendants, further exec and new sockets. Go runtime
// threads remain permitted. Invoke only inside the dedicated worker process.
func Restrict() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	mode, err := unix.PrctlRetInt(unix.PR_GET_SECCOMP, 0, 0, 0, 0)
	if err != nil || mode != 2 || unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
		return ErrContainment
	}
	if unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil || unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}) != nil {
		return ErrContainment
	}
	filters := restrictionFilter()
	programme := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
	result, _, errno := unix.RawSyscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&programme)))
	runtime.KeepAlive(filters)
	if errno != 0 || result != 0 {
		return ErrContainment
	}
	return nil
}

func restrictionFilter() []unix.SockFilter {
	load := func(offset uint32) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: offset}
	}
	equal := func(value uint32, yes, no uint8) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: value, Jt: yes, Jf: no}
	}
	ret := func(value uint32) unix.SockFilter { return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: value} }
	deny := ret(unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM))
	filter := []unix.SockFilter{load(4), equal(auditArchitecture, 1, 0), ret(unix.SECCOMP_RET_KILL_PROCESS), load(0),
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: 0x40000000, Jf: 1}, ret(unix.SECCOMP_RET_KILL_PROCESS)}
	for _, number := range append([]uint32{unix.SYS_EXECVE, unix.SYS_EXECVEAT, unix.SYS_SOCKET, unix.SYS_SOCKETPAIR}, legacyForkCalls...) {
		filter = append(filter, equal(number, 0, 1), deny)
	}
	// Returning ENOSYS for clone3 permits libc/runtime implementations to use
	// clone, whose flags can be checked without dereferencing a userspace pointer.
	filter = append(filter, equal(unix.SYS_CLONE3, 0, 1), ret(unix.SECCOMP_RET_ERRNO|uint32(unix.ENOSYS)), equal(unix.SYS_CLONE, 1, 0), ret(unix.SECCOMP_RET_ALLOW))
	const required = unix.CLONE_VM | unix.CLONE_SIGHAND | unix.CLONE_THREAD
	const allowed = required | unix.CLONE_FS | unix.CLONE_FILES | unix.CLONE_SYSVSEM | unix.CLONE_SETTLS | unix.CLONE_PARENT_SETTID | unix.CLONE_CHILD_SETTID | unix.CLONE_CHILD_CLEARTID
	// seccomp_data.args[0] is 64-bit. Both supported ABIs are little endian.
	filter = append(filter, load(20), equal(0, 1, 0), deny, load(16),
		unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: ^uint32(allowed)}, equal(0, 1, 0), deny, load(16),
		unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: uint32(required)}, equal(uint32(required), 1, 0), deny, ret(unix.SECCOMP_RET_ALLOW))
	return filter
}
