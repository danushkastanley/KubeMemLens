package trace

import (
	"errors"
	"fmt"
	"io"
	"time"
)

type GaugePair struct{ Before, After *uint64 }
type CounterDelta struct {
	State string // reported, unreported, reset
	Delta *uint64
}

// Correlation relates one trace window to bounded samples of the same retained
// cgroup lifetime. It reports temporal overlap, never causation or page ownership.
type Correlation struct {
	State                                             string // overlapping, disjoint, target_changed, clock_uncertain, unavailable
	EvidenceStart, BeforeEnd, AfterStart, EvidenceEnd time.Time
	OverlapStart, OverlapEnd                          time.Time
	Uncertainty                                       *time.Duration
	File, Dirty, Writeback                            GaugePair
	Refault, Scan, Steal                              CounterDelta
}

func (Correlation) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[ephemeral cgroup correlation]")
}
func (Correlation) MarshalJSON() ([]byte, error) {
	return nil, errors.New("correlation requires explicit ephemeral encoding")
}

// Validate requires the exact guaranteed overlap and bounded sampling intervals.
// Unavailable states contain no numeric evidence or invented zero timestamps.
func (c Correlation) Validate(start, end time.Time, duration time.Duration) error {
	invalid := errors.New("invalid trace cgroup correlation")
	window := CorrelationWindow{c.State, c.EvidenceStart, c.BeforeEnd, c.AfterStart, c.EvidenceEnd, c.OverlapStart, c.OverlapEnd, c.Uncertainty}
	if window.Validate(start, end, duration) != nil {
		return invalid
	}
	if c.State != "overlapping" {
		for _, g := range []GaugePair{c.File, c.Dirty, c.Writeback} {
			if g.Before != nil || g.After != nil {
				return invalid
			}
		}
		for _, d := range []CounterDelta{c.Refault, c.Scan, c.Steal} {
			if d.State != "" || d.Delta != nil {
				return invalid
			}
		}
		return nil
	}
	for _, d := range []CounterDelta{c.Refault, c.Scan, c.Steal} {
		switch d.State {
		case "reported":
			if d.Delta == nil {
				return invalid
			}
		case "unreported", "reset":
			if d.Delta != nil {
				return invalid
			}
		default:
			return invalid
		}
	}
	return nil
}
