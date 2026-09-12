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
	return container
}

func podForSchema(pod PodSnapshot, version int) PodSnapshot {
	if version == LegacySchemaVersion {
		pod.Context.Resources = model.PodMemoryResources{}
	}
	if version < IOPressureSnapshotSchemaVersion {
		pod.Memory.IOPressure = model.IOPressure{}
		pod.Containers = containersForSchema(pod.Containers, version)
	}
	return pod
}

// WithoutIOIncident keeps existing incident schemas 1/2 readable by their
// original strict readers. Volume schema 5 owns the new enrichment domain.
func WithoutIOIncident(bundle IncidentBundle) IncidentBundle {
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
