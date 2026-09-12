// Package volumecontext joins bounded, authorised volume evidence. Filesystem
// usage and configuration are independent of cgroup memory accounting.
package volumecontext

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

const (
	SchemaVersion      = 1
	MaxVolumesPerPod   = 64
	MaxBatchRecords    = 1024
	MaxBatchBytes      = 1 << 20
	MaxPageRecords     = 100
	MaxPageBytes       = 256 << 10
	MaxRetainedBytes   = 64 << 20
	MaxNameBytes       = 253
	MaxUIDBytes        = 128
	MaxMountsPerVolume = 64
	MaxReasonBytes     = 256
	MaxStatusBytes     = 256
	StaleAfter         = 45 * time.Second
	ExpireAfter        = 2 * time.Minute
	FutureSkew         = 5 * time.Second
)

// PodScope is resolved from current Kubernetes objects after authorisation.
// Node UID and creation time prevent stale samples crossing object lifetimes.
type PodScope struct {
	Namespace string    `json:"-"`
	PodName   string    `json:"-"`
	PodUID    string    `json:"-"`
	CreatedAt time.Time `json:"-"`
	NodeName  string    `json:"-"`
	NodeUID   string    `json:"-"`
}

// Binding contains only metadata the caller may read. A PVC binding requires
// current UID/claim/ephemeral-owner verification by the Kubernetes adapter.
type Binding struct {
	VolumeName        string                    `json:"-"`
	PVCName           string                    `json:"-"`
	PVCUID            string                    `json:"-"`
	PVCCreatedAt      time.Time                 `json:"-"`
	Driver            string                    `json:"-"`
	ClaimAvailability volumehealth.Availability `json:"-"`
	Configuration     Configuration             `json:"configuration"`
}

type Kind string

const (
	PersistentClaim Kind = "persistent-claim"
	EphemeralClaim  Kind = "ephemeral-claim"
	InlineCSI       Kind = "inline-csi"
	EmptyDir        Kind = "empty-dir"
	Other           Kind = "other"
)

// Mount configuration omits paths and container names. Counts describe the
// authorised Pod spec, not measured usage or attribution to a process.
type Configuration struct {
	Kind               Kind    `json:"kind"`
	MemoryBacked       bool    `json:"memoryBacked"`
	SizeLimitBytes     *uint64 `json:"sizeLimitBytes,omitempty"`
	MountCount         int     `json:"mountCount"`
	ReadOnlyMountCount int     `json:"readOnlyMountCount"`
}

// Filesystem values are optional and overlap. Available plus used need not
// equal capacity. Neither these bytes nor a size limit may enter memory totals.
type Filesystem struct {
	CapturedAt     time.Time `json:"capturedAt"`
	CapacityBytes  *uint64   `json:"capacityBytes,omitempty"`
	UsedBytes      *uint64   `json:"usedBytes,omitempty"`
	AvailableBytes *uint64   `json:"availableBytes,omitempty"`
	Inodes         *uint64   `json:"inodes,omitempty"`
	InodesUsed     *uint64   `json:"inodesUsed,omitempty"`
	InodesFree     *uint64   `json:"inodesFree,omitempty"`
}

type Reason string

const (
	NoReport           Reason = "no-report"
	Disabled           Reason = "disabled"
	Unsupported        Reason = "unsupported"
	AccessDenied       Reason = "access-denied"
	SourceFailed       Reason = "source-failed"
	BindingUnavailable Reason = "binding-unavailable"
	Expired            Reason = "expired"
)

type Usage struct {
	Source       string                    `json:"source"`
	Availability volumehealth.Availability `json:"availability"`
	Reason       Reason                    `json:"reason,omitempty"`
	Freshness    volumehealth.Freshness    `json:"freshness"`
	Completeness capability.Completeness   `json:"completeness"`
	Filesystem   *Filesystem               `json:"filesystem,omitempty"`
}

// RawUsage is a private adapter observation. Summary has no PVC UID; joining
// requires a current Binding and a sample within that PVC's lifetime.
type RawUsage struct {
	Namespace    string     `json:"-"`
	PodUID       string     `json:"-"`
	NodeUID      string     `json:"-"`
	VolumeName   string     `json:"-"`
	PVCNamespace string     `json:"-"`
	PVCName      string     `json:"-"`
	Filesystem   Filesystem `json:"filesystem"`
}

type Volume struct {
	Binding Binding                    `json:"-"`
	Usage   Usage                      `json:"usage"`
	Health  []volumehealth.Observation `json:"health"`
}

// HealthObservation binds a Kubernetes health read to the currently resolved
// Node lifetime. The adapter must invalidate cached reports on Node replacement.
type HealthObservation struct {
	volumehealth.Observation
	NodeUID string `json:"-"`
}

// Report defaults to a redacted export. Authorised is the deliberate named
// response boundary; callers must never use it for logs or default captures.
type Report struct {
	scope   PodScope
	volumes []Volume
}

func (PodScope) String() string            { return "volume Pod scope" }
func (PodScope) GoString() string          { return "volume Pod scope" }
func (Binding) String() string             { return "volume binding" }
func (Binding) GoString() string           { return "volume binding" }
func (RawUsage) String() string            { return "volume usage observation" }
func (RawUsage) GoString() string          { return "volume usage observation" }
func (HealthObservation) String() string   { return "volume health observation" }
func (HealthObservation) GoString() string { return "volume health observation" }
