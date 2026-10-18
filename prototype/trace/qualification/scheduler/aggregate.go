// Package scheduler derives bounded numeric runqueue observations from ordered
// scheduling events. It performs no kernel operations and retains no task names.
package scheduler

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
)

var ErrObservation = errors.New("incomplete scheduler observation")

type Kind uint8

const (
	Wake Kind = iota + 1
	NewTask
	Switch
	Exit
)

// Event is the numeric projection of one validated perf tracepoint sample.
// PID is the woken/exiting task. Switch uses PrevPID, NextPID and PrevRunnable.
// The reader must merge all CPU streams by monotonic timestamp before Observe.
type Event struct {
	Kind         Kind
	Time         uint64
	PID          uint32
	PrevPID      uint32
	NextPID      uint32
	PrevRunnable bool
}

// Event identities are transient matching inputs, never an evidence format.
func (Event) MarshalJSON() ([]byte, error) { return nil, ErrObservation }
func (Event) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private scheduler event]") }

type Histogram struct {
	// Bucket zero contains zero waits; bucket k>0 contains [2^(k-1), 2^k-1] ns.
	Buckets      [65]uint64 `json:"buckets"`
	Count        uint64     `json:"count"`
	SumNanos     uint64     `json:"sumNanos"`
	MaximumNanos uint64     `json:"maximumNanos"`
}

type Snapshot struct {
	Histogram          Histogram `json:"completedWaits"`
	Events             uint64    `json:"events"`
	UnmatchedSwitchIns uint64    `json:"unmatchedSwitchIns"`
	ReplacedEnqueues   uint64    `json:"replacedEnqueues"`
	ExitedPending      uint64    `json:"exitedPending"`
	Pending            int       `json:"pending"`
}

type Aggregate struct {
	pending  map[uint32]uint64
	maximum  int
	last     uint64
	snapshot Snapshot
	failed   bool
}

func New(maximum int) (*Aggregate, error) {
	if maximum < 1 || maximum > 32768 {
		return nil, ErrObservation
	}
	return &Aggregate{pending: make(map[uint32]uint64), maximum: maximum}, nil
}

func (a *Aggregate) enqueue(pid uint32, now uint64) error {
	if pid == 0 {
		return nil
	} // Per-CPU idle tasks are not runqueue wait samples.
	if _, exists := a.pending[pid]; exists {
		// Use the latest actual enqueue, not time spent outside a runnable state.
		a.snapshot.ReplacedEnqueues++
	} else if len(a.pending) >= a.maximum {
		return ErrObservation
	}
	a.pending[pid] = now
	return nil
}

func (a *Aggregate) switchIn(pid uint32, now uint64) error {
	if pid == 0 {
		return nil
	}
	start, exists := a.pending[pid]
	if !exists {
		a.snapshot.UnmatchedSwitchIns++
		return nil
	}
	if start > now {
		return ErrObservation
	}
	wait := now - start
	h := &a.snapshot.Histogram
	if wait > math.MaxUint64-h.SumNanos {
		return ErrObservation
	}
	h.Buckets[bits.Len64(wait)]++
	h.Count++
	h.SumNanos += wait
	if wait > h.MaximumNanos {
		h.MaximumNanos = wait
	}
	delete(a.pending, pid)
	return nil
}

func valid(e Event) bool {
	if e.Time == 0 {
		return false
	}
	switch e.Kind {
	case Wake, NewTask, Exit:
		return e.PID > 0 && e.PrevPID == 0 && e.NextPID == 0 && !e.PrevRunnable
	case Switch:
		return e.PID == 0 && e.PrevPID != e.NextPID
	default:
		return false
	}
}

// Observe fails permanently after ordering, integrity or capacity failure. The
// caller must retain partial evidence and close its owned kernel descriptors.
func (a *Aggregate) Observe(e Event) error {
	if a.failed {
		return ErrObservation
	}
	if !valid(e) || e.Time < a.last || a.snapshot.Events >= 100000000 {
		a.failed = true
		return ErrObservation
	}
	a.last = e.Time
	a.snapshot.Events++
	var err error
	switch e.Kind {
	case Wake:
		err = a.enqueue(e.PID, e.Time)
	case NewTask:
		// A new lifetime must not inherit an old lifetime's queued timestamp.
		if _, exists := a.pending[e.PID]; exists {
			err = ErrObservation
		} else {
			err = a.enqueue(e.PID, e.Time)
		}
	case Exit:
		if _, exists := a.pending[e.PID]; exists {
			a.snapshot.ExitedPending++
		}
		delete(a.pending, e.PID)
	case Switch:
		err = a.switchIn(e.NextPID, e.Time)
		if err == nil && e.PrevRunnable {
			err = a.enqueue(e.PrevPID, e.Time)
		}
	}
	if err != nil {
		a.failed = true
	}
	return err
}

// Lost invalidates the entire stream; no partial histogram can hide lost events.
func (a *Aggregate) Lost() { a.failed = true }

func (a *Aggregate) Snapshot() (Snapshot, error) {
	if a.failed {
		return Snapshot{}, ErrObservation
	}
	value := a.snapshot
	value.Pending = len(a.pending)
	return value, nil
}

type Bounds struct {
	Lower uint64 `json:"lowerNanos"`
	Upper uint64 `json:"upperNanos"`
}

// Quantile returns a conservative bucket interval, never an invented exact
// percentile. Empty input has no quantile. Percent is an integer from 1 to 100.
func (h Histogram) Quantile(percent uint64) (Bounds, error) {
	if h.Count == 0 || h.Count > 100000000 || percent < 1 || percent > 100 {
		return Bounds{}, ErrObservation
	}
	total := uint64(0)
	for _, count := range h.Buckets {
		if count > h.Count-total {
			return Bounds{}, ErrObservation
		}
		total += count
	}
	if total != h.Count {
		return Bounds{}, ErrObservation
	}
	rank := (h.Count*percent + 99) / 100
	total = 0
	for k, count := range h.Buckets {
		total += count
		if total < rank {
			continue
		}
		if k == 0 {
			return Bounds{}, nil
		}
		lower := uint64(1) << uint(k-1)
		upper := uint64(math.MaxUint64)
		if k < 64 {
			upper = (uint64(1) << uint(k)) - 1
		}
		return Bounds{lower, upper}, nil
	}
	return Bounds{}, ErrObservation
}
