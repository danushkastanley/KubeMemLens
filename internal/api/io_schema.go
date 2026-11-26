package api

import (
	"slices"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

const IOPressureSnapshotSchemaVersion = 6

func containerForSchema(container ContainerSnapshot, version int) ContainerSnapshot {
	if version == LegacySchemaVersion {
		container.Context.Resources = model.ContainerMemoryResources{}
	}
	if version < IOPressureSnapshotSchemaVersion {
		container.Memory.IOPressure = model.IOPressure{}
	}
	if version < PodCgroupSnapshotSchemaVersion {
		container.PodCgroup = model.PodCgroupMemory{}
		container.Memory = memoryWithoutSchema8Evidence(container.Memory)
	}
	return container
}

func podForSchema(pod PodSnapshot, version int) PodSnapshot {
	if version == LegacySchemaVersion {
		pod.Context.Resources = model.PodMemoryResources{}
	}
	if version < IOPressureSnapshotSchemaVersion {
		pod.Memory.IOPressure = model.IOPressure{}
	}
	if version < PodCgroupSnapshotSchemaVersion {
		pod.PodCgroup = model.PodCgroupMemory{}
		pod.Memory = memoryWithoutSchema8Evidence(pod.Memory)
		pod.Containers = containersForSchema(pod.Containers, version)
	}
	return pod
}

// WithoutIOIncident keeps existing incident schemas 1/2 readable by their
// original strict readers. Volume schema 5 owns the new enrichment domain.
// Schema 8 evidence is dropped first, with its own caveat, because no
// incident schema keeps it.
func WithoutIOIncident(bundle IncidentBundle) IncidentBundle {
	bundle = withoutPodCgroupIncident(bundle)
	omitted := false
	for _, pod := range bundle.Pods {
		omitted = omitted || pod.Memory.IOPressure.State != ""
		for _, container := range pod.Containers {
			omitted = omitted || container.Memory.IOPressure.State != ""
		}
	}
	if omitted {
		bundle.Partial = true
		bundle.Caveats = append(slices.Clone(bundle.Caveats), "Cgroup I/O pressure was omitted for incident schema 1/2; use a volume incident to retain it.")
		bundle.Pods = podsForSchema(bundle.Pods, VolumeHealthSnapshotSchemaVersion)
	}
	return bundle
}
