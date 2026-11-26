package probe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"golang.org/x/sys/unix"
)

const verifierLogBytes = 4096

// progLoadAttr is the first 64 bytes of Linux union bpf_attr for BPF_PROG_LOAD.
// All later fields are zero. Keeping this single syscall local permits a strict
// log ceiling: the library's general feature probe grows its verifier buffer.
// Only the fixed non-attaching probes in features_linux.go call this function.
type progLoadAttr struct {
	Type          uint32
	Count         uint32
	Instructions  uint64
	License       uint64
	LogLevel      uint32
	LogSize       uint32
	LogBuffer     uint64
	KernelVersion uint32
	Flags         uint32
	Name          [16]byte
}

func probeProgramme(kind ebpf.ProgramType, prefix asm.Instructions) error {
	if kind != ebpf.Kprobe && kind != ebpf.TracePoint {
		return errors.New("unreviewed probe type")
	}
	if len(prefix) > 14 {
		return errors.New("verifier probe exceeds instruction budget")
	}
	instructions := append(append(asm.Instructions(nil), prefix...), asm.Mov.Imm(asm.R0, 0), asm.Return())
	var encoded bytes.Buffer
	if err := instructions.Marshal(&encoded, binary.LittleEndian); err != nil {
		return err
	}
	code := encoded.Bytes()
	if len(code) == 0 || len(code) > 8*16 {
		return errors.New("verifier probe exceeds instruction budget")
	}
	license := []byte("GPL\x00")
	log := make([]byte, verifierLogBytes)
	var pinned runtime.Pinner
	pinned.Pin(&code[0])
	pinned.Pin(&license[0])
	pinned.Pin(&log[0])
	defer pinned.Unpin()
	attr := progLoadAttr{Type: uint32(kind), Count: uint32(len(code) / 8),
		Instructions: uint64(uintptr(unsafe.Pointer(&code[0]))), License: uint64(uintptr(unsafe.Pointer(&license[0]))),
		LogLevel: 1, LogSize: verifierLogBytes, LogBuffer: uint64(uintptr(unsafe.Pointer(&log[0])))}
	copy(attr.Name[:], "kml_pf_probe")
	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_LOAD, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	runtime.KeepAlive(code)
	runtime.KeepAlive(license)
	runtime.KeepAlive(log)
	if errno == 0 {
		return unix.Close(int(fd))
	}
	if errno == unix.EINVAL && len(prefix) == 0 {
		return ebpf.ErrNotSupported
	}
	if errno == unix.EINVAL && (bytes.Contains(log, []byte("invalid func")) || bytes.Contains(log, []byte("unknown func")) || bytes.Contains(log, []byte("program of this type cannot use helper"))) {
		return ebpf.ErrNotSupported
	}
	// Verifier text stays in this bounded process buffer and is never returned.
	return errno
}
