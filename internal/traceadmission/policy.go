package traceadmission

import (
	"errors"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"time"
)

var (
	ErrUnauthenticated = errors.New("trace authentication required")
	ErrDenied          = errors.New("trace access denied")
	ErrUnavailable     = errors.New("trace admission unavailable")
	ErrCapacity        = errors.New("trace capacity exhausted")
	ErrExpired         = errors.New("trace admission expired")
	ErrTargetChanged   = errors.New("trace target changed")
	ErrNotFound        = errors.New("trace admission not found")
)

type Policy struct {
	MaxBounds    trace.Bounds
	Paths        trace.PathPolicy
	PerPrincipal int
	PerNamespace int
	PerNode      int
	Global       int
	PendingTTL   time.Duration
}

func DefaultPolicy() Policy {
	return Policy{MaxBounds: trace.DefaultBounds(), Paths: trace.OmitPaths, PerPrincipal: 1, PerNamespace: 2, PerNode: 1, Global: 32, PendingTTL: 15 * time.Second}
}

func (p Policy) validate() error {
	if p.MaxBounds.Validate() != nil || (p.Paths != trace.OmitPaths && p.Paths != trace.ConfirmedPaths) {
		return ErrInvalidRequest
	}
	if p.PerPrincipal < 1 || p.PerPrincipal > 2 || p.PerNamespace < 1 || p.PerNamespace > 4 || p.PerNode < 1 || p.PerNode > 2 || p.Global < 1 || p.Global > 64 {
		return ErrInvalidRequest
	}
	if p.PendingTTL <= 0 || p.PendingTTL > 15*time.Second {
		return ErrInvalidRequest
	}
	return nil
}

func (p Policy) permits(r Request) error {
	if r.namespace == "" {
		return ErrInvalidRequest
	}
	b, max := r.bounds, p.MaxBounds
	if b.Duration > max.Duration || b.Events > max.Events || b.OutputBytes > max.OutputBytes || b.MapBytes > max.MapBytes || b.PathBytes > max.PathBytes {
		return ErrInvalidRequest
	}
	if r.paths == trace.ConfirmedPaths && p.Paths != trace.ConfirmedPaths {
		return ErrDenied
	}
	return nil
}
