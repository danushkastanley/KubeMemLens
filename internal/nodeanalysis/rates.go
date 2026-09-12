package nodeanalysis

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func deriveRates(input Input, result *Analysis) {
	current, previous := input.Current, input.Previous
	if current.Stats == nil {
		return
	}
	swap := current.Stats.Swap
	if swap != nil && swap.UsageBytes != nil && *swap.UsageBytes > 0 {
		addCaveat(result, SwapNotIO)
	}
	if previous == nil || previous.NodeUID != current.NodeUID || previous.Stats == nil || !current.Stats.StartedAt.Equal(previous.Stats.StartedAt) {
		addCaveat(result, CounterReset)
		return
	}
	if swap != nil && swap.UsageBytes != nil && previous.Stats.Swap != nil && previous.Stats.Swap.UsageBytes != nil &&
		validWindow(previous.Stats.Swap.CapturedAt, swap.CapturedAt, input.Now) {
		growth := uint64(0)
		if *swap.UsageBytes > *previous.Stats.Swap.UsageBytes {
			growth = *swap.UsageBytes - *previous.Stats.Swap.UsageBytes
		}
		result.Facts.GrowingSwapBytes = &growth
		result.Facts.SwapGrowthFormula = "max(0, current swap.usage - previous swap.usage)"
		result.Facts.SwapWindowStartedAt = previous.Stats.Swap.CapturedAt
	}
	memory, old := current.Stats.Memory, previous.Stats.Memory
	if memory == nil || old == nil || memory.MajorPageFaults == nil || old.MajorPageFaults == nil {
		return
	}
	if !validWindow(old.CapturedAt, memory.CapturedAt, input.Now) || *memory.MajorPageFaults < *old.MajorPageFaults {
		addCaveat(result, CounterReset)
		return
	}
	rate := float64(*memory.MajorPageFaults-*old.MajorPageFaults) / memory.CapturedAt.Sub(old.CapturedAt).Seconds()
	result.Facts.MajorFaultsPerSecond = &rate
	result.Facts.MajorFaultFormula = "(current majorPageFaults - previous majorPageFaults) / elapsedSeconds"
	result.Facts.FaultWindowStartedAt = old.CapturedAt
}

func validWindow(start, end, now time.Time) bool {
	return fresh(end, now, nodecontext.StaleAfter) && !start.IsZero() && end.After(start) && end.Sub(start) <= 2*time.Minute
}
