// Package traceaggregate summarises already target-filtered observations.
// It retains no target, path, timestamp, event queue or upstream engine object.
package traceaggregate

import (
	"errors"
	"fmt"
	"io"
	"math/bits"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

var ErrObservation = errors.New("invalid trace aggregate observation")
var ErrLimit = errors.New("trace aggregate observation limit reached")

// Total is the sum over accepted observations. Zero observations sum to zero.
// Missing inputs and arithmetic overflow are independent reasons for an unknown
// total; neither is represented by a fabricated zero or saturated byte value.
type Total struct {
	Value      *uint64
	Unreported bool
	Overflow   bool
}
type FileOperations struct {
	Operations     uint64
	RequestedBytes Total
	CompletedBytes Total
}
type CacheOperations struct {
	Operations uint64
	Pages      Total
}
type Summary struct {
	Kind                trace.Kind
	Observations        uint64
	Reads, Writes       FileOperations
	Additions, Removals CacheOperations
}

func (Summary) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[ephemeral trace aggregates]") }
func (Summary) MarshalJSON() ([]byte, error) { return nil, ErrObservation }

type total struct {
	value             uint64
	missing, overflow bool
}

func (t *total) add(value *uint64) {
	if value == nil {
		t.missing = true
		return
	}
	sum, carry := bits.Add64(t.value, *value, 0)
	if carry != 0 {
		t.overflow = true
	}
	t.value = sum
}
func (t total) snapshot() Total {
	out := Total{Unreported: t.missing, Overflow: t.overflow}
	if !t.missing && !t.overflow {
		value := t.value
		out.Value = &value
	}
	return out
}

type files struct {
	operations           uint64
	requested, completed total
}
type cache struct {
	operations uint64
	pages      total
}

// Accumulator is owned by one output session. The caller serialises updates and
// validates observation windows/consent at its existing output boundary.
type Accumulator struct {
	kind                trace.Kind
	limit, observations uint64
	reads, writes       files
	additions, removals cache
}

func (a *Accumulator) Observations() uint64 { return a.observations }

func New(kind trace.Kind, limit uint64) (*Accumulator, error) {
	if (kind != trace.Files && kind != trace.Cache) || limit == 0 || limit > 100000 {
		return nil, ErrObservation
	}
	return &Accumulator{kind: kind, limit: limit}, nil
}

func (a *Accumulator) File(event trace.FileActivity) error {
	if a.kind != trace.Files || (event.Operation != trace.FileRead && event.Operation != trace.FileWrite) ||
		(event.RequestedBytes != nil && event.CompletedBytes != nil && *event.CompletedBytes > *event.RequestedBytes) {
		return ErrObservation
	}
	if a.observations >= a.limit {
		return ErrLimit
	}
	row := &a.reads
	if event.Operation == trace.FileWrite {
		row = &a.writes
	}
	a.observations++
	row.operations++
	row.requested.add(event.RequestedBytes)
	row.completed.add(event.CompletedBytes)
	return nil
}

func (a *Accumulator) Cache(event trace.CacheActivity) error {
	if a.kind != trace.Cache || event.Pages == 0 || (event.Operation != trace.CacheAdd && event.Operation != trace.CacheRemove) {
		return ErrObservation
	}
	if a.observations >= a.limit {
		return ErrLimit
	}
	row := &a.additions
	if event.Operation == trace.CacheRemove {
		row = &a.removals
	}
	a.observations++
	row.operations++
	row.pages.add(&event.Pages)
	return nil
}

// Snapshot returns owned numeric values, never aliases to mutable counters.
func (a *Accumulator) Snapshot() Summary {
	return Summary{Kind: a.kind, Observations: a.observations,
		Reads:     FileOperations{a.reads.operations, a.reads.requested.snapshot(), a.reads.completed.snapshot()},
		Writes:    FileOperations{a.writes.operations, a.writes.requested.snapshot(), a.writes.completed.snapshot()},
		Additions: CacheOperations{a.additions.operations, a.additions.pages.snapshot()},
		Removals:  CacheOperations{a.removals.operations, a.removals.pages.snapshot()}}
}
