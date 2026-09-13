package probe

import (
	"errors"
	"os"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"golang.org/x/sys/unix"
)

// Only these fixed feature probes are allowed. They load and close tiny verifier
// probes; no link/attach, pin, arbitrary programme or caller parameter is used.
func feature(id p.ID) p.Check {
	var err error
	missing := p.ProgrammeMissing
	switch id {
	case p.Kprobe:
		err = probeProgramme(ebpf.Kprobe, nil)
	case p.Tracepoint:
		err = probeProgramme(ebpf.TracePoint, nil)
	case p.CgroupHelper:
		err = probeProgramme(ebpf.Kprobe, asm.Instructions{asm.FnGetCurrentCgroupId.Call()})
		missing = p.HelperMissing
	case p.KernelReadHelper:
		err = probeProgramme(ebpf.Kprobe, asm.Instructions{asm.Mov.Reg(asm.R1, asm.R10), asm.Add.Imm(asm.R1, -8), asm.Mov.Imm(asm.R2, 8), asm.Mov.Imm(asm.R3, 0), asm.FnProbeReadKernel.Call()})
		missing = p.HelperMissing
	case p.BootTimeHelper:
		err = probeProgramme(ebpf.Kprobe, asm.Instructions{asm.FnKtimeGetBootNs.Call()})
		missing = p.HelperMissing
	case p.RingBuffer:
		err = probeRingBuffer()
		missing = p.MapMissing
	default:
		return result(id, p.Unsupported, p.ProbeInvalid, "")
	}
	return featureResult(id, missing, err)
}

func probeRingBuffer() error {
	page := os.Getpagesize()
	if page > 1<<20 {
		return ebpf.ErrNotSupported
	}
	m, err := ebpf.NewMap(&ebpf.MapSpec{Name: "kml_pf_ring", Type: ebpf.RingBuf, MaxEntries: uint32(page)})
	if errors.Is(err, unix.EINVAL) {
		return ebpf.ErrNotSupported
	}
	if err != nil {
		return err
	}
	return m.Close()
}

func featureResult(id p.ID, missing p.Reason, err error) p.Check {
	switch {
	case err == nil:
		return result(id, p.Supported, p.Available, "")
	case errors.Is(err, ebpf.ErrNotSupported):
		return result(id, p.Unsupported, missing, "")
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES):
		return result(id, p.Unsupported, p.PolicyDenied, "")
	default:
		return result(id, p.Degraded, p.ProbeFailed, "")
	}
}
