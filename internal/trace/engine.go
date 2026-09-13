// Package trace defines the internal contract for the optional tracing engine.
// It does not load programmes, install a tracer, or authorise a caller.
package trace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

const ContractVersion = 1

// Adapter implementations select verified, release-matched programmes internally.
// A Specification never contains a gadget reference, bytecode or an open-ended
// parameter map. Implementations must terminate and release owned resources
// before returning, including when the context or output fails.
type Adapter interface {
	Run(context.Context, Specification, Output) (Result, error)
}

// Engine rejects invalid work before calling the optional node-side adapter.
// Authentication, live target verification and lower server policy limits are
// admission responsibilities; this boundary independently retains absolute caps.
type Engine struct{ adapter Adapter }

func NewEngine(adapter Adapter) (*Engine, error) {
	if adapter == nil {
		return nil, errors.New("trace engine adapter is required")
	}
	return &Engine{adapter: adapter}, nil
}

func (e *Engine) Run(ctx context.Context, spec Specification, output Output) (Result, error) {
	if err := spec.Validate(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if e == nil || e.adapter == nil || output == nil {
		return Result{}, errors.New("trace engine and output are required")
	}
	return e.adapter.Run(ctx, spec, output)
}

// Output receives only observations filtered to the admitted target before
// reaching userspace. A callback error cancels execution; adapters must not retry
// delivery or retain the observation. Transport framing belongs to the session.
type Output interface {
	FileActivity(FileActivity) error
	CacheActivity(CacheActivity) error
	OOMDecision(OOMDecision) error
}

type FileOperation string

const (
	FileRead  FileOperation = "read"
	FileWrite FileOperation = "write"
	FileOpen  FileOperation = "open"
)

// FileActivity describes an observed operation, not a cache hit or miss.
// RequestedBytes and CompletedBytes have separate meanings; nil is unreported.
type FileActivity struct {
	ObservedAt     time.Time
	Operation      FileOperation
	RequestedBytes *uint64
	CompletedBytes *uint64
	Path           SensitiveText
}

type CacheOperation string

const (
	CacheAdd    CacheOperation = "add"
	CacheRemove CacheOperation = "remove"
)

// CacheActivity describes a cache hook observed in the selected task's context.
// It does not establish ownership of shared pages or complete cache accounting.
type CacheActivity struct {
	ObservedAt time.Time
	Operation  CacheOperation
	Pages      uint64
}

type OOMScope string

const (
	OOMScopeUnknown OOMScope = "unknown"
	OOMScopeCgroup  OOMScope = "cgroup"
	OOMScopeGlobal  OOMScope = "global"
)

// OOMDecision contains only the selected victim's optional process context.
// Invoking tasks and neighbouring victims must not be emitted by the programme.
type OOMDecision struct {
	ObservedAt time.Time
	Scope      OOMScope
	VictimPID  *uint32
	Command    SensitiveText
}

func (FileActivity) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "[file trace event]") }
func (CacheActivity) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[cache trace event]") }
func (OOMDecision) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[OOM trace event]") }

func (FileActivity) MarshalJSON() ([]byte, error) {
	return nil, errors.New("trace event requires explicit ephemeral encoding")
}
func (CacheActivity) MarshalJSON() ([]byte, error) {
	return nil, errors.New("trace event requires explicit ephemeral encoding")
}
func (OOMDecision) MarshalJSON() ([]byte, error) {
	return nil, errors.New("trace event requires explicit ephemeral encoding")
}

type Termination string

const (
	Expired           Termination = "expired"
	Cancelled         Termination = "cancelled"
	TargetChanged     Termination = "target_changed"
	OutputLimit       Termination = "output_limit"
	EventLimit        Termination = "event_limit"
	EngineFailed      Termination = "engine_failed"
	AuthorisationLost Termination = "authorisation_lost"
)

// Counts are cumulative over one session. Nil counts mean the engine could not
// establish the value, never zero. The session owns delivered/encoded-byte counts.
type Counts struct {
	Produced *uint64
	Sampled  *uint64
	Lost     *uint64
	Rejected *uint64
}

type Result struct {
	Version     int
	StartedAt   time.Time
	EndedAt     time.Time
	Termination Termination
	Counts      Counts
	Incomplete  bool
}
