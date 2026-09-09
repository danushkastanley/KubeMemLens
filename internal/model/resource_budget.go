package model

type ResourceBudgetSource string

const (
	BudgetUnreported ResourceBudgetSource = "unreported"
	BudgetUnset      ResourceBudgetSource = "unset"
	BudgetPod        ResourceBudgetSource = "pod-spec"
	BudgetContainers ResourceBudgetSource = "container-sum"
	BudgetPartial    ResourceBudgetSource = "partial-container-sum"
)

type EffectiveResourceValue struct {
	Bytes  uint64               `json:"bytes"`
	Source ResourceBudgetSource `json:"source"`
}

type EffectiveMemoryResources struct {
	Request EffectiveResourceValue `json:"request"`
	Limit   EffectiveResourceValue `json:"limit"`
}

type ContainerResourceTotals struct {
	RequestBytes      uint64
	LimitBytes        uint64
	RequestContainers int
	LimitContainers   int
	Containers        int
}

// EffectivePodMemory selects the configured Pod budget independently for each
// resource. Container totals remain available to callers as separate evidence;
// they are never added to a Pod budget or presented as cgroup enforcement.
func EffectivePodMemory(pod PodMemoryResources, containers ContainerResourceTotals) EffectiveMemoryResources {
	return EffectiveMemoryResources{
		Request: selectResourceBudget(pod.Configured.Request, containers.RequestBytes, containers.RequestContainers, containers.Containers),
		Limit:   selectResourceBudget(pod.Configured.Limit, containers.LimitBytes, containers.LimitContainers, containers.Containers),
	}
}

func selectResourceBudget(pod ResourceValue, total uint64, covered, containers int) EffectiveResourceValue {
	if pod.Known {
		return EffectiveResourceValue{Bytes: pod.Bytes, Source: BudgetPod}
	}
	if containers == 0 {
		return EffectiveResourceValue{Source: BudgetUnreported}
	}
	if covered == 0 {
		return EffectiveResourceValue{Source: BudgetUnset}
	}
	if covered < containers {
		return EffectiveResourceValue{Bytes: total, Source: BudgetPartial}
	}
	return EffectiveResourceValue{Bytes: total, Source: BudgetContainers}
}
