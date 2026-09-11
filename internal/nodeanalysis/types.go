// Package nodeanalysis interprets bounded Node and authorised cgroup evidence.
// It has no transport, persistence or Kubernetes client dependency.
package nodeanalysis

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

const (
	SchemaVersion       = 1
	MaxContributors     = 100
	DefaultContributors = 20
	MaxContainers       = 10000
	CgroupStaleAfter    = 30 * time.Second
)

type Severity string

const (
	Unknown  Severity = "unknown"
	Normal   Severity = "normal"
	Warning  Severity = "warning"
	Critical Severity = "critical"
)

type Confidence string

const (
	Low    Confidence = "low"
	Medium Confidence = "medium"
	High   Confidence = "high"
)

type Access string

const (
	NodeOnly    Access = "node-only"
	ClusterPods Access = "cluster-pods"
)

type Coverage string

const (
	Missing  Coverage = "missing"
	Partial  Coverage = "partial"
	Complete Coverage = "complete"
)

type Metric string

const (
	Total    Metric = "total"
	Anon     Metric = "anon"
	Cache    Metric = "cache"
	Shmem    Metric = "shmem"
	Residual Metric = "residual"
	PSI      Metric = "psi"
	OOM      Metric = "oom"
)

type Caveat string

const (
	SourceMissing      Caveat = "node-source-missing"
	SourceFailed       Caveat = "node-source-failed"
	SourceStale        Caveat = "node-source-stale"
	AgentMissing       Caveat = "cgroup-agent-missing"
	AgentPartial       Caveat = "cgroup-coverage-partial"
	AgentStale         Caveat = "cgroup-snapshot-stale"
	Unmapped           Caveat = "unmapped-containers"
	IdentityMismatch   Caveat = "node-identity-mismatch"
	Skewed             Caveat = "sample-skew"
	NegativeGap        Caveat = "negative-gap-floored"
	Overflow           Caveat = "arithmetic-overflow"
	Unqualified        Caveat = "accounting-unqualified"
	SystemOverlap      Caveat = "system-accounting-overlap"
	PodAccessDenied    Caveat = "cluster-pod-access-unavailable"
	HugepagesSeparate  Caveat = "hugepage-pools-are-not-headroom"
	CounterReset       Caveat = "counter-window-unavailable"
	SwapNotIO          Caveat = "swap-allocation-is-not-swap-io"
	NoPressureEvidence Caveat = "pressure-evidence-unavailable"
	CompositionOverlap Caveat = "cgroup-composition-overlap"
)

type Input struct {
	Now                time.Time
	NodeUID            string
	NodeName           string
	Current            *nodecontext.Observation
	Previous           *nodecontext.Observation
	SourceAvailability capability.Availability
	Access             Access
	Cgroup             CgroupFrame
	Qualification      *Qualification
	Rank               Metric
	Limit              int
}

type CgroupFrame struct {
	NodeUID    string
	CapturedAt time.Time
	Coverage   Coverage
	Containers []Container
}

// Container contains only fields required for analysis; raw paths, labels,
// identifiers from other Nodes, and arbitrary source text do not enter it.
type Container struct {
	ID                    string
	Namespace             string
	PodName               string
	PodUID                string
	ContainerName         string
	WorkloadKind          string
	WorkloadName          string
	Charge                Charges
	PSIFullAvg10          *float64
	OOMKills              *uint64
	OOMWindowStartedAt    time.Time
	CompositionConsistent bool
}

type Charges struct {
	Total    uint64 `json:"totalBytes"`
	Anon     uint64 `json:"anonBytes"`
	Cache    uint64 `json:"cacheBytes"`
	Shmem    uint64 `json:"shmemBytes"`
	Residual uint64 `json:"residualBytes"`
}

