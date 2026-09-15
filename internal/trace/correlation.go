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
	if c.State != "overlapping" {
		switch c.State {
		case "disjoint", "target_changed", "clock_uncertain", "unavailable":
		default:
			return invalid
		}
		if !c.EvidenceStart.IsZero() || !c.BeforeEnd.IsZero() || !c.AfterStart.IsZero() || !c.EvidenceEnd.IsZero() || !c.OverlapStart.IsZero() || !c.OverlapEnd.IsZero() || c.Uncertainty != nil {
			return invalid
		}
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
	if start.IsZero() || !end.After(start) || duration <= 0 || duration > 5*time.Minute || end.Sub(start) > duration || c.Uncertainty == nil || *c.Uncertainty < 0 || *c.Uncertainty > 5*time.Millisecond {
		return invalid
	}
	if c.EvidenceStart.IsZero() || c.BeforeEnd.Before(c.EvidenceStart) || c.BeforeEnd.Sub(c.EvidenceStart) > time.Second || !c.AfterStart.After(c.BeforeEnd) || c.EvidenceEnd.Before(c.AfterStart) || c.EvidenceEnd.Sub(c.AfterStart) > time.Second || c.EvidenceEnd.Sub(c.EvidenceStart) > duration+2*time.Second {
		return invalid
	}
	// The full evidence interval, rather than each gap around the observation,
	// is bounded above. Transport also binds it to the admitted session start.
	low, high := start.Add(*c.Uncertainty), end.Add(-*c.Uncertainty)
	if c.BeforeEnd.After(low) {
		low = c.BeforeEnd
	}
	if c.AfterStart.Before(high) {
		high = c.AfterStart
	}
	if !high.After(low) || !c.OverlapStart.Equal(low) || !c.OverlapEnd.Equal(high) {
		return invalid
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
