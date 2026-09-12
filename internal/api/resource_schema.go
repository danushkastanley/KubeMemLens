package api

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

const LegacySchemaVersion = 1

// Callers advertise their highest understood snapshot schema. An absent header
// retains the representation understood by existing strict JSON clients.
const SnapshotSchemaHeader = "X-KubeMemLens-Snapshot-Schema"

func NegotiateSnapshotSchema(value string) (int, error) {
	if value == "" {
		return LegacySchemaVersion, nil
	}
	version, err := strconv.ParseUint(value, 10, 16)
	if err != nil || version < LegacySchemaVersion {
		return 0, fmt.Errorf("snapshot schema must be a positive version number")
	}
	return min(int(version), CurrentSnapshotSchemaVersion), nil
}

func SupportedSnapshotSchema(version int) bool {
	return version >= LegacySchemaVersion && version <= CurrentSnapshotSchemaVersion
}

func AgentSnapshotForSchema(snapshot AgentSnapshot, version int) AgentSnapshot {
	snapshot.SchemaVersion = version
	if version < VolumeSnapshotSchemaVersion {
		snapshot.VolumeBatch = nil
	}
	if version < 3 {
		snapshot.NodeContext = nil
	}
	if version < IOPressureSnapshotSchemaVersion {
		snapshot.Containers = containersForSchema(snapshot.Containers, version)
	}
	return snapshot
}

func LegacyPodSnapshot(pod PodSnapshot) PodSnapshot {
	return podForSchema(pod, LegacySchemaVersion)
}

func containersForSchema(containers []ContainerSnapshot, version int) []ContainerSnapshot {
	items := slices.Clone(containers)
	for index := range items {
		items[index] = containerForSchema(items[index], version)
	}
	return items
}

func legacyPods(pods []PodSnapshot) []PodSnapshot {
	return podsForSchema(pods, LegacySchemaVersion)
}

func podsForSchema(pods []PodSnapshot, version int) []PodSnapshot {
	items := slices.Clone(pods)
	for index := range items {
		items[index] = podForSchema(items[index], version)
	}
	return items
}

func workloadForSchema(workload WorkloadSnapshot, version int) WorkloadSnapshot {
	workload.Pods = podsForSchema(workload.Pods, version)
	workload.Memory.IOPressure = model.IOPressure{}
	return workload
}

// SnapshotView projects only the existing read representations containing
// resource context. It preserves identity, pagination, memory evidence and
// source ownership. The caller must negotiate version before reading data.
func SnapshotView(value any, version int) any {
	if version < 3 {
		switch data := value.(type) {
		case DebugStore:
			data.NodeContext = nil
			value = data
		case ClusterStatus:
			data.Store.NodeContext = nil
			value = data
		}
	}
	if version >= IOPressureSnapshotSchemaVersion {
		return value
	}
	switch data := value.(type) {
	case ContainerPage:
		data.Items = containersForSchema(data.Items, version)
		return data
	case []ContainerSnapshot:
		return containersForSchema(data, version)
	case []PodSnapshot:
		return podsForSchema(data, version)
	case PodSnapshot:
		return podForSchema(data, version)
	case []WorkloadSnapshot:
		items := slices.Clone(data)
		for index := range items {
			items[index] = workloadForSchema(items[index], version)
		}
		return items
	case PodMemory:
		data.Snapshot = podForSchema(data.Snapshot, version)
		return data
	case PodMemoryList:
		data.Items = slices.Clone(data.Items)
		for index := range data.Items {
			data.Items[index].Snapshot = podForSchema(data.Items[index].Snapshot, version)
		}
		return data
	case ContainerMemory:
		data.Snapshot = containerForSchema(data.Snapshot, version)
		return data
	case ContainerMemoryList:
		data.Items = slices.Clone(data.Items)
		for index := range data.Items {
			data.Items[index].Snapshot = containerForSchema(data.Items[index].Snapshot, version)
		}
		return data
	case WorkloadMemory:
		data.Snapshot = workloadForSchema(data.Snapshot, version)
		return data
	case WorkloadMemoryList:
		data.Items = slices.Clone(data.Items)
		for index := range data.Items {
			data.Items[index].Snapshot = workloadForSchema(data.Items[index].Snapshot, version)
		}
		return data
	default:
		return value
	}
}

func IncidentSchema(pods []PodSnapshot) int {
	for _, pod := range pods {
		if PodHasResourceContext(pod) {
			return CurrentIncidentSchemaVersion
		}
	}
	return LegacySchemaVersion
}

func PodHasResourceContext(pod PodSnapshot) bool {
	if !pod.Context.Resources.IsZero() {
		return true
	}
	for _, container := range pod.Containers {
		if !container.Context.Resources.IsZero() {
			return true
		}
	}
	return false
}

func LegacyIncident(bundle IncidentBundle) IncidentBundle {
	bundle = WithoutIOIncident(bundle)
	if IncidentSchema(bundle.Pods) != LegacySchemaVersion {
		bundle.Partial = true
		bundle.Caveats = append(slices.Clone(bundle.Caveats), "Pod resource and resize context was omitted for incident schema 1.")
	}
	bundle.SchemaVersion = LegacySchemaVersion
	bundle.Pods = legacyPods(bundle.Pods)
	return bundle
}
