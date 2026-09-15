package trace

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// CorrelationWindow records guaranteed temporal overlap without choosing the
// meaning of the sampled counters. File/cache and OOM evidence share this bound.
type CorrelationWindow struct {
	State                                             string
	EvidenceStart, BeforeEnd, AfterStart, EvidenceEnd time.Time
	OverlapStart, OverlapEnd                          time.Time
	Uncertainty                                       *time.Duration
}

func (CorrelationWindow) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[ephemeral correlation window]")
}
func (CorrelationWindow) MarshalJSON() ([]byte, error) {
	return nil, errors.New("correlation requires explicit ephemeral encoding")
}

func (c CorrelationWindow) Validate(start, end time.Time, duration time.Duration) error {
	invalid := errors.New("invalid trace correlation window")
	if c.State != "overlapping" {
		switch c.State {
		case "disjoint", "target_changed", "clock_uncertain", "unavailable":
		default:
			return invalid
		}
		if !c.EvidenceStart.IsZero() || !c.BeforeEnd.IsZero() || !c.AfterStart.IsZero() || !c.EvidenceEnd.IsZero() || !c.OverlapStart.IsZero() || !c.OverlapEnd.IsZero() || c.Uncertainty != nil {
			return invalid
		}
		return nil
	}
	if start.IsZero() || !end.After(start) || duration <= 0 || duration > 5*time.Minute || end.Sub(start) > duration || c.Uncertainty == nil || *c.Uncertainty < 0 || *c.Uncertainty > 5*time.Millisecond {
		return invalid
	}
	if c.EvidenceStart.IsZero() || c.BeforeEnd.Before(c.EvidenceStart) || c.BeforeEnd.Sub(c.EvidenceStart) > time.Second || !c.AfterStart.After(c.BeforeEnd) || c.EvidenceEnd.Before(c.AfterStart) || c.EvidenceEnd.Sub(c.AfterStart) > time.Second || c.EvidenceEnd.Sub(c.EvidenceStart) > duration+2*time.Second {
		return invalid
	}
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
	return nil
}
