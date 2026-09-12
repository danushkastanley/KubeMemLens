package model

import (
	"errors"
	"math"
)

type IOState string

const (
	IOAvailable      IOState = "available"
	IOUnreported     IOState = "unreported"
	IOUnavailable    IOState = "unavailable"
	IOInvalid        IOState = "invalid-response"
	IOPressureSource         = "cgroup-v2/io.pressure"
)

// PSIWindow measures time spent stalled, not bytes or per-volume latency.
type PSIWindow struct {
	Avg10       float64 `json:"avg10"`
	Avg60       float64 `json:"avg60"`
	Avg300      float64 `json:"avg300"`
	TotalMicros uint64  `json:"totalMicros"`
}

// IOPressure belongs to the containing container instance and CapturedAt.
// Aggregated MemoryBreakdowns omit it: overlapping stall percentages and
// cumulative counters cannot be added across containers, Pods or filesystems.
type IOPressure struct {
	State           IOState   `json:"state"`
	Some            PSIWindow `json:"some,omitzero"`
	Full            PSIWindow `json:"full,omitzero"`
	CounterReset    bool      `json:"counterReset,omitempty"`
	DeltaKnown      bool      `json:"deltaKnown,omitempty"`
	SomeDeltaMicros uint64    `json:"someDeltaMicros,omitempty"`
	FullDeltaMicros uint64    `json:"fullDeltaMicros,omitempty"`
}

func (p IOPressure) Validate() error {
	if p == (IOPressure{}) {
		return nil
	}
	invalid := errors.New("invalid cgroup I/O pressure observation")
	switch p.State {
	case IOUnreported, IOUnavailable, IOInvalid:
		if p != (IOPressure{State: p.State}) {
			return invalid
		}
		return nil
	case IOAvailable:
		for _, w := range []PSIWindow{p.Some, p.Full} {
			for _, value := range []float64{w.Avg10, w.Avg60, w.Avg300} {
				if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
					return invalid
				}
			}
		}
		if (p.CounterReset && p.DeltaKnown) || (!p.DeltaKnown && (p.SomeDeltaMicros != 0 || p.FullDeltaMicros != 0)) || p.SomeDeltaMicros > p.Some.TotalMicros || p.FullDeltaMicros > p.Full.TotalMicros {
			return invalid
		}
		return nil
	default:
		return invalid
	}
}

// withIODeltas is called only after the store has matched a container instance.
// Copy first: the incoming snapshot and prior retained sample remain immutable.
func withIODeltas(current, previous IOPressure, sameInstance bool) IOPressure {
	if current == (IOPressure{}) {
		return current
	}
	p := current
	p.CounterReset, p.DeltaKnown = false, false
	p.SomeDeltaMicros, p.FullDeltaMicros = 0, 0
	if !sameInstance || p.State != IOAvailable || previous.State != IOAvailable {
		return p
	}
	if p.Some.TotalMicros < previous.Some.TotalMicros || p.Full.TotalMicros < previous.Full.TotalMicros {
		p.CounterReset = true
		return p
	}
	p.DeltaKnown = true
	p.SomeDeltaMicros = p.Some.TotalMicros - previous.Some.TotalMicros
	p.FullDeltaMicros = p.Full.TotalMicros - previous.Full.TotalMicros
	return p
}
