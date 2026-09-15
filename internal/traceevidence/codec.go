// Package traceevidence is the explicit ephemeral encoding boundary for cgroup
// correlation. It does not sample a cgroup or authorise disclosure.
package traceevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const MaxBytes = 1536

var ErrInvalid = errors.New("invalid ephemeral cgroup evidence")

type gauge struct {
	Before *uint64 `json:"before"`
	After  *uint64 `json:"after"`
}
type delta struct {
	State string  `json:"state"`
	Delta *uint64 `json:"delta"`
}
type wire struct {
	State         string         `json:"state"`
	EvidenceStart *time.Time     `json:"evidenceStart,omitempty"`
	BeforeEnd     *time.Time     `json:"beforeEnd,omitempty"`
	AfterStart    *time.Time     `json:"afterStart,omitempty"`
	EvidenceEnd   *time.Time     `json:"evidenceEnd,omitempty"`
	OverlapStart  *time.Time     `json:"overlapStart,omitempty"`
	OverlapEnd    *time.Time     `json:"overlapEnd,omitempty"`
	Uncertainty   *time.Duration `json:"uncertaintyNanos,omitempty"`
	File          *gauge         `json:"fileBytes,omitempty"`
	Dirty         *gauge         `json:"dirtyBytes,omitempty"`
	Writeback     *gauge         `json:"writebackBytes,omitempty"`
	Refault       *delta         `json:"refault,omitempty"`
	Scan          *delta         `json:"scan,omitempty"`
	Steal         *delta         `json:"steal,omitempty"`
}

// Encode returns an owned canonical wire snapshot after window validation.
func Encode(c *trace.Correlation, start, end time.Time, duration time.Duration) (json.RawMessage, error) {
	if c == nil {
		return nil, nil
	}
	if c.Validate(start, end, duration) != nil {
		return nil, ErrInvalid
	}
	w := wire{State: c.State}
	if c.State == "overlapping" {
		w.EvidenceStart, w.BeforeEnd, w.AfterStart, w.EvidenceEnd = utc(c.EvidenceStart), utc(c.BeforeEnd), utc(c.AfterStart), utc(c.EvidenceEnd)
		w.OverlapStart, w.OverlapEnd, w.Uncertainty = utc(c.OverlapStart), utc(c.OverlapEnd), c.Uncertainty
		w.File, w.Dirty, w.Writeback = &gauge{c.File.Before, c.File.After}, &gauge{c.Dirty.Before, c.Dirty.After}, &gauge{c.Writeback.Before, c.Writeback.After}
		w.Refault, w.Scan, w.Steal = &delta{c.Refault.State, c.Refault.Delta}, &delta{c.Scan.State, c.Scan.Delta}, &delta{c.Steal.State, c.Steal.Delta}
	}
	data, err := json.Marshal(w)
	if err != nil || len(data) > MaxBytes {
		return nil, ErrInvalid
	}
	return data, nil
}

// Decode accepts exactly the canonical representation produced by Encode.
// Missing/extra fields, aliases, duplicate keys and ambiguous nulls are rejected.
func Decode(data json.RawMessage, start, end time.Time, duration time.Duration) (*trace.Correlation, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > MaxBytes {
		return nil, ErrInvalid
	}
	var w wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&w) != nil {
		return nil, ErrInvalid
	}
	c := &trace.Correlation{State: w.State}
	if w.State == "overlapping" {
		if w.EvidenceStart == nil || w.BeforeEnd == nil || w.AfterStart == nil || w.EvidenceEnd == nil || w.OverlapStart == nil || w.OverlapEnd == nil || w.Uncertainty == nil || w.File == nil || w.Dirty == nil || w.Writeback == nil || w.Refault == nil || w.Scan == nil || w.Steal == nil {
			return nil, ErrInvalid
		}
		c.EvidenceStart, c.BeforeEnd, c.AfterStart, c.EvidenceEnd = *w.EvidenceStart, *w.BeforeEnd, *w.AfterStart, *w.EvidenceEnd
		c.OverlapStart, c.OverlapEnd, c.Uncertainty = *w.OverlapStart, *w.OverlapEnd, w.Uncertainty
		c.File, c.Dirty, c.Writeback = trace.GaugePair{Before: w.File.Before, After: w.File.After}, trace.GaugePair{Before: w.Dirty.Before, After: w.Dirty.After}, trace.GaugePair{Before: w.Writeback.Before, After: w.Writeback.After}
		c.Refault, c.Scan, c.Steal = trace.CounterDelta{State: w.Refault.State, Delta: w.Refault.Delta}, trace.CounterDelta{State: w.Scan.State, Delta: w.Scan.Delta}, trace.CounterDelta{State: w.Steal.State, Delta: w.Steal.Delta}
	}
	canonical, err := Encode(c, start, end, duration)
	if err != nil || !bytes.Equal(data, canonical) {
		return nil, ErrInvalid
	}
	return c, nil
}

func utc(t time.Time) *time.Time { t = t.UTC(); return &t }
