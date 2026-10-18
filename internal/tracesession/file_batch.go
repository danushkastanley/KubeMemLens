package tracesession

import (
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

// FileActivities coalesces only explicitly authorised path frames. Aggregate-only
// output and the original stream version retain their existing event paths.
func (o *output) FileActivities(events []trace.FileActivity) error {
	if len(events) == 0 || len(events) > trace.MaxFileBatch {
		o.mu.Lock()
		defer o.mu.Unlock()
		return o.rejectAggregate(trace.EngineFailed)
	}
	if len(events) == 1 || o.aggregates == nil || o.spec.Paths() != trace.ConfirmedPaths {
		for _, event := range events {
			if err := o.FileActivity(event); err != nil {
				return err
			}
		}
		return nil
	}
	return o.aggregateFiles(events)
}

func (o *output) aggregateFiles(events []trace.FileActivity) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	var storage [traceframe.MaxBytes]byte
	defer clear(storage[:])
	data := storage[:0]
	pending := uint64(0)
	next := *o.aggregates
	flush := func() error {
		if pending == 0 {
			return nil
		}
		if err := o.write(data); err != nil {
			o.rejected += pending
			return err
		}
		o.events += pending
		*o.aggregates = next
		clear(data)
		data, pending = storage[:0], 0
		return nil
	}
	reject := func(err error) error {
		// Cancellation never authorises delivery of the pending prefix.
		if o.ctx.Err() == nil && !o.closed {
			if err := flush(); err != nil {
				return err
			}
		}
		if o.closed {
			return err
		}
		return o.rejectAggregate(trace.Termination(reason(err)))
	}
	for _, event := range events {
		if err := o.checkAggregateWindow(event.ObservedAt, next.Observations()); err != nil {
			return reject(err)
		}
		frame, err := traceframe.NewFileVersion(event, o.spec, traceframe.AggregateVersion)
		if err != nil {
			return reject(Stop(trace.EngineFailed))
		}
		encoded, err := traceframe.Encode(frame)
		if err != nil {
			return reject(Stop(trace.EngineFailed))
		}
		if o.bytes+uint64(len(data)+len(encoded))+traceframe.AggregateTerminalReserve > o.spec.Bounds().OutputBytes {
			return reject(Stop(trace.OutputLimit))
		}
		if len(data)+len(encoded) > len(storage) {
			if err := flush(); err != nil {
				return err
			}
		}
		candidate := next
		if candidate.File(event) != nil {
			return reject(Stop(trace.EngineFailed))
		}
		next = candidate
		data = append(data, encoded...)
		pending++
		if next.Observations() == o.spec.Bounds().Events {
			if err := flush(); err != nil {
				return err
			}
			return o.stop(trace.EventLimit)
		}
	}
	return flush()
}
