package traceevidence

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const MaxKubernetesOOMBytes = 768

type kubernetesOOMWire struct {
	State          string     `json:"state"`
	BeforeStart    *time.Time `json:"beforeStart,omitempty"`
	BeforeEnd      *time.Time `json:"beforeEnd,omitempty"`
	AfterStart     *time.Time `json:"afterStart,omitempty"`
	AfterEnd       *time.Time `json:"afterEnd,omitempty"`
	Restarts       *delta     `json:"restarts,omitempty"`
	PressureBefore *string    `json:"pressureBefore,omitempty"`
	PressureAfter  *string    `json:"pressureAfter,omitempty"`
}

func KubernetesOOMEncode(c *trace.KubernetesOOMContext, duration time.Duration) (json.RawMessage, error) {
	if c == nil {
		return nil, nil
	}
	if c.Validate(duration) != nil {
		return nil, ErrInvalid
	}
	w := kubernetesOOMWire{State: c.State}
	if c.State == "observed" {
		w.BeforeStart, w.BeforeEnd, w.AfterStart, w.AfterEnd = utc(c.BeforeStart), utc(c.BeforeEnd), utc(c.AfterStart), utc(c.AfterEnd)
		w.Restarts = &delta{c.Restarts.State, c.Restarts.Delta}
		w.PressureBefore, w.PressureAfter = &c.PressureBefore, &c.PressureAfter
	}
	data, err := json.Marshal(w)
	if err != nil || len(data) > MaxKubernetesOOMBytes {
		return nil, ErrInvalid
	}
	return data, nil
}

func KubernetesOOMDecode(data json.RawMessage, duration time.Duration) (*trace.KubernetesOOMContext, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > MaxKubernetesOOMBytes {
		return nil, ErrInvalid
	}
	var w kubernetesOOMWire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&w) != nil {
		return nil, ErrInvalid
	}
	c := &trace.KubernetesOOMContext{State: w.State}
	if w.State == "observed" {
		if w.BeforeStart == nil || w.BeforeEnd == nil || w.AfterStart == nil || w.AfterEnd == nil || w.Restarts == nil || w.PressureBefore == nil || w.PressureAfter == nil {
			return nil, ErrInvalid
		}
		c.BeforeStart, c.BeforeEnd, c.AfterStart, c.AfterEnd = *w.BeforeStart, *w.BeforeEnd, *w.AfterStart, *w.AfterEnd
		c.Restarts = trace.CounterDelta{State: w.Restarts.State, Delta: w.Restarts.Delta}
		c.PressureBefore, c.PressureAfter = *w.PressureBefore, *w.PressureAfter
	}
	canonical, err := KubernetesOOMEncode(c, duration)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, ErrInvalid
	}
	return c, nil
}
