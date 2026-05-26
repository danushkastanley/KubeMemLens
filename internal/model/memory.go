package model

// MemoryBreakdown is the v0.1 memory shape used by the parser, CLI, agent, and
// future collector.
//
// AnonBytes is used as the RSS/anonymous memory proxy for this MVP. FileBytes is
// page-cache/file-backed memory from cgroup accounting. TotalBytes comes from
// memory.current when available. Some fields may be zero on older kernels or
// cgroup versions.
type MemoryBreakdown struct {
	Name string

	TotalBytes        uint64
	AnonBytes         uint64
	FileBytes         uint64
	ActiveFileBytes   uint64
	InactiveFileBytes uint64
	ShmemBytes        uint64
	SlabBytes         uint64
	KernelBytes       uint64
	DirtyBytes        uint64
	WritebackBytes    uint64

	OOMEvents     uint64
	OOMKillEvents uint64
	HighEvents    uint64
	MaxEvents     uint64
}

func (m MemoryBreakdown) RSSBytes() uint64 {
	return m.AnonBytes
}

func (m MemoryBreakdown) CacheBytes() uint64 {
	return m.FileBytes
}

func (m MemoryBreakdown) DirtyWritebackBytes() uint64 {
	return m.DirtyBytes + m.WritebackBytes
}

func (m MemoryBreakdown) FileCacheRatio() float64 {
	return ratio(m.FileBytes, m.TotalBytes)
}

func (m MemoryBreakdown) AnonRatio() float64 {
	return ratio(m.AnonBytes, m.TotalBytes)
}

func (m MemoryBreakdown) ShmemRatio() float64 {
	return ratio(m.ShmemBytes, m.TotalBytes)
}

func (m MemoryBreakdown) SlabRatio() float64 {
	return ratio(m.SlabBytes, m.TotalBytes)
}

func (m MemoryBreakdown) DirtyWritebackRatio() float64 {
	return ratio(m.DirtyWritebackBytes(), m.TotalBytes)
}

func (m MemoryBreakdown) HasOOMRisk() bool {
	return m.OOMEvents > 0 || m.OOMKillEvents > 0 || m.MaxEvents > 0
}

func ratio(part, total uint64) float64 {
	if total == 0 {
		return 0
	}

	return float64(part) / float64(total)
}
