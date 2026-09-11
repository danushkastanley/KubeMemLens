package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

// Each measurement has its own clock. The oldest field can change when an
// optional field first appears, so it cannot order whole observations.
type nodeContextClock [16]time.Time

func observationClock(value nodecontext.Observation) nodeContextClock {
	var clock nodeContextClock
	if value.Context != nil {
		clock[10] = value.Context.CapturedAt
	}
	if value.Stats == nil {
		return clock
	}
	stats := value.Stats
	clock[11] = stats.StartedAt
	if stats.Memory != nil {
		clock[0] = stats.Memory.CapturedAt
	}
	if stats.Swap != nil {
		clock[1] = stats.Swap.CapturedAt
	}
	for _, item := range stats.SystemContainers {
		index := 0
		switch item.Category {
		case nodecontext.Kubelet:
			index = 0
		case nodecontext.Runtime:
			index = 1
		case nodecontext.Misc:
			index = 2
		case nodecontext.Pods:
			index = 3
		}
		if item.Memory != nil {
			clock[2+index*2] = item.Memory.CapturedAt
		}
		if item.Swap != nil {
			clock[3+index*2] = item.Swap.CapturedAt
		}
		clock[12+index] = item.StartedAt
	}
	return clock
}

func (next nodeContextClock) before(previous nodeContextClock) bool {
	for i, at := range next {
		if !at.IsZero() && at.Before(previous[i]) {
			return true
		}
	}
	return false
}

func (next nodeContextClock) newMeasurements(previous nodeContextClock) bool {
	for i := 0; i < 10; i++ {
		if next[i].After(previous[i]) {
			return true
		}
	}
	return false
}

func (next nodeContextClock) retainingMissing(previous nodeContextClock) nodeContextClock {
	for i, at := range next {
		if at.IsZero() {
			next[i] = previous[i]
		}
	}
	return next
}
