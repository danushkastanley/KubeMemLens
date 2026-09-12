package nodecontext

// HasMeasurements distinguishes absent fields from reported zero values.
func HasMeasurements(stats Stats) bool {
	known := func(memory *Memory, swap *Swap) bool {
		return (memory != nil && (memory.UsageBytes != nil || memory.AvailableBytes != nil || memory.WorkingSetBytes != nil || memory.RSSBytes != nil || memory.PageFaults != nil || memory.MajorPageFaults != nil || memory.PSI != nil)) ||
			(swap != nil && (swap.UsageBytes != nil || swap.AvailableBytes != nil))
	}
	if known(stats.Memory, stats.Swap) {
		return true
	}
	for _, item := range stats.SystemContainers {
		if known(item.Memory, item.Swap) {
			return true
		}
	}
	return false
}

func MemoryComplete(value *Memory) bool {
	return value != nil && value.UsageBytes != nil && value.AvailableBytes != nil && value.WorkingSetBytes != nil &&
		value.RSSBytes != nil && value.PageFaults != nil && value.MajorPageFaults != nil && value.PSI != nil
}

func SwapComplete(value *Swap) bool {
	return value != nil && value.UsageBytes != nil && value.AvailableBytes != nil
}

func StatsComplete(value Stats) bool {
	if !MemoryComplete(value.Memory) || !SwapComplete(value.Swap) || len(value.SystemContainers) != MaxSystemContainers {
		return false
	}
	for _, item := range value.SystemContainers {
		if !MemoryComplete(item.Memory) || item.StartedAt.IsZero() {
			return false
		}
	}
	return true
}
