package api

import "github.com/danushkastanley/kube-memlens/internal/model"

// PodLimitSource names where a Pod-level limit was read.
type PodLimitSource string

const (
	// PodLimitPodCgroup is the Pod cgroup's own finite memory.max.
	PodLimitPodCgroup PodLimitSource = "pod-cgroup"
	// PodLimitContainerSum is the sum of the container limits, unlimited when
	// any container has none: the value used before schema 8.
	PodLimitContainerSum PodLimitSource = "container-sum"
)

// PodLimit is the one Pod-level limit every Pod consumer uses. Known false
// means no limit was read; Unlimited means one was read and it is "max".
// Neither is a zero limit.
type PodLimit struct {
	Source     PodLimitSource
	Known      bool
	Unlimited  bool
	LimitBytes uint64
	UsedBytes  uint64
}

// EffectiveLimit returns the Pod cgroup's own memory.max when it is finite,
// with usage from the Pod cgroup's memory.current (which includes the sandbox)
// when that is known. Otherwise it returns exactly the container-sum values in
// p.Memory. The Kubernetes-API budget in p.Context.Resources is separate
// evidence and is never used here.
func (p PodSnapshot) EffectiveLimit() PodLimit {
	cgroup := p.PodCgroup
	if !cgroup.MaxKnown || cgroup.MaxUnlimited || cgroup.MaxBytes == 0 {
		return PodLimit{
			Source:     PodLimitContainerSum,
			Known:      p.Memory.MaxKnown,
			Unlimited:  p.Memory.MaxUnlimited,
			LimitBytes: p.Memory.MaxBytes,
			UsedBytes:  p.Memory.TotalBytes,
		}
	}
	used := p.Memory.TotalBytes
	if cgroup.CurrentKnown {
		used = cgroup.CurrentBytes
	}
	return PodLimit{Source: PodLimitPodCgroup, Known: true, LimitBytes: cgroup.MaxBytes, UsedBytes: used}
}

// UsageRatio is UsedBytes over LimitBytes, and zero when the limit is unknown,
// unlimited or zero, as MemoryBreakdown.LimitUsageRatio is.
func (l PodLimit) UsageRatio() float64 {
	return l.asMemory().LimitUsageRatio()
}

// AtRisk uses the same 90% threshold as MemoryBreakdown.HasLimitRisk.
func (l PodLimit) AtRisk() bool {
	return l.asMemory().HasLimitRisk()
}

// asMemory lets the ratio and risk rules stay defined in one place.
func (l PodLimit) asMemory() model.MemoryBreakdown {
	return model.MemoryBreakdown{TotalBytes: l.UsedBytes, MaxBytes: l.LimitBytes, MaxKnown: l.Known, MaxUnlimited: l.Unlimited}
}
