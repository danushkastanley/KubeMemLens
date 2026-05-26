package model

func AddMemory(a, b MemoryBreakdown) MemoryBreakdown {
	return MemoryBreakdown{
		Name:              a.Name,
		TotalBytes:        a.TotalBytes + b.TotalBytes,
		AnonBytes:         a.AnonBytes + b.AnonBytes,
		FileBytes:         a.FileBytes + b.FileBytes,
		ActiveFileBytes:   a.ActiveFileBytes + b.ActiveFileBytes,
		InactiveFileBytes: a.InactiveFileBytes + b.InactiveFileBytes,
		ShmemBytes:        a.ShmemBytes + b.ShmemBytes,
		SlabBytes:         a.SlabBytes + b.SlabBytes,
		KernelBytes:       a.KernelBytes + b.KernelBytes,
		DirtyBytes:        a.DirtyBytes + b.DirtyBytes,
		WritebackBytes:    a.WritebackBytes + b.WritebackBytes,
		OOMEvents:         a.OOMEvents + b.OOMEvents,
		OOMKillEvents:     a.OOMKillEvents + b.OOMKillEvents,
		HighEvents:        a.HighEvents + b.HighEvents,
		MaxEvents:         a.MaxEvents + b.MaxEvents,
	}
}

func SumMemory(name string, items []MemoryBreakdown) MemoryBreakdown {
	sum := MemoryBreakdown{Name: name}
	for _, item := range items {
		sum = AddMemory(sum, item)
	}
	sum.Name = name
	return sum
}
