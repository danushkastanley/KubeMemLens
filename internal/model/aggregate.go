package model

func AddMemory(a, b MemoryBreakdown) MemoryBreakdown {
	// An aggregate has a finite limit only when every member has one: a known
	// limit summed with an unread one would understate the real ceiling. An
	// empty accumulator has no limit of its own to contribute.
	unlimited := a.MaxUnlimited || b.MaxUnlimited
	limitKnown := unlimited || (b.MaxKnown && (a.MaxKnown || a == MemoryBreakdown{Name: a.Name}))
	localKnown := a.LocalEventsKnown || b.LocalEventsKnown
	localA, localB := a.ownEvents(localKnown), b.ownEvents(localKnown)
	return MemoryBreakdown{
		Name:                    a.Name,
		TotalBytes:              a.TotalBytes + b.TotalBytes,
		AnonBytes:               a.AnonBytes + b.AnonBytes,
		FileBytes:               a.FileBytes + b.FileBytes,
		ActiveFileBytes:         a.ActiveFileBytes + b.ActiveFileBytes,
		InactiveFileBytes:       a.InactiveFileBytes + b.InactiveFileBytes,
		ShmemBytes:              a.ShmemBytes + b.ShmemBytes,
		SlabBytes:               a.SlabBytes + b.SlabBytes,
		SlabReclaimableBytes:    a.SlabReclaimableBytes + b.SlabReclaimableBytes,
		SlabUnreclaimableBytes:  a.SlabUnreclaimableBytes + b.SlabUnreclaimableBytes,
		KernelBytes:             a.KernelBytes + b.KernelBytes,
		SocketBytes:             a.SocketBytes + b.SocketBytes,
		PageTableBytes:          a.PageTableBytes + b.PageTableBytes,
		FileMappedBytes:         a.FileMappedBytes + b.FileMappedBytes,
		AnonTHPBytes:            a.AnonTHPBytes + b.AnonTHPBytes,
		FileTHPBytes:            a.FileTHPBytes + b.FileTHPBytes,
		ShmemTHPBytes:           a.ShmemTHPBytes + b.ShmemTHPBytes,
		DirtyBytes:              a.DirtyBytes + b.DirtyBytes,
		WritebackBytes:          a.WritebackBytes + b.WritebackBytes,
		PeakBytes:               a.PeakBytes + b.PeakBytes,
		PeakKnown:               a.PeakKnown || b.PeakKnown,
		MinBytes:                a.MinBytes + b.MinBytes,
		MinKnown:                a.MinKnown || b.MinKnown,
		MinUnlimited:            a.MinUnlimited || b.MinUnlimited,
		LowBytes:                a.LowBytes + b.LowBytes,
		LowKnown:                a.LowKnown || b.LowKnown,
		LowUnlimited:            a.LowUnlimited || b.LowUnlimited,
		HighBytes:               a.HighBytes + b.HighBytes,
		HighKnown:               a.HighKnown || b.HighKnown,
		HighUnlimited:           a.HighUnlimited || b.HighUnlimited,
		MaxBytes:                a.MaxBytes + b.MaxBytes,
		MaxKnown:                limitKnown,
		MaxUnlimited:            unlimited,
		SwapCurrentBytes:        a.SwapCurrentBytes + b.SwapCurrentBytes,
		SwapCurrentKnown:        a.SwapCurrentKnown || b.SwapCurrentKnown,
		SwapPeakBytes:           a.SwapPeakBytes + b.SwapPeakBytes,
		SwapPeakKnown:           a.SwapPeakKnown || b.SwapPeakKnown,
		SwapMaxBytes:            a.SwapMaxBytes + b.SwapMaxBytes,
		SwapMaxKnown:            a.SwapMaxKnown || b.SwapMaxKnown,
		SwapMaxUnlimited:        a.SwapMaxUnlimited || b.SwapMaxUnlimited,
		PressureKnown:           a.PressureKnown || b.PressureKnown,
		PSISomeAvg10:            maxFloat(a.PSISomeAvg10, b.PSISomeAvg10),
		PSISomeAvg60:            maxFloat(a.PSISomeAvg60, b.PSISomeAvg60),
		PSISomeAvg300:           maxFloat(a.PSISomeAvg300, b.PSISomeAvg300),
		PSISomeTotalMicros:      a.PSISomeTotalMicros + b.PSISomeTotalMicros,
		PSIFullAvg10:            maxFloat(a.PSIFullAvg10, b.PSIFullAvg10),
		PSIFullAvg60:            maxFloat(a.PSIFullAvg60, b.PSIFullAvg60),
		PSIFullAvg300:           maxFloat(a.PSIFullAvg300, b.PSIFullAvg300),
		PSIFullTotalMicros:      a.PSIFullTotalMicros + b.PSIFullTotalMicros,
		WorkingsetRefaultAnon:   a.WorkingsetRefaultAnon + b.WorkingsetRefaultAnon,
		WorkingsetRefaultFile:   a.WorkingsetRefaultFile + b.WorkingsetRefaultFile,
		PageScan:                a.PageScan + b.PageScan,
		PageSteal:               a.PageSteal + b.PageSteal,
		MajorPageFaults:         a.MajorPageFaults + b.MajorPageFaults,
		ReclaimCountersKnown:    a.ReclaimCountersKnown || b.ReclaimCountersKnown,
		ReclaimDeltasKnown:      a.ReclaimDeltasKnown || b.ReclaimDeltasKnown,
		RefaultAnonDelta:        a.RefaultAnonDelta + b.RefaultAnonDelta,
		RefaultFileDelta:        a.RefaultFileDelta + b.RefaultFileDelta,
		PageScanDelta:           a.PageScanDelta + b.PageScanDelta,
		PageStealDelta:          a.PageStealDelta + b.PageStealDelta,
		MajorPageFaultsDelta:    a.MajorPageFaultsDelta + b.MajorPageFaultsDelta,
		OOMEvents:               a.OOMEvents + b.OOMEvents,
		OOMKillEvents:           a.OOMKillEvents + b.OOMKillEvents,
		HighEvents:              a.HighEvents + b.HighEvents,
		MaxEvents:               a.MaxEvents + b.MaxEvents,
		LocalEventsKnown:        localKnown,
		LocalOOMEvents:          localA.oom + localB.oom,
		LocalOOMKillEvents:      localA.oomKill + localB.oomKill,
		LocalHighEvents:         localA.high + localB.high,
		LocalMaxEvents:          localA.max + localB.max,
		SwapEventsKnown:         a.SwapEventsKnown || b.SwapEventsKnown,
		SwapHighEvents:          a.SwapHighEvents + b.SwapHighEvents,
		SwapMaxEvents:           a.SwapMaxEvents + b.SwapMaxEvents,
		SwapFailEvents:          a.SwapFailEvents + b.SwapFailEvents,
		EventDeltasKnown:        a.EventDeltasKnown || b.EventDeltasKnown,
		OOMEventsDelta:          a.OOMEventsDelta + b.OOMEventsDelta,
		OOMKillEventsDelta:      a.OOMKillEventsDelta + b.OOMKillEventsDelta,
		HighEventsDelta:         a.HighEventsDelta + b.HighEventsDelta,
		MaxEventsDelta:          a.MaxEventsDelta + b.MaxEventsDelta,
		LocalEventDeltasKnown:   a.LocalEventDeltasKnown || b.LocalEventDeltasKnown,
		LocalOOMEventsDelta:     localA.oomDelta + localB.oomDelta,
		LocalOOMKillEventsDelta: localA.oomKillDelta + localB.oomKillDelta,
		LocalHighEventsDelta:    localA.highDelta + localB.highDelta,
		LocalMaxEventsDelta:     localA.maxDelta + localB.maxDelta,
		SwapEventDeltasKnown:    a.SwapEventDeltasKnown || b.SwapEventDeltasKnown,
		SwapHighEventsDelta:     a.SwapHighEventsDelta + b.SwapHighEventsDelta,
		SwapMaxEventsDelta:      a.SwapMaxEventsDelta + b.SwapMaxEventsDelta,
		SwapFailEventsDelta:     a.SwapFailEventsDelta + b.SwapFailEventsDelta,
	}
}

