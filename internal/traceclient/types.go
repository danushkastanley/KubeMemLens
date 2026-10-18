// Package traceclient owns bounded authenticated operator trace workflows.
// It never talks to node services or falls back to name-only admission.
package traceclient

import (
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

type ErrorKind string

const (
	Invalid       ErrorKind = "invalid"
	Configuration ErrorKind = "configuration"
	Unavailable   ErrorKind = "unavailable"
	Denied        ErrorKind = "denied"
	TargetChanged ErrorKind = "target_changed"
	Capacity      ErrorKind = "capacity"
	Gone          ErrorKind = "gone"
	Protocol      ErrorKind = "invalid_response"
	Uncertain     ErrorKind = "outcome_unknown"
	Incomplete    ErrorKind = "incomplete"
	Incompatible  ErrorKind = "incompatible"
)

type Error struct{ Kind ErrorKind }

func (e *Error) Error() string {
	switch e.Kind {
	case Invalid:
		return "trace selection or limits are invalid"
	case Configuration:
		return "trace access requires a verified Kubernetes HTTPS configuration and native credential transport"
	case Denied:
		return "trace permission was denied; trace and Pod access are separate from ordinary memory reads"
	case TargetChanged:
		return "the selected target or node profile changed; select it again and repeat preflight"
	case Capacity:
		return "trace capacity is currently exhausted"
	case Gone:
		return "the trace is absent or expired; this does not confirm kernel cleanup"
	case Protocol:
		return "the trace service returned an invalid or incompatible response"
	case Uncertain:
		return "the trace request outcome is unknown; it was not retried automatically"
	case Incomplete:
		return "the trace stream ended without a complete terminal result; partial evidence remains incomplete"
	case Incompatible:
		return "the CLI and trace extension have no compatible trace contract; use a supported version pair"
	default:
		return "the optional trace service is unavailable; ask an administrator to check its installation and node preflight"
	}
}
func failure(kind ErrorKind) error { return &Error{Kind: kind} }

type Selection struct {
	Namespace, Pod, PodUID, Container, ContainerID, NodeName string
	ContainerStartedAt                                       time.Time
}

// SelectionPin comes from an already displayed container row. It prevents a
// subsequent live read from silently replacing the operator's selected target.
type SelectionPin struct{ PodUID, ContainerID, NodeName string }

func (SelectionPin) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private trace selection pin]")
}
func (SelectionPin) MarshalJSON() ([]byte, error) { return nil, failure(Invalid) }

func (Selection) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private trace selection]") }
func (Selection) MarshalJSON() ([]byte, error) { return nil, failure(Invalid) }

type Intent struct {
	Kind   trace.Kind
	Paths  trace.PathPolicy
	Bounds trace.Bounds
}

func DefaultIntent(kind trace.Kind) Intent {
	return Intent{Kind: kind, Paths: trace.OmitPaths, Bounds: trace.DefaultBounds()}
}

// Plan freezes exactly what preflight checked; admission still repeats checks.
type Plan struct {
	client    *Client
	selection Selection
	intent    Intent
	document  admission.PreflightDocument
}

func (p Plan) Selection() Selection       { return p.selection }
func (p Plan) Intent() Intent             { return p.intent }
func (Plan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private trace plan]") }
func (Plan) MarshalJSON() ([]byte, error) { return nil, failure(Invalid) }

type Admission struct {
	client                       *Client
	namespace, id, engine, state string
	expires                      time.Time
	plan                         *Plan
}

func (a Admission) ID() string                 { return a.id }
func (a Admission) Namespace() string          { return a.namespace }
func (a Admission) State() string              { return a.state }
func (a Admission) ExpiresAt() time.Time       { return a.expires }
func (Admission) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private trace admission]") }
func (Admission) MarshalJSON() ([]byte, error) { return nil, failure(Invalid) }

type Cleanup string

const (
	CleanupUnconfirmed Cleanup = "unconfirmed"
	CleanupConfirmed   Cleanup = "confirmed"
)
