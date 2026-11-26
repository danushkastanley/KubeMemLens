package api

import (
	"slices"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

// PodCgroupSnapshotSchemaVersion adds the Pod's own cgroup (PodCgroup on
// containers and Pods) and the zswap and oom_group_kill values of
// MemoryBreakdown. Every older schema projects all three out.
const PodCgroupSnapshotSchemaVersion = 8

// PodCgroupIncidentCaveat is recorded when an incident file of schema 1 or 2
// drops this evidence.
const PodCgroupIncidentCaveat = "Pod cgroup, zswap and oom_group_kill evidence was omitted; incident files do not keep it."

// ContainerHasSchema8Evidence reports whether a container carries anything an
// older schema cannot represent.
func ContainerHasSchema8Evidence(container ContainerSnapshot) bool {
	return container.PodCgroup != (model.PodCgroupMemory{}) || memoryHasSchema8Evidence(container.Memory)
}

// PodHasSchema8Evidence reports whether a Pod or any of its containers carries
// anything an older schema cannot represent.
func PodHasSchema8Evidence(pod PodSnapshot) bool {
	if pod.PodCgroup != (model.PodCgroupMemory{}) || memoryHasSchema8Evidence(pod.Memory) {
		return true
	}
	return slices.ContainsFunc(pod.Containers, ContainerHasSchema8Evidence)
}

// PodWithoutSchema8Evidence returns a copy of pod as schema 7 represents it.
// The input, including its container slice, is not modified.
func PodWithoutSchema8Evidence(pod PodSnapshot) PodSnapshot {
	return podForSchema(pod, TopologySnapshotSchemaVersion)
}

func memoryHasSchema8Evidence(memory model.MemoryBreakdown) bool {
	return memory.Zswap != (model.Zswap{}) || memory.OOMGroupKill != (model.OOMGroupKill{})
}

func memoryWithoutSchema8Evidence(memory model.MemoryBreakdown) model.MemoryBreakdown {
	memory.Zswap = model.Zswap{}
	memory.OOMGroupKill = model.OOMGroupKill{}
	return memory
}

func namespaceForSchema(namespace NamespaceSnapshot, version int) NamespaceSnapshot {
	if version < PodCgroupSnapshotSchemaVersion {
		namespace.Memory = memoryWithoutSchema8Evidence(namespace.Memory)
	}
	return namespace
}

// withoutPodCgroupIncident keeps incident schemas 1 and 2 readable by their
// original strict readers. No incident schema keeps this evidence yet.
func withoutPodCgroupIncident(bundle IncidentBundle) IncidentBundle {
	if !slices.ContainsFunc(bundle.Pods, PodHasSchema8Evidence) {
		return bundle
	}
	bundle.Partial = true
	bundle.Caveats = append(slices.Clone(bundle.Caveats), PodCgroupIncidentCaveat)
	bundle.Pods = podsForSchema(bundle.Pods, TopologySnapshotSchemaVersion)
	return bundle
}

// ValidateContainerSchema8Evidence checks the schema 8 values a container carries:
// none of them may claim a number for something unknown.
func ValidateContainerSchema8Evidence(container ContainerSnapshot) error {
	if err := container.PodCgroup.Validate(); err != nil {
		return err
	}
	if err := container.Memory.Zswap.Validate(); err != nil {
		return err
	}
	return container.Memory.OOMGroupKill.Validate()
}
