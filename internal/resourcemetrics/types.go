// Package resourcemetrics provides optional, caller-authorised Metrics API reads.
// Working-set memory is a separate source from cgroup charge and composition.
package resourcemetrics

import (
	"context"
	"time"
)

type Availability string

const (
	Available       Availability = "available"
	MissingProvider Availability = "missing-provider"
	Forbidden       Availability = "forbidden"
	Unavailable     Availability = "unavailable"
	Partial         Availability = "partial"
	Stale           Availability = "stale"
)

type Reason string

const (
	NoReason               Reason = ""
	DiscoveryMissing       Reason = "discovery-missing"
	UnsupportedAPI         Reason = "unsupported-api"
	AccessDenied           Reason = "access-denied"
	RequestFailed          Reason = "request-failed"
	InvalidResponse        Reason = "invalid-response"
	IncompleteUsage        Reason = "incomplete-usage"
	LimitReached           Reason = "limit-reached"
	OutsideFreshnessBounds Reason = "outside-freshness-bounds"
)

type Freshness string

const (
	Fresh  Freshness = "fresh"
	Old    Freshness = "stale"
	Future Freshness = "future"
)

// Identity preserves exactly the provider's join fields. UID may be unreported;
// callers must not invent a UID match from a display name alone.
type Identity struct {
	Namespace     string `json:"namespace"`
	PodName       string `json:"podName"`
	PodUID        string `json:"podUID,omitempty"`
	ContainerName string `json:"containerName"`
}

type Observation struct {
	Identity              Identity      `json:"identity"`
	APIVersion            string        `json:"apiVersion"`
	Timestamp             time.Time     `json:"timestamp"`
	Window                time.Duration `json:"windowNanoseconds"`
	Freshness             Freshness     `json:"freshness"`
	CPUUsageNanocores     uint64        `json:"cpuUsageNanocores"`
	MemoryWorkingSetBytes uint64        `json:"memoryWorkingSetBytes"`
}

type Report struct {
	Availability      Availability  `json:"availability"`
	Reason            Reason        `json:"reason,omitempty"`
	APIVersion        string        `json:"apiVersion,omitempty"`
	Observations      []Observation `json:"observations"`
	OmittedContainers int           `json:"omittedContainers"`
	// A successful list cannot establish whether the provider omitted entire Pods.
	ProviderCoverageKnown bool `json:"providerCoverageKnown"`
}

type Source interface {
	Read(context.Context) (Report, error)
}

type Options struct {
	Namespace        string
	Timeout          time.Duration
	MaxAge           time.Duration
	MaxFutureSkew    time.Duration
	MaxResponseBytes int64
	PageSize         int
	MaxPages         int
	MaxPods          int
	MaxContainers    int
	Now              func() time.Time
}
