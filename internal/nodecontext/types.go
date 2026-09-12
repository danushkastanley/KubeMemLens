// Package nodecontext defines the optional, bounded Node memory contract.
// Collection, authenticated ingestion and presentation are separate adapters.
package nodecontext

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

const Source capability.Source = "kubelet-summary"

type Reason string

const (
	NotObserved       Reason = "not-observed"
	Unsupported       Reason = "unsupported-profile"
	InvalidTarget     Reason = "invalid-target"
	UntrustedTLS      Reason = "untrusted-tls"
	Authentication    Reason = "authentication-failed"
	Forbidden         Reason = "access-denied"
	TimedOut          Reason = "timed-out"
	Unreachable       Reason = "unreachable"
	InvalidResponse   Reason = "invalid-response"
	ResponseTooLarge  Reason = "response-too-large"
	Throttled         Reason = "throttled"
	SourceUnavailable Reason = "source-unavailable"
)

type Provenance string

const (
	Unknown  Provenance = "unknown"
	CAdvisor Provenance = "cadvisor"
	CRI      Provenance = "cri"
)

// Observation is one source report. A failure report has no Stats; the store
// retains any last good sample separately without refreshing its timestamps.
type Observation struct {
	NodeName     string                  `json:"nodeName"`
	NodeUID      string                  `json:"nodeUID"`
	ReportedAt   time.Time               `json:"reportedAt"`
	Availability capability.Availability `json:"availability"`
	Reason       Reason                  `json:"reason,omitempty"`
	Evidence     capability.Envelope     `json:"evidence"`
	Stats        *Stats                  `json:"stats,omitempty"`
	Context      *KubernetesContext      `json:"context,omitempty"`
}

type Stats struct {
	StartedAt        time.Time         `json:"startedAt"`
	Provenance       Provenance        `json:"provenance"`
	Memory           *Memory           `json:"memory,omitempty"`
	Swap             *Swap             `json:"swap,omitempty"`
	SystemContainers []SystemContainer `json:"systemContainers,omitempty"`
}

// Memory fields overlap. None may be substituted for cgroup charge or added to
// another field to form a partition. Nil means unavailable; a pointer to zero
// is a reported zero. Fault counters are cumulative, not interval rates.
type Memory struct {
	CapturedAt      time.Time `json:"capturedAt"`
	AvailableBytes  *uint64   `json:"availableBytes,omitempty"`
	UsageBytes      *uint64   `json:"usageBytes,omitempty"`
	WorkingSetBytes *uint64   `json:"workingSetBytes,omitempty"`
	RSSBytes        *uint64   `json:"rssBytes,omitempty"`
	PageFaults      *uint64   `json:"pageFaults,omitempty"`
	MajorPageFaults *uint64   `json:"majorPageFaults,omitempty"`
	PSI             *PSI      `json:"psi,omitempty"`
}

type PSI struct {
	Some PSIData `json:"some"`
	Full PSIData `json:"full"`
}

// Summary PSI cumulative totals are nanoseconds. The cgroup text source uses
// microseconds; adapters must convert explicitly rather than sharing raw units.
type PSIData struct {
	TotalNanoseconds uint64  `json:"totalNanoseconds"`
	Avg10            float64 `json:"avg10"`
	Avg60            float64 `json:"avg60"`
	Avg300           float64 `json:"avg300"`
}

type Swap struct {
	CapturedAt     time.Time `json:"capturedAt"`
	UsageBytes     *uint64   `json:"usageBytes,omitempty"`
	AvailableBytes *uint64   `json:"availableBytes,omitempty"`
}

type SystemCategory string

const (
	Kubelet SystemCategory = "kubelet"
	Runtime SystemCategory = "runtime"
	Misc    SystemCategory = "misc"
	Pods    SystemCategory = "pods"
)

// Pods overlaps observed Pod charge. Other categories may also overlap on a
// particular host configuration; qualification must establish disjointness.
type SystemContainer struct {
	Category  SystemCategory `json:"category"`
	StartedAt time.Time      `json:"startedAt"`
	Memory    *Memory        `json:"memory,omitempty"`
	Swap      *Swap          `json:"swap,omitempty"`
}

// KubernetesContext is separately sampled Node-object context. Capacity is not
// Summary usage plus available, and allocatable is not current free memory.
type KubernetesContext struct {
	CapturedAt       time.Time  `json:"capturedAt"`
	CapacityBytes    *uint64    `json:"capacityBytes,omitempty"`
	AllocatableBytes *uint64    `json:"allocatableBytes,omitempty"`
	MemoryPressure   string     `json:"memoryPressure"`
	Hugepages        []Hugepage `json:"hugepages,omitempty"`
}

type Hugepage struct {
	Resource         string  `json:"resource"`
	CapacityBytes    *uint64 `json:"capacityBytes,omitempty"`
	AllocatableBytes *uint64 `json:"allocatableBytes,omitempty"`
}
