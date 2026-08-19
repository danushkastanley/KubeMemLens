package model

// MemoryBreakdown is the v0.1 memory shape used by the parser, CLI, agent, and
// future collector.
//
// AnonBytes is used as the RSS/anonymous memory proxy for this MVP. FileBytes is
// the raw cgroup v2 "file" value and includes shmem/tmpfs. KernelBytes is the raw
// "kernel" value and includes slab. Use FileCacheBytes and KernelOtherBytes when
// presenting mutually exclusive subcategories. TotalBytes comes from
// memory.current when available. Some fields may be zero on older kernels.
type MemoryBreakdown struct {
	Name string

	TotalBytes             uint64
	AnonBytes              uint64
	FileBytes              uint64
	ActiveFileBytes        uint64
	InactiveFileBytes      uint64
	ShmemBytes             uint64
	SlabBytes              uint64
	SlabReclaimableBytes   uint64
	SlabUnreclaimableBytes uint64
	KernelBytes            uint64
	SocketBytes            uint64
	PageTableBytes         uint64
	FileMappedBytes        uint64
	AnonTHPBytes           uint64
	FileTHPBytes           uint64
	ShmemTHPBytes          uint64
	DirtyBytes             uint64
	WritebackBytes         uint64
	PeakBytes              uint64
	PeakKnown              bool

	MinBytes      uint64
	MinKnown      bool
	MinUnlimited  bool
	LowBytes      uint64
	LowKnown      bool
	LowUnlimited  bool
	HighBytes     uint64
	HighKnown     bool
	HighUnlimited bool
	MaxBytes      uint64
	MaxKnown      bool
	MaxUnlimited  bool

	SwapCurrentBytes uint64
	SwapCurrentKnown bool
	SwapPeakBytes    uint64
	SwapPeakKnown    bool
	SwapMaxBytes     uint64
	SwapMaxKnown     bool
	SwapMaxUnlimited bool

	PressureKnown      bool
	PSISomeAvg10       float64
	PSISomeAvg60       float64
	PSISomeAvg300      float64
	PSISomeTotalMicros uint64
	PSIFullAvg10       float64
	PSIFullAvg60       float64
	PSIFullAvg300      float64
	PSIFullTotalMicros uint64

	WorkingsetRefaultAnon uint64
	WorkingsetRefaultFile uint64
	PageScan              uint64
	PageSteal             uint64
	MajorPageFaults       uint64
	ReclaimCountersKnown  bool
	ReclaimDeltasKnown    bool
	RefaultAnonDelta      uint64
	RefaultFileDelta      uint64
	PageScanDelta         uint64
	PageStealDelta        uint64
	MajorPageFaultsDelta  uint64

	OOMEvents     uint64
	OOMKillEvents uint64
	HighEvents    uint64
	MaxEvents     uint64

	LocalEventsKnown   bool
	LocalOOMEvents     uint64
	LocalOOMKillEvents uint64
	LocalHighEvents    uint64
	LocalMaxEvents     uint64

	SwapEventsKnown bool
	SwapHighEvents  uint64
	SwapMaxEvents   uint64
	SwapFailEvents  uint64

	EventDeltasKnown   bool
	OOMEventsDelta     uint64
	OOMKillEventsDelta uint64
	HighEventsDelta    uint64
	MaxEventsDelta     uint64

	LocalEventDeltasKnown   bool
	LocalOOMEventsDelta     uint64
	LocalOOMKillEventsDelta uint64
	LocalHighEventsDelta    uint64
	LocalMaxEventsDelta     uint64

	SwapEventDeltasKnown bool
	SwapHighEventsDelta  uint64
	SwapMaxEventsDelta   uint64
	SwapFailEventsDelta  uint64
}

func (m MemoryBreakdown) RSSBytes() uint64 {
	return m.AnonBytes
}

func (m MemoryBreakdown) CacheBytes() uint64 {
	return m.FileCacheBytes()
}

func (m MemoryBreakdown) FileCacheBytes() uint64 {
	return subtractFloor(m.FileBytes, m.ShmemBytes)
}

func (m MemoryBreakdown) KernelOtherBytes() uint64 {
	return subtractFloor(m.KernelBytes, m.SlabBytes)
}

// ResidualBytes is the primary non-overlapping "other" bucket: the part of
// memory.current not classified as anonymous, file cache, or shmem. It includes
// kernel memory, which is reported separately only as overlapping detail.
// Cgroup files are sampled independently, so the result floors at zero when
// their reported values briefly exceed memory.current.
func (m MemoryBreakdown) ResidualBytes() uint64 {
	return subtractManyFloor(
		m.TotalBytes,
		m.AnonBytes,
		m.FileCacheBytes(),
		m.ShmemBytes,
	)
}

func (m MemoryBreakdown) DirtyWritebackBytes() uint64 {
	return m.DirtyBytes + m.WritebackBytes
}

func (m MemoryBreakdown) FileCacheRatio() float64 {
	return ratio(m.FileCacheBytes(), m.TotalBytes)
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
	oom, oomKill, _, maxEvents := m.RecentEventCounts()
	return oom > 0 || oomKill > 0 || maxEvents > 0
}

