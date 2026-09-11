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
	if version < 3 {
		snapshot.NodeContext = nil
	}
	if version == LegacySchemaVersion {
		snapshot.Containers = legacyContainers(snapshot.Containers)
	}
	return snapshot
}

func LegacyPodSnapshot(pod PodSnapshot) PodSnapshot {
	pod.Context.Resources = model.PodMemoryResources{}
	pod.Containers = legacyContainers(pod.Containers)
	return pod
}

func legacyContainers(containers []ContainerSnapshot) []ContainerSnapshot {
	items := slices.Clone(containers)
	for index := range items {
		items[index].Context.Resources = model.ContainerMemoryResources{}
	}
	return items
}

func legacyPods(pods []PodSnapshot) []PodSnapshot {
	items := slices.Clone(pods)
	for index := range items {
		items[index] = LegacyPodSnapshot(items[index])
	}
	return items
}

func legacyWorkload(workload WorkloadSnapshot) WorkloadSnapshot {
	workload.Pods = legacyPods(workload.Pods)
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
	if version != LegacySchemaVersion {
		return value
	}
	switch data := value.(type) {
	case ContainerPage:
		data.Items = legacyContainers(data.Items)
		return data
	case []ContainerSnapshot:
		return legacyContainers(data)
	case []PodSnapshot:
		return legacyPods(data)
	case PodSnapshot:
		return LegacyPodSnapshot(data)
	case []WorkloadSnapshot:
		items := slices.Clone(data)
		for index := range items {
			items[index] = legacyWorkload(items[index])
		}
		return items
	case PodMemory:
		data.Snapshot = LegacyPodSnapshot(data.Snapshot)
		return data
	case PodMemoryList:
		data.Items = slices.Clone(data.Items)
		for index := range data.Items {
			data.Items[index].Snapshot = LegacyPodSnapshot(data.Items[index].Snapshot)
		}
		return data
	case ContainerMemory:
		data.Snapshot.Context.Resources = model.ContainerMemoryResources{}
		return data
	case ContainerMemoryList:
		data.Items = slices.Clone(data.Items)
		for index := range data.Items {
			data.Items[index].Snapshot.Context.Resources = model.ContainerMemoryResources{}
		}
		return data
	case WorkloadMemory:
		data.Snapshot = legacyWorkload(data.Snapshot)
		return data
	case WorkloadMemoryList:
		data.Items = slices.Clone(data.Items)
		for index := range data.Items {
			data.Items[index].Snapshot = legacyWorkload(data.Items[index].Snapshot)
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
	if IncidentSchema(bundle.Pods) != LegacySchemaVersion {
		bundle.Partial = true
		bundle.Caveats = append(slices.Clone(bundle.Caveats), "Pod resource and resize context was omitted for incident schema 1.")
	}
	bundle.SchemaVersion = LegacySchemaVersion
	bundle.Pods = legacyPods(bundle.Pods)
	return bundle
}
