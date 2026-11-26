package collector

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// validatePodCgroupEvidence rejects schema 8 values below schema 8, values
// that claim a number for something unknown, a Pod cgroup on a container that
// names no Pod, and containers of one Pod that disagree about its cgroup.
func validatePodCgroupEvidence(snapshot api.AgentSnapshot) error {
	for i, container := range snapshot.Containers {
		if snapshot.SchemaVersion < api.PodCgroupSnapshotSchemaVersion {
			if field := schema8Field(container); field != "" {
				return fmt.Errorf("containers[%d].%s requires snapshot schema 8", i, field)
			}
		}
		if err := api.ValidateContainerSchema8Evidence(container); err != nil {
			return fmt.Errorf("containers[%d]: %w", i, err)
		}
		if container.PodCgroup != (model.PodCgroupMemory{}) && container.PodUID == "" {
			return fmt.Errorf("containers[%d].podCgroup requires a Pod UID", i)
		}
	}
	return validatePodCgroupAgreement(snapshot.Containers)
}

func schema8Field(container api.ContainerSnapshot) string {
	switch {
	case container.PodCgroup != (model.PodCgroupMemory{}):
		return "podCgroup"
	case container.Memory.Zswap != (model.Zswap{}):
		return "memory.zswap"
	case container.Memory.OOMGroupKill != (model.OOMGroupKill{}):
		return "memory.oomGroupKill"
	default:
		return ""
	}
}

// validatePodCgroupAgreement holds the Pod cgroup to the rule Pod resources
// follow: every container of one Pod that carries it carries the same value.
func validatePodCgroupAgreement(containers []api.ContainerSnapshot) error {
	observed := map[podResourceKey]model.PodCgroupMemory{}
	for _, container := range containers {
		if container.PodCgroup == (model.PodCgroupMemory{}) {
			continue
		}
		key := podResourceKey{container.Namespace, container.PodName, container.PodUID}
		if previous, found := observed[key]; found && previous != container.PodCgroup {
			return fmt.Errorf("Pod cgroup differs between containers in the same snapshot")
		}
		observed[key] = container.PodCgroup
	}
	return nil
}

// withPodCgroupDeltas sets the Pod cgroup's local event deltas from the
// previous stored snapshot of the same node, matched by Pod identity, and
// applies one result to every container of the Pod so they still agree. A Pod
// not in the previous snapshot is a first sample. Every delta is overwritten.
func withPodCgroupDeltas(current, previous []api.ContainerSnapshot) {
	before := map[podResourceKey]model.PodCgroupMemory{}
	for _, container := range previous {
		key := podResourceKey{container.Namespace, container.PodName, container.PodUID}
		if _, found := before[key]; found || container.PodCgroup == (model.PodCgroupMemory{}) {
			continue
		}
		before[key] = container.PodCgroup
	}
	after := map[podResourceKey]model.PodCgroupMemory{}
	for index := range current {
		container := &current[index]
		if container.PodCgroup == (model.PodCgroupMemory{}) {
			continue
		}
		key := podResourceKey{container.Namespace, container.PodName, container.PodUID}
		value, computed := after[key]
		if !computed {
			last, hasPrevious := before[key]
			value = model.WithPodCgroupDeltas(container.PodCgroup, last, hasPrevious)
			after[key] = value
		}
		container.PodCgroup = value
	}
}
