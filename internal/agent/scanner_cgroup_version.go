package agent

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/cgroup"
	"github.com/danushkastanley/kube-memlens/internal/kube"
)

// cgroupV1Scan is the result for a node whose memory controller is on cgroup
// v1, including a hybrid node. The walk reads only cgroup v2 memory files, so
// on v1 every scan would fail and the node would look missing. Instead the
// node is published with no containers and cgroupVersion v1, so the collector,
// doctor and status can say why it has no evidence.
func cgroupV1Scan(nodeName string, capturedAt time.Time, idx kube.PodIndex) ScanResult {
	return ScanResult{
		Snapshot: api.AgentSnapshot{
			SchemaVersion: api.CurrentSnapshotSchemaVersion,
			NodeName:      nodeName,
			CapturedAt:    capturedAt,
			Environment: api.NodeEnvironment{
				CgroupVersion:            cgroup.VersionV1,
				ContainerRuntimes:        []string{"unknown"},
				NodeContextAvailable:     idx.NodeContext.Available,
				MemoryPressureStatus:     idx.NodeContext.MemoryPressureStatus,
				MemoryPressureSince:      idx.NodeContext.MemoryPressureSince,
				MemoryAllocatableBytes:   idx.NodeContext.MemoryAllocatableBytes,
				MemoryAllocatableKnown:   idx.NodeContext.MemoryAllocatableKnown,
				WorkloadContextAvailable: idx.WorkloadContextAvailable,
				WorkloadContextErrors:    idx.WorkloadContextErrors,
			},
		},
	}
}
