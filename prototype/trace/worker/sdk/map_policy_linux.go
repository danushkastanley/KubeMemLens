package sdk

import (
	"github.com/cilium/ebpf"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"golang.org/x/sys/unix"
)

// The pinned loader adds MMAPABLE to ELF data-section maps when supported by
// the kernel. Keep the signed ELF flags exact; account for this one documented
// load-time transformation only on the two fixed data sections.
func matchesLoadedMap(expected filecache.MapInventory, info *ebpf.MapInfo) bool {
	if info == nil || info.Type.String() != expected.Type || info.KeySize != expected.KeyBytes || info.ValueSize != expected.ValueBytes || info.MaxEntries != expected.MaxEntries {
		return false
	}
	flags := info.Flags
	if expected.Name == ".bss" || expected.Name == ".rodata" {
		flags &^= unix.BPF_F_MMAPABLE
	}
	return flags == expected.Flags
}
