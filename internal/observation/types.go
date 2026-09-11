// Package observation holds caller-scoped application queries. Its types are
// separate from agent ingestion and collector wire snapshots.
package observation

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type Reader interface {
	Current(context.Context) (Batch, error)
}

type SourceReport struct {
	capability.SourceState
	Scope capability.Scope `json:"scope"`
}

type Coverage struct {
	Reported int              `json:"reported"`
	Expected int              `json:"expected"`
	Unit     capability.Scope `json:"unit"`
}

// Bytes is nil for unavailable evidence. A non-nil zero is a measured zero.
// Rollups retain their oldest and newest sample instants and coverage.
type WorkingSet struct {
	Bytes          *uint64                 `json:"bytes"`
	Availability   capability.Availability `json:"availability"`
	Reason         capability.Reason       `json:"reason,omitempty"`
	Evidence       capability.Envelope     `json:"evidence"`
	LatestSampleAt time.Time               `json:"latestSampleAt,omitzero"`
	Coverage       Coverage                `json:"coverage"`
}

type Cgroup struct {
	Memory           model.MemoryBreakdown `json:"memory"`
	Evidence         capability.Envelope   `json:"evidence"`
	ContainerID      string                `json:"-"`
	CgroupPath       string                `json:"-"`
	DeltaStartedAt   time.Time             `json:"deltaStartedAt,omitzero"`
	DeltaWindowKnown bool                  `json:"deltaWindowKnown"`
}

type Hugepages struct {
	Resource     string  `json:"resource"`
	RequestBytes *uint64 `json:"requestBytes,omitempty"`
	LimitBytes   *uint64 `json:"limitBytes,omitempty"`
}

type Container struct {
	Name        string               `json:"name"`
	Kind        string               `json:"kind"`
	State       string               `json:"state"`
	StateReason string               `json:"stateReason,omitempty"`
	ExitCode    *int32               `json:"exitCode,omitempty"`
	StartedAt   time.Time            `json:"startedAt,omitzero"`
	Context     api.ContainerContext `json:"context"`
	Hugepages   []Hugepages          `json:"hugepages,omitempty"`
	WorkingSet  WorkingSet           `json:"workingSet"`
	Cgroup      *Cgroup              `json:"cgroup,omitempty"`
}

type Pod struct {
	Namespace         string                  `json:"namespace"`
	Name              string                  `json:"name"`
	UID               string                  `json:"uid,omitempty"`
	NodeName          string                  `json:"nodeName,omitempty"`
	Context           api.PodContext          `json:"context"`
	StatusEvidence    capability.Envelope     `json:"statusEvidence"`
	OwnerAvailability capability.Availability `json:"ownerAvailability"`
	OwnerEvidence     capability.Envelope     `json:"ownerEvidence"`
	OwnerReason       capability.Reason       `json:"ownerReason,omitempty"`
	Containers        []Container             `json:"containers"`
	Hugepages         []Hugepages             `json:"hugepages,omitempty"`
	WorkingSet        WorkingSet              `json:"workingSet"`
	Cgroup            *Cgroup                 `json:"cgroup,omitempty"`
}

type Node struct {
	Name                   string                  `json:"name"`
	UID                    string                  `json:"uid,omitempty"`
	CreatedAt              time.Time               `json:"createdAt,omitzero"`
	StatusEvidence         capability.Envelope     `json:"statusEvidence"`
	StatusAvailability     capability.Availability `json:"statusAvailability"`
	StatusReason           capability.Reason       `json:"statusReason,omitempty"`
	CapacityMemoryBytes    *uint64                 `json:"capacityMemoryBytes"`
	AllocatableMemoryBytes *uint64                 `json:"allocatableMemoryBytes"`
	MemoryPressure         string                  `json:"memoryPressure"`
	HugepageCapacity       map[string]uint64       `json:"hugepageCapacity,omitempty"`
	HugepageAllocatable    map[string]uint64       `json:"hugepageAllocatable,omitempty"`
	WorkingSet             WorkingSet              `json:"workingSet"`
	// DeepStatus preserves the existing observed-Pod/agent node contract. It
	// is not full Node memory and must never populate WorkingSet.
	DeepStatus *api.NodeSnapshotStatus `json:"deepStatus,omitempty"`
}

type Group struct {
	Namespace  string     `json:"namespace,omitempty"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	PodCount   int        `json:"podCount"`
	WorkingSet WorkingSet `json:"workingSet"`
	Cgroup     *Cgroup    `json:"cgroup,omitempty"`
}

type Batch struct {
	Mode         capability.Mode         `json:"mode"`
	ReceivedAt   time.Time               `json:"receivedAt"`
	Completeness capability.Completeness `json:"completeness"`
	Sources      []SourceReport          `json:"sources"`
	Pods         []Pod                   `json:"pods"`
	Nodes        []Node                  `json:"nodes"`
	Namespaces   []Group                 `json:"namespaces"`
	Workloads    []Group                 `json:"workloads"`
	Caveats      []string                `json:"caveats,omitempty"`
}
