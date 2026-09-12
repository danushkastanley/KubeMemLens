package nodeanalysis

import (
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func estimates(input Input, result *Analysis) {
	reasons := []Caveat{}
	current := input.Current
	switch {
	case current == nil || current.NodeUID != input.NodeUID || current.Stats == nil || current.Stats.Memory == nil || current.Stats.Memory.UsageBytes == nil:
		reasons = append(reasons, SourceMissing)
	case input.SourceAvailability != capability.Available:
		reasons = append(reasons, SourceFailed)
	case !fresh(current.Stats.Memory.CapturedAt, input.Now, nodecontext.StaleAfter):
		reasons = append(reasons, SourceStale)
	}
	if result.Coverage == nil || result.Coverage.State != Complete || result.ObservedPodCharge == nil {
		reasons = append(reasons, AgentPartial)
	}
	if current != nil && current.Stats != nil && current.Stats.Memory != nil && !aligned(current.Stats.Memory.CapturedAt, input.Cgroup.CapturedAt) {
		reasons = append(reasons, Skewed)
	}
	if !qualified(input) {
		reasons = append(reasons, Unqualified)
	}
	if len(reasons) > 0 {
		result.OutsidePods.Caveats = reasons
		result.Unaccounted.Caveats = append([]Caveat(nil), reasons...)
		for _, reason := range reasons {
			addCaveat(result, reason)
		}
		return
	}
	memory := current.Stats.Memory
	result.OutsidePods = calculated(result.OutsidePods, input, *memory.UsageBytes, *result.ObservedPodCharge, result)
	q := input.Qualification
	if len(q.DisjointSystems) == 0 {
		result.Unaccounted.Caveats = []Caveat{SystemOverlap}
		return
	}
	systemUsage := uint64(0)
	for _, category := range q.DisjointSystems {
		system := findSystem(current.Stats.SystemContainers, category)
		if system == nil || system.Memory == nil || system.Memory.UsageBytes == nil || !fresh(system.Memory.CapturedAt, input.Now, nodecontext.StaleAfter) ||
			!aligned(system.Memory.CapturedAt, memory.CapturedAt) || !aligned(system.Memory.CapturedAt, input.Cgroup.CapturedAt) {
			result.Unaccounted.Caveats = []Caveat{SystemOverlap, Skewed}
			return
		}
		sum, ok := add(systemUsage, *system.Memory.UsageBytes)
		if !ok {
			result.Unaccounted.Caveats = []Caveat{Overflow}
			addCaveat(result, Overflow)
			result.Confidence = Low
			return
		}
		systemUsage = sum
	}
	included, ok := add(*result.ObservedPodCharge, systemUsage)
	if !ok {
		result.Unaccounted.Caveats = []Caveat{Overflow}
		addCaveat(result, Overflow)
		result.Confidence = Low
		return
	}
	result.Unaccounted = calculated(result.Unaccounted, input, *memory.UsageBytes, included, result)
}

func calculated(estimate Estimate, input Input, total, included uint64, result *Analysis) Estimate {
	value := uint64(0)
	if total >= included {
		value = total - included
	} else {
		estimate.Caveats = []Caveat{NegativeGap, Skewed}
		addCaveat(result, NegativeGap)
		addCaveat(result, Skewed)
		result.Confidence = Low
	}
	estimate.State, estimate.Bytes = capability.Available, &value
	estimate.CapturedAt = input.Current.Stats.Memory.CapturedAt
	estimate.ComparedAt = input.Cgroup.CapturedAt
	estimate.Qualification = input.Qualification.EvidenceSHA256
	return estimate
}

func findSystem(values []nodecontext.SystemContainer, category nodecontext.SystemCategory) *nodecontext.SystemContainer {
	for i := range values {
		if values[i].Category == category {
			return &values[i]
		}
	}
	return nil
}