type Analysis struct {
	SchemaVersion     int                  `json:"schemaVersion"`
	NodeName          string               `json:"nodeName"`
	NodeUID           string               `json:"nodeUID"`
	EvaluatedAt       time.Time            `json:"evaluatedAt"`
	Severity          Severity             `json:"severity"`
	Confidence        Confidence           `json:"confidence"`
	Signals           []Signal             `json:"signals"`
	Caveats           []Caveat             `json:"caveats"`
	Facts             Facts                `json:"facts"`
	Coverage          *ContributorCoverage `json:"coverage,omitempty"`
	ObservedPodCharge *uint64              `json:"observedPodChargeBytes,omitempty"`
	OutsidePods       Estimate             `json:"outsideObservedPods"`
	Unaccounted       Estimate             `json:"unaccounted"`
	Rankings          *Rankings            `json:"rankings,omitempty"`
	ContributorAccess Access               `json:"contributorAccess"`
}

type Facts struct {
	ContextSource        capability.Source              `json:"contextSource,omitempty"`
	MajorFaultFormula    string                         `json:"majorFaultFormula,omitempty"`
	SwapGrowthFormula    string                         `json:"swapGrowthFormula,omitempty"`
	FaultWindowStartedAt time.Time                      `json:"faultWindowStartedAt,omitzero"`
	SwapWindowStartedAt  time.Time                      `json:"swapWindowStartedAt,omitzero"`
	Source               capability.Source              `json:"source"`
	Availability         capability.Availability        `json:"availability"`
	Memory               *nodecontext.Memory            `json:"memory,omitempty"`
	Swap                 *nodecontext.Swap              `json:"swap,omitempty"`
	Context              *nodecontext.KubernetesContext `json:"context,omitempty"`
	SystemContainers     []nodecontext.SystemContainer  `json:"systemContainers,omitempty"`
	GrowingSwapBytes     *uint64                        `json:"growingSwapBytes,omitempty"`
	MajorFaultsPerSecond *float64                       `json:"majorFaultsPerSecond,omitempty"`
}

type Signal struct {
	Count      *uint64           `json:"count,omitempty"`
	Code       string            `json:"code"`
	Source     capability.Source `json:"source"`
	CapturedAt time.Time         `json:"capturedAt"`
	Severity   Severity          `json:"severity"`
	Value      *float64          `json:"value,omitempty"`
	Unit       string            `json:"unit,omitempty"`
}

type Estimate struct {
	State         capability.Availability `json:"state"`
	Bytes         *uint64                 `json:"bytes,omitempty"`
	Formula       string                  `json:"formula"`
	Source        string                  `json:"source"`
	CapturedAt    time.Time               `json:"capturedAt,omitzero"`
	ComparedAt    time.Time               `json:"comparedAt,omitzero"`
	Qualification string                  `json:"qualification,omitempty"`
	Caveats       []Caveat                `json:"caveats,omitempty"`
}

type ContributorCoverage struct {
	Source                capability.Source `json:"source"`
	ObservedChargeFormula string            `json:"observedChargeFormula"`
	State                 Coverage          `json:"state"`
	CapturedAt            time.Time         `json:"capturedAt,omitzero"`
	MappedContainers      int               `json:"mappedContainers"`
	UnmappedContainers    int               `json:"unmappedContainers"`
	Pods                  int               `json:"pods"`
}

type Contributor struct {
	Namespace    string   `json:"namespace"`
	Name         string   `json:"name"`
	UID          string   `json:"uid,omitempty"`
	Kind         string   `json:"kind"`
	Charge       Charges  `json:"charge"`
	PSIFullAvg10 *float64 `json:"psiFullAvg10,omitempty"`
	OOMKills     *uint64  `json:"oomKills,omitempty"`
}

type Rankings struct {
	Source    capability.Source `json:"source"`
	Formula   string            `json:"formula"`
	Metric    Metric            `json:"metric"`
	Pods      []Contributor     `json:"pods"`
	Workloads []Contributor     `json:"workloads"`
	Limit     int               `json:"limit"`
	Truncated bool              `json:"truncated"`
}
