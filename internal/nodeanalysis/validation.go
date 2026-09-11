package nodeanalysis

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func validateInput(input Input) error {
	for _, node := range []*nodecontext.Observation{input.Current, input.Previous} {
		if node == nil {
			continue
		}
		if len(node.NodeName) > nodecontext.MaxNodeNameBytes || len(node.NodeUID) > nodecontext.MaxNodeUIDBytes {
			return fmt.Errorf("Node identity exceeds analysis bounds")
		}
		if node.Context != nil && len(node.Context.Hugepages) > nodecontext.MaxHugepages {
			return fmt.Errorf("hugepage context exceeds analysis bounds")
		}
		if node.Stats == nil {
			continue
		}
		if len(node.Stats.SystemContainers) > nodecontext.MaxSystemContainers {
			return fmt.Errorf("system context exceeds analysis bounds")
		}
		memories := []*nodecontext.Memory{node.Stats.Memory}
		for _, system := range node.Stats.SystemContainers {
			memories = append(memories, system.Memory)
		}
		for _, memory := range memories {
			if memory == nil || memory.PSI == nil {
				continue
			}
			for _, psi := range []nodecontext.PSIData{memory.PSI.Some, memory.PSI.Full} {
				if !validPercent(psi.Avg10) || !validPercent(psi.Avg60) || !validPercent(psi.Avg300) {
					return fmt.Errorf("invalid Node pressure percentage")
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, container := range input.Cgroup.Containers {
		if len(container.ID) > 256 || len(container.Namespace) > 63 || len(container.PodName) > 253 || len(container.PodUID) > 128 || len(container.ContainerName) > 63 ||
			len(container.WorkloadName) > 253 || len(container.WorkloadKind) > 63 {
			return fmt.Errorf("contributor identity exceeds analysis bounds")
		}
		if container.ID != "" && seen[container.ID] {
			return fmt.Errorf("duplicate contributor container")
		}
		seen[container.ID] = true
	}
	return nil
}
