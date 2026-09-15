package sdk

import (
	"context"
	"os"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type resources struct {
	spec        trace.Specification
	target      *os.File
	inventory   filecache.ObjectInventory
	maps        map[string]*ebpf.Map
	reader      *ringbuf.Reader
	kernelBytes uint64
}

// beforeAttach is mandatory in the constrained SDK. It verifies every owned
// map, establishes an immutable kernel cgroup reference and opens the bounded
// reader while control remains disabled and no programme is attached.
func (r *resources) beforeAttach(ctx context.Context, owned map[string]*ebpf.Map) error {
	if ctx.Err() != nil || len(owned) != len(r.inventory.Maps) || targetfs.VerifyWorkerDescriptor(ctx, r.target, r.spec.Target()) != nil {
		return ErrWorker
	}
	r.maps = owned
	for _, expected := range r.inventory.Maps {
		m := owned[expected.Name]
		if m == nil {
			return ErrWorker
		}
		info, err := m.Info()
		if err != nil || !matchesLoadedMap(expected, info) {
			return ErrWorker
		}
		memory, known := info.Memlock()
		if !known || memory == 0 || memory > r.spec.Bounds().MapBytes-r.kernelBytes {
			return ErrWorker
		}
		r.kernelBytes += memory
	}
	// Reserve both virtual ring mappings, their control pages and one page
	// each for the fixed .bss/.rodata maps, in addition to kernel allocations.
	// The signed object policy bounds both data sections below one native page.
	userMapping := uint64(2*262144 + 4*os.Getpagesize())
	if userMapping > r.spec.Bounds().MapBytes-r.kernelBytes {
		return ErrWorker
	}
	zero := uint32(0)
	var enabled uint32
	if owned["control"].Lookup(zero, &enabled) != nil || enabled != 0 {
		return ErrWorker
	}
	fd := r.target.Fd()
	if fd > 1<<31-1 {
		return ErrWorker
	}
	if owned["target_ref"].Update(zero, uint32(fd), ebpf.UpdateAny) != nil || owned["target_ref"].Freeze() != nil {
		return ErrWorker
	}
	reader, err := ringbuf.NewReader(owned["events"])
	if err != nil {
		return ErrWorker
	}
	r.reader = reader
	return targetfs.VerifyWorkerDescriptor(ctx, r.target, r.spec.Target())
}

func (r *resources) enable(ctx context.Context) error {
	if r.reader == nil || targetfs.VerifyWorkerDescriptor(ctx, r.target, r.spec.Target()) != nil {
		return ErrWorker
	}
	return r.maps["control"].Update(uint32(0), uint32(1), ebpf.UpdateAny)
}
func (r *resources) disable() error {
	control := r.maps["control"]
	if control == nil {
		return nil
	}
	return control.Update(uint32(0), uint32(0), ebpf.UpdateAny)
}
func (r *resources) closeReader() error {
	if r.reader == nil {
		return nil
	}
	return r.reader.Close()
}
