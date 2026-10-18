// Package memorytopology keeps optional NUMA and HugeTLB observations separate
// from ordinary memory accounting, transport, permissions and presentation.
package memorytopology

import (
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

const (
	SchemaVersion       = 1
	MaxNodes            = 8
	MaxPageSizes        = 8
	MaxObservationBytes = 16 << 10
	MaxReportBytes      = 64 << 10
	FreshFor            = 45 * time.Second
	FutureSkew          = 5 * time.Second
	MinimumExcessBytes  = 64 << 20
)

var ErrInvalid = errors.New("invalid memory topology evidence")
var ErrBounds = errors.New("memory topology exceeds its evidence budget")

type Source string

const (
	NUMASysfs     Source = "linux-numa-sysfs"
	HugeTLBSysfs  Source = "linux-hugetlb-sysfs"
	HugeTLBCgroup Source = "cgroup-v2-hugetlb"
)

type Reason string

const (
	NotObserved     Reason = "not-observed"
	SourceAbsent    Reason = "source-absent"
	ProfileDisabled Reason = "profile-disabled"
	AccessDenied    Reason = "access-denied"
	InvalidSource   Reason = "invalid-source"
	SourceBounds    Reason = "source-bounds"
	SourceChanged   Reason = "source-changed"
	TimedOut        Reason = "timed-out"
	SourceFailed    Reason = "source-failed"
	PartialFields   Reason = "partial-fields"
)

type Section[T any] struct {
	Source       Source                  `json:"source"`
	Availability capability.Availability `json:"availability"`
	Completeness capability.Completeness `json:"completeness"`
	Reason       Reason                  `json:"reason,omitempty"`
	CapturedAt   time.Time               `json:"capturedAt,omitzero"`
	Items        []T                     `json:"items"`
}

type Observation struct {
	SchemaVersion int                 `json:"schemaVersion"`
	NodeName      string              `json:"nodeName"`
	NodeUID       string              `json:"nodeUID"`
	ReportedAt    time.Time           `json:"reportedAt"`
	NUMA          Section[NUMANode]   `json:"numa"`
	Pools         Section[HugePool]   `json:"pools"`
	Cgroup        Section[HugeCgroup] `json:"cgroup"`
}

type NUMANode struct {
	ID         uint16     `json:"id"`
	TotalBytes *uint64    `json:"totalBytes,omitempty"`
	FreeBytes  *uint64    `json:"freeBytes,omitempty"`
	Placement  *Placement `json:"placement,omitempty"`
	Pools      []HugePool `json:"pools"`
}

// Placement counters are cumulative pages, not latency or measured traffic.
type Placement struct {
	HitPages        *uint64 `json:"hitPages,omitempty"`
	MissPages       *uint64 `json:"missPages,omitempty"`
	ForeignPages    *uint64 `json:"foreignPages,omitempty"`
	LocalPages      *uint64 `json:"localPages,omitempty"`
	OtherPages      *uint64 `json:"otherPages,omitempty"`
	InterleavePages *uint64 `json:"interleavePages,omitempty"`
}

// TotalPages already includes SurplusPages. ReservedPages is an outstanding
// commitment within the free pool, not additional current allocation.
type HugePool struct {
	PageSizeBytes uint64  `json:"pageSizeBytes"`
	TotalPages    *uint64 `json:"totalPages,omitempty"`
	FreePages     *uint64 `json:"freePages,omitempty"`
	ReservedPages *uint64 `json:"reservedPages,omitempty"`
	SurplusPages  *uint64 `json:"surplusPages,omitempty"`
}

// Nil Bytes with Unlimited=false is unreported; a pointer to zero is a limit.
type Limit struct {
	Bytes     *uint64 `json:"bytes,omitempty"`
	Unlimited bool    `json:"unlimited"`
}

type DomainBytes struct {
	ID    uint16 `json:"id"`
	Bytes uint64 `json:"bytes"`
}

type HugeCgroup struct {
	PageSizeBytes    uint64        `json:"pageSizeBytes"`
	CurrentBytes     *uint64       `json:"currentBytes,omitempty"`
	Limit            Limit         `json:"limit"`
	ReservationBytes *uint64       `json:"reservationBytes,omitempty"`
	ReservationLimit Limit         `json:"reservationLimit"`
	LimitFailures    *uint64       `json:"limitFailures,omitempty"`
	NUMATotalBytes   *uint64       `json:"numaTotalBytes,omitempty"`
	NUMA             []DomainBytes `json:"numa"`
}
