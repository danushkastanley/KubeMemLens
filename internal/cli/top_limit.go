package cli

import "github.com/danushkastanley/kube-memlens/internal/model"

// limitState says what is known about a memory limit. A bare byte count
// cannot: zero would mean both "no limit" and "limit not read".
type limitState string

const (
	limitUnknown   limitState = "unknown"
	limitUnlimited limitState = "unlimited"
	limitFinite    limitState = "finite"
)

// memoryLimit returns the limit in bytes, which is zero unless the state is finite.
func memoryLimit(memory model.MemoryBreakdown) (uint64, limitState) {
	if !memory.MaxKnown {
		return 0, limitUnknown
	}
	if memory.MaxUnlimited {
		return 0, limitUnlimited
	}
	return memory.MaxBytes, limitFinite
}
