package probe

import (
	"testing"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
)

func TestProbeSyscallUsesTheStableLinuxUAPIPrefix(t *testing.T) {
	var attr progLoadAttr
	if unsafe.Sizeof(attr) != 64 || unsafe.Offsetof(attr.Instructions) != 8 || unsafe.Offsetof(attr.License) != 16 || unsafe.Offsetof(attr.LogBuffer) != 32 || unsafe.Offsetof(attr.Name) != 48 {
		t.Fatal("BPF_PROG_LOAD ABI layout changed")
	}
}

func TestUnreviewedProbeInputsAreRejectedBeforeAnySyscall(t *testing.T) {
	if err := probeProgramme(ebpf.SocketFilter, nil); err == nil {
		t.Fatal("arbitrary programme type accepted")
	}
	if err := probeProgramme(ebpf.Kprobe, make(asm.Instructions, 100)); err == nil {
		t.Fatal("unbounded instruction sequence accepted")
	}
}
