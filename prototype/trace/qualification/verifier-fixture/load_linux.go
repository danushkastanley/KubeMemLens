package verifierfixture

import (
	"bytes"
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The reviewed BPF_PROG_LOAD UAPI prefix through log_true_size is 144 bytes.
// All unused fields remain zero. No library retry or log resizing is allowed.
type loadAttr struct {
	Type, Count                                                  uint32
	Instructions, License                                        uint64
	LogLevel, LogSize                                            uint32
	LogBuffer                                                    uint64
	KernelVersion, Flags                                         uint32
	Name                                                         [16]byte
	Ifindex, ExpectedAttachType, BTFFD, FuncInfoRecordSize       uint32
	FuncInfo                                                     uint64
	FuncInfoCount, LineInfoRecordSize                            uint32
	LineInfo                                                     uint64
	LineInfoCount, AttachBTFID, AttachBTFFD, CoreRelocationCount uint32
	FDArray, CoreRelocations                                     uint64
	CoreRelocationRecordSize, LogTrueSize                        uint32
}

func clock() (uint64, error) {
	var t unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &t) != nil || t.Sec < 0 || t.Nsec < 0 {
		return 0, ErrFixture
	}
	return uint64(t.Nano()), nil
}

func programmeID(fd int) (uint32, error) {
	info := struct{ Type, ID uint32 }{}
	attr := struct {
		FD, Size uint32
		Info     uint64
	}{uint32(fd), uint32(unsafe.Sizeof(info)), uint64(uintptr(unsafe.Pointer(&info)))}
	_, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_OBJ_GET_INFO_BY_FD, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	runtime.KeepAlive(&info)
	if errno != 0 || info.Type != unix.BPF_PROG_TYPE_TRACEPOINT || info.ID == 0 {
		return 0, ErrFixture
	}
	return info.ID, nil
}

// Run performs exactly one fixed BPF_PROG_LOAD. Accepted FDs are identified and
// closed without attaching or pinning. Text stays in a 4 KiB buffer and is
// cleared; only byte counts and the kernel's log_true_size leave the function.
func Run(which Case) (out Outcome, result error) {
	d, err := describe(which)
	if err != nil || (runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64") {
		return out, ErrFixture
	}
	license := []byte("GPL\x00")
	var log [4096]byte
	defer clear(log[:])
	var pinned runtime.Pinner
	pinned.Pin(&d.code[0])
	pinned.Pin(&license[0])
	pinned.Pin(&log[0])
	defer pinned.Unpin()
	attr := loadAttr{Type: unix.BPF_PROG_TYPE_TRACEPOINT, Count: 2,
		Instructions: uint64(uintptr(unsafe.Pointer(&d.code[0]))), License: uint64(uintptr(unsafe.Pointer(&license[0]))), LogLevel: d.level}
	copy(attr.Name[:], "kml_vcal")
	if d.level != 0 {
		attr.LogSize = uint32(len(log))
		attr.LogBuffer = uint64(uintptr(unsafe.Pointer(&log[0])))
	}
	started, err := clock()
	if err != nil {
		return out, err
	}
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_LOAD, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	runtime.KeepAlive(&d.code)
	runtime.KeepAlive(license)
	runtime.KeepAlive(&log)
	ended, timeErr := clock()
	out = Outcome{Case: which, Accepted: errno == 0, Errno: uint32(errno), LogBufferBytes: attr.LogSize, RequiredLogBytes: attr.LogTrueSize}
	if errno == 0 {
		defer func() { result = errors.Join(result, unix.Close(int(fd))) }()
		out.ProgrammeID, err = programmeID(int(fd))
		if err != nil {
			return out, err
		}
	}
	if timeErr != nil || ended <= started {
		return out, ErrFixture
	}
	out.SyscallUpperNanos = ended - started
	end := bytes.IndexByte(log[:], 0)
	if end < 0 {
		return out, ErrFixture
	}
	out.ObservedLogBytes = uint32(end)
	if (which == Rejected) == out.Accepted {
		return out, ErrFixture
	}
	if which == NoLog {
		if out.RequiredLogBytes != 0 || out.ObservedLogBytes != 0 {
			return out, ErrFixture
		}
	} else if out.RequiredLogBytes == 0 || out.RequiredLogBytes > uint32(len(log)) || out.ObservedLogBytes == 0 ||
		out.ObservedLogBytes+1 > out.RequiredLogBytes {
		return out, ErrFixture
	}
	return out, nil
}
