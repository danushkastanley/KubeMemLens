// Package memoryhistory keeps retained cgroup evidence and remote memory trends
// in one bounded read contract without treating their measurements as equivalent.
package memoryhistory

import (
	"context"
	"errors"
	"time"
)

const (
	MaxTargets       = 16
	MaxPoints        = 241
	MaxRange         = 7 * 24 * time.Hour
	MinStep          = time.Minute
	MaxResponseBytes = 1 << 20
	SampleMaxAge     = 2 * time.Minute
	QueryTimeout     = 5 * time.Second
)

var (
	ErrSource      = errors.New("memory history provider returned invalid evidence")
	ErrNotFound    = errors.New("memory history target was not found")
	ErrInvalid     = errors.New("invalid memory history request or response")
	ErrBounds      = errors.New("memory history exceeds its query budget")
	ErrUnavailable = errors.New("memory history provider is unavailable")
	ErrDenied      = errors.New("memory history access is denied")
	ErrChanged     = errors.New("memory history target changed; refresh the selection")
)

type Source string

const (
	Local      Source = "local"
	Prometheus Source = "prometheus"
)

type Metric string

const (
	Charge     Metric = "cgroup-charge"
	WorkingSet Metric = "working-set"
	RSS        Metric = "rss"
)

type Scope string

const (
	Pod       Scope = "pod"
	Container Scope = "container"
	Workload  Scope = "workload"
	Node      Scope = "node"
)

// Request contains names only. A server-side resolver supplies immutable identity
// after caller authorisation; a caller cannot submit a raw selector or target UID.
type Request struct {
	Scope        Scope  `json:"scope"`
	Namespace    string `json:"namespace,omitempty"`
	Name         string `json:"name"`
	Container    string `json:"container,omitempty"`
	WorkloadKind string `json:"workloadKind,omitempty"`
}

type Target struct {
	Namespace    string    `json:"namespace,omitempty"`
	Pod          string    `json:"pod,omitempty"`
	PodUID       string    `json:"podUID,omitempty"`
	Container    string    `json:"container,omitempty"`
	ContainerID  string    `json:"containerID,omitempty"`
	Node         string    `json:"node"`
	NodeUID      string    `json:"nodeUID"`
	PodCreatedAt time.Time `json:"podCreatedAt,omitzero"`
	StartedAt    time.Time `json:"startedAt"`
}

type Selection struct {
	Request    Request   `json:"request"`
	UID        string    `json:"uid"`
	Revision   string    `json:"revision,omitempty"`
	ResolvedAt time.Time `json:"resolvedAt"`
	Targets    []Target  `json:"targets"`
}

type Query struct {
	Source Source        `json:"source"`
	Metric Metric        `json:"metric"`
	Start  time.Time     `json:"start"`
	End    time.Time     `json:"end"`
	Step   time.Duration `json:"stepNanos"`
}

type State string

const (
	Disabled    State = "disabled"
	Fresh       State = "fresh"
	Stale       State = "stale"
	Missing     State = "missing"
	Unsupported State = "unsupported"
	Unavailable State = "unavailable"
)

type Completeness string

const (
	Complete Completeness = "complete"
	Partial  Completeness = "partial"
)

type Point struct {
	At        time.Time `json:"at"`
	SampledAt time.Time `json:"sampledAt,omitzero"`
	Bytes     *uint64   `json:"bytes"`
	State     State     `json:"state"`
}

type Series struct {
	FreshFor     time.Duration `json:"freshForNanos"`
	Origin       string        `json:"origin"`
	SampleClock  string        `json:"sampleClock"`
	Target       Target        `json:"target"`
	Source       Source        `json:"source"`
	Metric       Metric        `json:"metric"`
	Resolution   time.Duration `json:"resolutionNanos"`
	Freshness    State         `json:"freshness"`
	Completeness Completeness  `json:"completeness"`
	Points       []Point       `json:"points"`
}

type Report struct {
	SchemaVersion int          `json:"schemaVersion"`
	Selection     Selection    `json:"selection"`
	Query         Query        `json:"query"`
	ReceivedAt    time.Time    `json:"receivedAt"`
	State         State        `json:"state"`
	Completeness  Completeness `json:"completeness"`
	Reason        string       `json:"reason,omitempty"`
	Series        []Series     `json:"series"`
}

type Provider interface {
	Query(context.Context, Selection, Query) (Report, error)
}

// Resolver checks the caller and returns the current immutable selection.
type Resolver interface {
	Resolve(context.Context, Request) (Selection, error)
}
