package trace

import (
	"errors"
	"fmt"
	"io"
	"time"
)

type OOMEventDeltas struct {
	Low, High, Max, OOM, Kill, GroupKill CounterDelta
}

func (d OOMEventDeltas) Values() []CounterDelta {
	return []CounterDelta{d.Low, d.High, d.Max, d.OOM, d.Kill, d.GroupKill}
}

// OOMLimit preserves unlimited and unavailable as distinct from a measured zero.
type OOMLimit struct {
	State string // finite, unlimited, unreported
	Bytes *uint64
}

func (l OOMLimit) valid() bool {
	return (l.State == "finite" && l.Bytes != nil) || ((l.State == "unlimited" || l.State == "unreported") && l.Bytes == nil)
}

// OOMCorrelation keeps local and hierarchical cgroup counters separate. PSI
// deltas are microseconds of observed stall, not counts of OOM decisions.
type OOMCorrelation struct {
	Window                  CorrelationWindow
	Local, Hierarchical     OOMEventDeltas
	Current                 GaugePair
	LimitBefore, LimitAfter OOMLimit
	PSISome, PSIFull        CounterDelta
}

func (OOMCorrelation) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[ephemeral OOM correlation]")
}
func (OOMCorrelation) MarshalJSON() ([]byte, error) {
	return nil, errors.New("OOM correlation requires explicit ephemeral encoding")
}

func (c OOMCorrelation) Validate(start, end time.Time, duration time.Duration) error {
	invalid := errors.New("invalid OOM correlation")
	if c.Window.Validate(start, end, duration) != nil {
		return invalid
	}
	if c.Window.State != "overlapping" {
		c.Window = CorrelationWindow{}
		if c != (OOMCorrelation{}) {
			return invalid
		}
		return nil
	}
	deltas := append(c.Local.Values(), c.Hierarchical.Values()...)
	deltas = append(deltas, c.PSISome, c.PSIFull)
	for _, delta := range deltas {
		if !validCounterDelta(delta) {
			return invalid
		}
	}
	if !c.LimitBefore.valid() || !c.LimitAfter.valid() {
		return invalid
	}
	return nil
}

func validCounterDelta(delta CounterDelta) bool {
	return (delta.State == "reported" && delta.Delta != nil) || ((delta.State == "unreported" || delta.State == "reset") && delta.Delta == nil)
}
