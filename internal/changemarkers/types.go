// Package changemarkers aligns bounded workload changes with memory evidence.
// It preserves source identity and time without claiming that a change caused
// a memory observation, or that retained Kubernetes events are an audit log.
package changemarkers

import (
	"context"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

const (
	MaxMarkers     = 64
	MaxOwners      = 3
	MaxEvents      = 256
	MaxReportBytes = 128 << 10
)

var ErrInvalid = errors.New("invalid workload change evidence")
var ErrBounds = errors.New("workload change evidence exceeds its budget")
var ErrUnsupported = errors.New("workload change context does not support this controller type or version")

type Kind string

const (
	Created     Kind = "created"
	Restarted   Kind = "restarted"
	Rollout     Kind = "rollout"
	Revision    Kind = "revision-observed"
	Resize      Kind = "resize"
	Replacement Kind = "pod-replacement"
	Scheduled   Kind = "scheduled"
	Started     Kind = "started"
	Stopped     Kind = "stopped"
	BackOff     Kind = "back-off"
	Evicted     Kind = "evicted"
)

type Clock string

const (
	APICreation     Clock = "api-creation"
	ContainerState  Clock = "container-state"
	KubernetesEvent Clock = "kubernetes-event"
	PodCondition    Clock = "pod-condition"
	Observation     Clock = "observation-interval"
)

type Coverage string

const (
	Partial     Coverage = "partial"
	Missing     Coverage = "missing"
	Denied      Coverage = "denied"
	Unavailable Coverage = "unavailable"
	Disabled    Coverage = "disabled"
)

type Object struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
}

// Owners runs from the subject's immediate controller to its outermost verified
// controller. It never contains unverified ownerReference strings.
type Marker struct {
	Kind        Kind      `json:"kind"`
	At          time.Time `json:"at"`
	Until       time.Time `json:"until"`
	Clock       Clock     `json:"clock"`
	SourceUID   string    `json:"sourceUID"`
	Subject     Object    `json:"subject"`
	Owners      []Object  `json:"owners,omitempty"`
	Container   string    `json:"container,omitempty"`
	Revision    int64     `json:"revision,omitempty"`
	ResizeState string    `json:"resizeState,omitempty"`
	PreviousUID string    `json:"previousUID,omitempty"`
	Count       uint64    `json:"count"`
	Uncertain   bool      `json:"uncertain"`
}

type Report struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Request       memoryhistory.Request `json:"request"`
	UID           string                `json:"uid"`
	Start         time.Time             `json:"start"`
	End           time.Time             `json:"end"`
	ObservedAt    time.Time             `json:"observedAt"`
	Events        Coverage              `json:"events"`
	Truncated     bool                  `json:"truncated"`
	Markers       []Marker              `json:"markers"`
}

type Provider interface {
	Query(context.Context, memoryhistory.Selection, memoryhistory.Query) (Report, error)
	Revalidate(context.Context, Report) error
}

// Context is a new opt-in representation. The nested history retains its original
// schema and semantics; an old client never receives this envelope on /trends.
type Context struct {
	SchemaVersion int                  `json:"schemaVersion"`
	History       memoryhistory.Report `json:"history"`
	Changes       Report               `json:"changes"`
}
