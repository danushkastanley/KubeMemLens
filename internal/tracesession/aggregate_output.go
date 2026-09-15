package tracesession

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

// These operations hold the output lock from validation through delivery and
// accumulation. Default file/cache output has no per-event public frames.
func (o *output) aggregateFile(event trace.FileActivity) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.aggregateWindow(event.ObservedAt); err != nil {
		return err
	}
	if o.spec.Paths() == trace.ConfirmedPaths {
		frame, err := traceframe.NewFileVersion(event, o.spec, traceframe.AggregateVersion)
		if err != nil {
			return o.rejectAggregate(trace.EngineFailed)
		}
		data, err := traceframe.Encode(frame)
		if err != nil {
			return o.rejectAggregate(trace.EngineFailed)
		}
		if o.bytes+uint64(len(data))+traceframe.AggregateTerminalReserve > o.spec.Bounds().OutputBytes {
			return o.rejectAggregate(trace.OutputLimit)
		}
		if err := o.write(data); err != nil {
			o.rejected++
			return err
		}
		o.events++
	}
	return o.finishAggregate(o.aggregates.File(event))
}

func (o *output) aggregateCache(event trace.CacheActivity) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.aggregateWindow(event.ObservedAt); err != nil {
		return err
	}
	return o.finishAggregate(o.aggregates.Cache(event))
}

func (o *output) aggregateWindow(observed time.Time) error {
	if o.closed {
		return Stop(trace.Cancelled)
	}
	if o.ctx.Err() != nil {
		o.rejected++
		return reason(context.Cause(o.ctx))
	}
	if observed.Before(o.started) || observed.After(o.deadline) {
		return o.rejectAggregate(trace.EngineFailed)
	}
	if o.aggregates.Observations() >= o.spec.Bounds().Events {
		return o.rejectAggregate(trace.EventLimit)
	}
	return nil
}

func (o *output) finishAggregate(err error) error {
	if err == traceaggregate.ErrLimit {
		return o.rejectAggregate(trace.EventLimit)
	}
	if err != nil {
		return o.rejectAggregate(trace.EngineFailed)
	}
	if o.aggregates.Observations() == o.spec.Bounds().Events {
		return o.stop(trace.EventLimit)
	}
	return nil
}
func (o *output) rejectAggregate(reason trace.Termination) error { o.rejected++; return o.stop(reason) }
