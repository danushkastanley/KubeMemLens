// Package verifier matches numeric observations from owned verifier calls.
// It performs no kernel operations and never retains programme or log contents.
package verifier

import (
	"errors"
	"fmt"
	"io"
)

var ErrObservation = errors.New("incomplete verifier observation")

type Kind uint8

const (
	CheckEnter Kind = iota + 1
	LogFinalized
	CheckReturn
)

// Event contains only the numeric projection of a validated, cgroup-filtered
// kernel sample. TID is transient correlation state, not output evidence.
type Event struct {
	Kind         Kind
	Time         uint64
	TID          uint32
	Result       int32
	LogSizeBytes uint32
}

func (Event) MarshalJSON() ([]byte, error) { return nil, ErrObservation }
func (Event) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private verifier event]") }

type Totals struct {
	CompletedCalls    uint64 `json:"completedCalls"`
	RejectedCalls     uint64 `json:"rejectedCalls"`
	LogFailures       uint64 `json:"logFailures"`
	DurationNanos     uint64 `json:"durationNanos"`
	MaximumNanos      uint64 `json:"maximumNanos"`
	FinalizedLogBytes uint64 `json:"finalizedLogBytes"`
	MaximumLogBytes   uint32 `json:"maximumLogBytes"`
	PendingCalls      int    `json:"pendingCalls"`
	UnassociatedLogs  uint64 `json:"unassociatedLogs"`
}

type call struct {
	start, last uint64
	log         *Event
}

type Aggregate struct {
	pending map[uint32]call
	last    uint64
	events  uint64
	totals  Totals
	failed  bool
	closed  bool
}

const maxCalls = 4096
const maxEvents = 32768
const maxConcurrent = 64
const maxDuration = 30_000_000_000

func New() *Aggregate { return &Aggregate{pending: make(map[uint32]call)} }

func (a *Aggregate) Observe(event Event) (err error) {
	defer func() { a.failed = a.failed || err != nil }()
	if a.failed || a.closed || event.Time == 0 || event.Time < a.last || event.TID == 0 ||
		event.TID > 0x7fffffff || event.Result > 0 || event.Result < -4095 || a.events >= maxEvents {
		return ErrObservation
	}
	a.events++
	a.last = event.Time
	current, exists := a.pending[event.TID]
	if exists && (event.Time <= current.last || event.Time-current.start > maxDuration) {
		return ErrObservation
	}
	switch event.Kind {
	case CheckEnter:
		if exists || event.Result != 0 || event.LogSizeBytes != 0 ||
			len(a.pending) >= maxConcurrent || a.totals.CompletedCalls+uint64(len(a.pending)) >= maxCalls {
			return ErrObservation
		}
		a.pending[event.TID] = call{start: event.Time, last: event.Time}
	case LogFinalized:
		if !exists {
			// The same finalizer also serves BTF and other BPF operations. Those
			// calls cannot supply a missing measurement for bpf_check.
			a.totals.UnassociatedLogs++
			return nil
		}
		if current.log != nil {
			return ErrObservation
		}
		current.log, current.last = &event, event.Time
		a.pending[event.TID] = current
	case CheckReturn:
		if !exists || current.log == nil || event.LogSizeBytes != 0 {
			return ErrObservation
		}
		duration := event.Time - current.start
		a.totals.CompletedCalls++
		a.totals.DurationNanos += duration
		a.totals.MaximumNanos = max(a.totals.MaximumNanos, duration)
		a.totals.FinalizedLogBytes += uint64(current.log.LogSizeBytes)
		a.totals.MaximumLogBytes = max(a.totals.MaximumLogBytes, current.log.LogSizeBytes)
		if event.Result != 0 {
			a.totals.RejectedCalls++
		}
		if current.log.Result != 0 {
			a.totals.LogFailures++
		}
		delete(a.pending, event.TID)
	default:
		return ErrObservation
	}
	return nil
}

// Snapshot cannot clear an earlier integrity failure. Unfinished calls remain
// explicit and cannot qualify a final capture, even if their logs were seen.
func (a *Aggregate) Snapshot() (Totals, error) {
	if a.failed {
		return Totals{}, ErrObservation
	}
	result := a.totals
	result.PendingCalls = len(a.pending)
	return result, nil
}

// Advance moves the merged-stream watermark even when no probe events arrive.
// Call after draining and ordering through now; late events then invalidate the
// capture, and a missing return cannot occupy a pending slot indefinitely.
func (a *Aggregate) Advance(now uint64) error {
	if a.failed || a.closed || now == 0 || now < a.last {
		a.failed = true
		return ErrObservation
	}
	a.last = now
	for _, call := range a.pending {
		if now-call.start > maxDuration {
			a.failed = true
			return ErrObservation
		}
	}
	return nil
}

func (a *Aggregate) Finish() (Totals, error) {
	a.closed = true
	if len(a.pending) != 0 {
		a.failed = true
	}
	return a.Snapshot()
}