func (m MemoryBreakdown) HasPressureRisk() bool {
	_, _, high, _ := m.RecentEventCounts()
	return high > 0 || (m.PressureKnown && (m.PSISomeAvg10 >= 1 || m.PSIFullAvg10 > 0))
}

func (m MemoryBreakdown) HasLimitRisk() bool {
	return m.MaxKnown && !m.MaxUnlimited && m.MaxBytes > 0 && ratio(m.TotalBytes, m.MaxBytes) >= 0.90
}

func (m MemoryBreakdown) LimitUsageRatio() float64 {
	if !m.MaxKnown || m.MaxUnlimited {
		return 0
	}
	return ratio(m.TotalBytes, m.MaxBytes)
}

func (m MemoryBreakdown) RecentEventCounts() (oom, oomKill, high, maxEvents uint64) {
	if m.LocalEventsKnown {
		if m.LocalEventDeltasKnown {
			return m.LocalOOMEventsDelta, m.LocalOOMKillEventsDelta, m.LocalHighEventsDelta, m.LocalMaxEventsDelta
		}
		return m.LocalOOMEvents, m.LocalOOMKillEvents, m.LocalHighEvents, m.LocalMaxEvents
	}
	if m.EventDeltasKnown {
		return m.OOMEventsDelta, m.OOMKillEventsDelta, m.HighEventsDelta, m.MaxEventsDelta
	}
	return m.OOMEvents, m.OOMKillEvents, m.HighEvents, m.MaxEvents
}

func WithEventDeltas(current MemoryBreakdown, previous MemoryBreakdown, hasPrevious bool) MemoryBreakdown {
	current.EventDeltasKnown = true
	current.LocalEventDeltasKnown = current.LocalEventsKnown
	current.SwapEventDeltasKnown = current.SwapEventsKnown
	if !hasPrevious {
		return current
	}
	current.ReclaimDeltasKnown = current.ReclaimCountersKnown && previous.ReclaimCountersKnown && countersMonotonic(
		current.WorkingsetRefaultAnon, previous.WorkingsetRefaultAnon,
		current.WorkingsetRefaultFile, previous.WorkingsetRefaultFile,
		current.PageScan, previous.PageScan,
		current.PageSteal, previous.PageSteal,
		current.MajorPageFaults, previous.MajorPageFaults,
	)
	if current.ReclaimDeltasKnown {
		current.RefaultAnonDelta = current.WorkingsetRefaultAnon - previous.WorkingsetRefaultAnon
		current.RefaultFileDelta = current.WorkingsetRefaultFile - previous.WorkingsetRefaultFile
		current.PageScanDelta = current.PageScan - previous.PageScan
		current.PageStealDelta = current.PageSteal - previous.PageSteal
		current.MajorPageFaultsDelta = current.MajorPageFaults - previous.MajorPageFaults
	}
	current.OOMEventsDelta = counterDelta(current.OOMEvents, previous.OOMEvents)
	current.OOMKillEventsDelta = counterDelta(current.OOMKillEvents, previous.OOMKillEvents)
	current.HighEventsDelta = counterDelta(current.HighEvents, previous.HighEvents)
	current.MaxEventsDelta = counterDelta(current.MaxEvents, previous.MaxEvents)
	if current.LocalEventsKnown && previous.LocalEventsKnown {
		current.LocalOOMEventsDelta = counterDelta(current.LocalOOMEvents, previous.LocalOOMEvents)
		current.LocalOOMKillEventsDelta = counterDelta(current.LocalOOMKillEvents, previous.LocalOOMKillEvents)
		current.LocalHighEventsDelta = counterDelta(current.LocalHighEvents, previous.LocalHighEvents)
		current.LocalMaxEventsDelta = counterDelta(current.LocalMaxEvents, previous.LocalMaxEvents)
	}
	if current.SwapEventsKnown && previous.SwapEventsKnown {
		current.SwapHighEventsDelta = counterDelta(current.SwapHighEvents, previous.SwapHighEvents)
		current.SwapMaxEventsDelta = counterDelta(current.SwapMaxEvents, previous.SwapMaxEvents)
		current.SwapFailEventsDelta = counterDelta(current.SwapFailEvents, previous.SwapFailEvents)
	}
	return current
}

func countersMonotonic(values ...uint64) bool {
	for i := 0; i+1 < len(values); i += 2 {
		if values[i] < values[i+1] {
			return false
		}
	}
	return true
}

func ratio(part, total uint64) float64 {
	if total == 0 {
		return 0
	}

	return float64(part) / float64(total)
}

func subtractFloor(total, included uint64) uint64 {
	if included >= total {
		return 0
	}
	return total - included
}

func subtractManyFloor(total uint64, included ...uint64) uint64 {
	remaining := total
	for _, value := range included {
		if value >= remaining {
			return 0
		}
		remaining -= value
	}
	return remaining
}

func counterDelta(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}
