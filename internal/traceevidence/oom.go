package traceevidence

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const MaxOOMBytes = 2560

type oomWindowWire struct {
	EvidenceStart time.Time     `json:"evidenceStart"`
	BeforeEnd     time.Time     `json:"beforeEnd"`
	AfterStart    time.Time     `json:"afterStart"`
	EvidenceEnd   time.Time     `json:"evidenceEnd"`
	OverlapStart  time.Time     `json:"overlapStart"`
	OverlapEnd    time.Time     `json:"overlapEnd"`
	Uncertainty   time.Duration `json:"uncertaintyNanos"`
}
type oomDeltasWire struct {
	Low       delta `json:"low"`
	High      delta `json:"high"`
	Max       delta `json:"max"`
	OOM       delta `json:"oomEvents"`
	Kill      delta `json:"oomKills"`
	GroupKill delta `json:"oomGroupKills"`
}
type oomLimitWire struct {
	State string  `json:"state"`
	Bytes *uint64 `json:"bytes"`
}
type oomWire struct {
	State        string         `json:"state"`
	Window       *oomWindowWire `json:"window,omitempty"`
	Local        *oomDeltasWire `json:"local,omitempty"`
	Hierarchical *oomDeltasWire `json:"hierarchical,omitempty"`
	Current      *gauge         `json:"currentBytes,omitempty"`
	LimitBefore  *oomLimitWire  `json:"limitBefore,omitempty"`
	LimitAfter   *oomLimitWire  `json:"limitAfter,omitempty"`
	PSISome      *delta         `json:"someStallMicros,omitempty"`
	PSIFull      *delta         `json:"fullStallMicros,omitempty"`
}

func OOMEncode(c *trace.OOMCorrelation, start, end time.Time, duration time.Duration) (json.RawMessage, error) {
	if c == nil {
		return nil, nil
	}
	if c.Validate(start, end, duration) != nil {
		return nil, ErrInvalid
	}
	w := oomWire{State: c.Window.State}
	if c.Window.State == "overlapping" {
		t := c.Window
		w.Window = &oomWindowWire{t.EvidenceStart.UTC(), t.BeforeEnd.UTC(), t.AfterStart.UTC(), t.EvidenceEnd.UTC(), t.OverlapStart.UTC(), t.OverlapEnd.UTC(), *t.Uncertainty}
		local, hierarchical := encodeOOMDeltas(c.Local), encodeOOMDeltas(c.Hierarchical)
		w.Local, w.Hierarchical = &local, &hierarchical
		w.Current = &gauge{c.Current.Before, c.Current.After}
		w.LimitBefore, w.LimitAfter = &oomLimitWire{c.LimitBefore.State, c.LimitBefore.Bytes}, &oomLimitWire{c.LimitAfter.State, c.LimitAfter.Bytes}
		w.PSISome, w.PSIFull = &delta{c.PSISome.State, c.PSISome.Delta}, &delta{c.PSIFull.State, c.PSIFull.Delta}
	}
	data, err := json.Marshal(w)
	if err != nil || len(data) > MaxOOMBytes {
		return nil, ErrInvalid
	}
	return data, nil
}

func OOMDecode(data json.RawMessage, start, end time.Time, duration time.Duration) (*trace.OOMCorrelation, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > MaxOOMBytes {
		return nil, ErrInvalid
	}
	var w oomWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&w) != nil {
		return nil, ErrInvalid
	}
	c := &trace.OOMCorrelation{Window: trace.CorrelationWindow{State: w.State}}
	if w.State == "overlapping" {
		if w.Window == nil || w.Local == nil || w.Hierarchical == nil || w.Current == nil || w.LimitBefore == nil || w.LimitAfter == nil || w.PSISome == nil || w.PSIFull == nil {
			return nil, ErrInvalid
		}
		t := w.Window
		c.Window = trace.CorrelationWindow{State: w.State, EvidenceStart: t.EvidenceStart, BeforeEnd: t.BeforeEnd, AfterStart: t.AfterStart, EvidenceEnd: t.EvidenceEnd, OverlapStart: t.OverlapStart, OverlapEnd: t.OverlapEnd, Uncertainty: &t.Uncertainty}
		c.Local, c.Hierarchical = decodeOOMDeltas(*w.Local), decodeOOMDeltas(*w.Hierarchical)
		c.Current = trace.GaugePair{Before: w.Current.Before, After: w.Current.After}
		c.LimitBefore, c.LimitAfter = trace.OOMLimit{State: w.LimitBefore.State, Bytes: w.LimitBefore.Bytes}, trace.OOMLimit{State: w.LimitAfter.State, Bytes: w.LimitAfter.Bytes}
		c.PSISome, c.PSIFull = trace.CounterDelta{State: w.PSISome.State, Delta: w.PSISome.Delta}, trace.CounterDelta{State: w.PSIFull.State, Delta: w.PSIFull.Delta}
	}
	canonical, err := OOMEncode(c, start, end, duration)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, ErrInvalid
	}
	return c, nil
}

func encodeOOMDeltas(d trace.OOMEventDeltas) oomDeltasWire {
	return oomDeltasWire{delta{d.Low.State, d.Low.Delta}, delta{d.High.State, d.High.Delta}, delta{d.Max.State, d.Max.Delta}, delta{d.OOM.State, d.OOM.Delta}, delta{d.Kill.State, d.Kill.Delta}, delta{d.GroupKill.State, d.GroupKill.Delta}}
}

func decodeOOMDeltas(d oomDeltasWire) trace.OOMEventDeltas {
	return trace.OOMEventDeltas{Low: trace.CounterDelta{State: d.Low.State, Delta: d.Low.Delta}, High: trace.CounterDelta{State: d.High.State, Delta: d.High.Delta}, Max: trace.CounterDelta{State: d.Max.State, Delta: d.Max.Delta}, OOM: trace.CounterDelta{State: d.OOM.State, Delta: d.OOM.Delta}, Kill: trace.CounterDelta{State: d.Kill.State, Delta: d.Kill.Delta}, GroupKill: trace.CounterDelta{State: d.GroupKill.State, Delta: d.GroupKill.Delta}}
}
