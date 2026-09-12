// Package capability selects evidence sources without depending on a transport
// or renderer. Discovery does not grant access; every query still authorises.
package capability

import (
	"context"
	"fmt"
	"time"
)

type Mode string

const (
	Auto       Mode = "auto"
	Deep       Mode = "deep"
	Restricted Mode = "restricted"
)

func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case "", Auto:
		return Auto, nil
	case Deep, Restricted:
		return Mode(value), nil
	default:
		return "", fmt.Errorf("invalid evidence mode %q, want auto, deep or restricted", value)
	}
}

type Source string

const (
	Cgroup            Source = "cgroup"
	KubernetesMetrics Source = "kubernetes-metrics"
	KubernetesStatus  Source = "kubernetes-status"
)

type Availability string

const (
	Available   Availability = "available"
	Forbidden   Availability = "forbidden"
	Absent      Availability = "absent"
	Unsupported Availability = "unsupported"
	Disabled    Availability = "disabled"
	Unreported  Availability = "unreported"
	Unavailable Availability = "unavailable"
)

type Freshness string

const (
	Fresh            Freshness = "fresh"
	Stale            Freshness = "stale"
	Rebuilding       Freshness = "rebuilding"
	UnknownFreshness Freshness = "unavailable"
)

type Completeness string

const (
	Complete            Completeness = "complete"
	Partial             Completeness = "partial"
	UnsupportedEvidence Completeness = "unsupported"
)

type Stability string

const (
	Stable                 Stability = "stable"
	Beta                   Stability = "beta"
	Alpha                  Stability = "alpha"
	ImplementationSpecific Stability = "implementation-specific"
)

type Scope string

const (
	NodeScope      Scope = "node"
	NamespaceScope Scope = "namespace"
	WorkloadScope  Scope = "workload"
	PodScope       Scope = "pod"
	ContainerScope Scope = "container"
	VolumeScope    Scope = "volume"
)

type Reason string

const (
	AccessDenied         Reason = "access-denied"
	SourceAbsent         Reason = "source-absent"
	UnsupportedAPI       Reason = "unsupported-api"
	RequestFailed        Reason = "request-failed"
	InvalidResponse      Reason = "invalid-response"
	AuthenticationFailed Reason = "authentication-failed"
	NotObserved          Reason = "not-observed"
	DiscoveryTimedOut    Reason = "discovery-timed-out"
	DiscoveryCancelled   Reason = "discovery-cancelled"
	RequiresDeep         Reason = "requires-deep-evidence"
	QueryNotImplemented  Reason = "query-not-implemented"
)

// Envelope accompanies a measurement. Discovery alone cannot fill sample times
// or assert completeness/freshness. Caveats contain bounded, non-sensitive text.
type Envelope struct {
	Source       Source        `json:"source"`
	APIVersion   string        `json:"apiVersion,omitempty"`
	CapturedAt   time.Time     `json:"capturedAt,omitzero"`
	ReceivedAt   time.Time     `json:"receivedAt,omitzero"`
	Window       time.Duration `json:"windowNanoseconds,omitempty"`
	Scope        Scope         `json:"scope"`
	Freshness    Freshness     `json:"freshness"`
	Completeness Completeness  `json:"completeness"`
	Stability    Stability     `json:"capability"`
	Caveats      []string      `json:"caveats,omitempty"`
}

type SourceState struct {
	Source       Source       `json:"source"`
	Availability Availability `json:"availability"`
	Reason       Reason       `json:"reason,omitempty"`
	APIVersion   string       `json:"apiVersion,omitempty"`
	Freshness    Freshness    `json:"freshness"`
	Completeness Completeness `json:"completeness"`
	Stability    Stability    `json:"capability"`
}

type Probe interface {
	Discover(context.Context) (SourceState, error)
}

type ProbeFunc func(context.Context) (SourceState, error)

func (f ProbeFunc) Discover(ctx context.Context) (SourceState, error) { return f(ctx) }

type Query string

const (
	Current      Query = "current"
	Composition  Query = "composition"
	LocalEvents  Query = "local-events"
	Pressure     Query = "pressure"
	History      Query = "history"
	Capture      Query = "capture"
	VolumeHealth Query = "volume-health"
	NodeContext  Query = "node-context"
	Trace        Query = "trace"
)

type QueryState struct {
	Query        Query        `json:"query"`
	Availability Availability `json:"availability"`
	Reason       Reason       `json:"reason,omitempty"`
}

type Selection struct {
	Mode         Mode          `json:"mode,omitempty"`
	State        Availability  `json:"state"`
	Freshness    Freshness     `json:"freshness"`
	Completeness Completeness  `json:"completeness"`
	Sources      []SourceState `json:"sources"`
	Queries      []QueryState  `json:"queries"`
}

func (s Selection) Require(query Query) error {
	for _, q := range s.Queries {
		if q.Query == query && q.Availability == Available {
			return nil
		}
		if q.Query == query {
			return &SelectionError{Mode: s.Mode, Reason: q.Reason}
		}
	}
	return &SelectionError{Mode: s.Mode, Reason: QueryNotImplemented}
}

type SelectionError struct {
	Mode   Mode
	Reason Reason
	Cause  error
}

func (e *SelectionError) Unwrap() error { return e.Cause }

func (e *SelectionError) Error() string {
	if e.Reason == AccessDenied {
		return fmt.Sprintf("%s evidence is unavailable: %s (permission denied)", e.Mode, e.Reason)
	}
	return fmt.Sprintf("%s evidence is unavailable: %s", e.Mode, e.Reason)
}

// Label is shared by status and the TUI. It never contains cluster identities.
func (s Selection) Label() string {
	switch s.Mode {
	case Deep:
		return "deep / cgroup"
	case Restricted:
		return "restricted / Kubernetes APIs"
	default:
		return "evidence unavailable"
	}
}