type eventCounters struct {
	oom, oomKill, high, max                     uint64
	oomDelta, oomKillDelta, highDelta, maxDelta uint64
}

// ownEvents returns the counters that describe m's own activity for a local
// roll-up: memory.events.local when the kernel reports it, otherwise the
// hierarchical counters, so a member without local events is not read as
// quiet once another member makes the aggregate use local counters.
func (m MemoryBreakdown) ownEvents(aggregateUsesLocal bool) eventCounters {
	if m.LocalEventsKnown {
		return eventCounters{
			oom: m.LocalOOMEvents, oomKill: m.LocalOOMKillEvents, high: m.LocalHighEvents, max: m.LocalMaxEvents,
			oomDelta: m.LocalOOMEventsDelta, oomKillDelta: m.LocalOOMKillEventsDelta,
			highDelta: m.LocalHighEventsDelta, maxDelta: m.LocalMaxEventsDelta,
		}
	}
	if !aggregateUsesLocal {
		return eventCounters{}
	}
	return eventCounters{
		oom: m.OOMEvents, oomKill: m.OOMKillEvents, high: m.HighEvents, max: m.MaxEvents,
		oomDelta: m.OOMEventsDelta, oomKillDelta: m.OOMKillEventsDelta,
		highDelta: m.HighEventsDelta, maxDelta: m.MaxEventsDelta,
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func SumMemory(name string, items []MemoryBreakdown) MemoryBreakdown {
	sum := MemoryBreakdown{Name: name}
	for _, item := range items {
		sum = AddMemory(sum, item)
	}
	sum.Name = name
	return sum
}
