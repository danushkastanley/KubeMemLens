package model

// KernelNotReported is shown in place of a kernel figure the node's kernel
// does not provide.
const KernelNotReported = "not reported by this kernel"

// KernelReported reports whether memory.stat carried a kernel total.
//
// The "kernel" key arrived in Linux 5.18. On older kernels KernelBytes is zero
// because the key is absent, not because the cgroup uses no kernel memory. A
// reported total includes slab and page tables, so a zero total beside a
// non-zero part can only mean the key was missing. Deriving this from values
// already collected keeps the snapshot format unchanged, and it also applies
// to captures and agents from before this check existed.
func (m MemoryBreakdown) KernelReported() bool {
	return m.KernelBytes > 0 || (m.SlabBytes == 0 && m.PageTableBytes == 0)
}

func FormatKernelTotal(m MemoryBreakdown) string {
	if !m.KernelReported() {
		return KernelNotReported
	}
	return FormatBytes(m.KernelBytes)
}

func FormatKernelOther(m MemoryBreakdown) string {
	if !m.KernelReported() {
		return KernelNotReported
	}
	return FormatBytes(m.KernelOtherBytes())
}
