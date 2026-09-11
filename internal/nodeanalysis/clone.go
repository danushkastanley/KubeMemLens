package nodeanalysis

import "github.com/danushkastanley/kube-memlens/internal/nodecontext"

func copyUint(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func copyFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneMemory(value *nodecontext.Memory) *nodecontext.Memory {
	if value == nil {
		return nil
	}
	result := *value
	result.AvailableBytes = copyUint(value.AvailableBytes)
	result.UsageBytes = copyUint(value.UsageBytes)
	result.WorkingSetBytes = copyUint(value.WorkingSetBytes)
	result.RSSBytes = copyUint(value.RSSBytes)
	result.PageFaults = copyUint(value.PageFaults)
	result.MajorPageFaults = copyUint(value.MajorPageFaults)
	if value.PSI != nil {
		psi := *value.PSI
		result.PSI = &psi
	}
	return &result
}

func cloneSwap(value *nodecontext.Swap) *nodecontext.Swap {
	if value == nil {
		return nil
	}
	result := *value
	result.UsageBytes = copyUint(value.UsageBytes)
	result.AvailableBytes = copyUint(value.AvailableBytes)
	return &result
}

func cloneContext(value *nodecontext.KubernetesContext) *nodecontext.KubernetesContext {
	if value == nil {
		return nil
	}
	result := *value
	result.CapacityBytes = copyUint(value.CapacityBytes)
	result.AllocatableBytes = copyUint(value.AllocatableBytes)
	result.Hugepages = make([]nodecontext.Hugepage, len(value.Hugepages))
	for i, page := range value.Hugepages {
		result.Hugepages[i] = nodecontext.Hugepage{Resource: page.Resource, CapacityBytes: copyUint(page.CapacityBytes), AllocatableBytes: copyUint(page.AllocatableBytes)}
	}
	return &result
}

func cloneSystems(values []nodecontext.SystemContainer) []nodecontext.SystemContainer {
	result := make([]nodecontext.SystemContainer, len(values))
	for i, value := range values {
		result[i] = nodecontext.SystemContainer{Category: value.Category, StartedAt: value.StartedAt, Memory: cloneMemory(value.Memory), Swap: cloneSwap(value.Swap)}
	}
	return result
}
