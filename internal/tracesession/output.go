package tracesession

import (
	"context"
	"math/bits"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

type output struct {
	mu                      sync.Mutex
	ctx                     context.Context
	cancel                  context.CancelCauseFunc
	sink                    Sink
	spec                    trace.Specification
	started, deadline       time.Time
	bytes, events, rejected uint64
	transportFailed         bool
	closed                  bool
}

func (o *output) FileActivity(e trace.FileActivity) error {
	return o.event(e.ObservedAt, func() (traceframe.Frame, error) { return traceframe.NewFile(e, o.spec) })
}
func (o *output) CacheActivity(e trace.CacheActivity) error {
	return o.event(e.ObservedAt, func() (traceframe.Frame, error) { return traceframe.NewCache(e, o.spec) })
}
func (o *output) OOMDecision(e trace.OOMDecision) error {
	return o.event(e.ObservedAt, func() (traceframe.Frame, error) { return traceframe.NewOOM(e, o.spec) })
}
func (o *output) event(observed time.Time, encode func() (traceframe.Frame, error)) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return Stop(trace.Cancelled)
	}
	if err := o.ctx.Err(); err != nil {
		o.rejected++
		return reason(context.Cause(o.ctx))
	}
	if observed.Before(o.started) || observed.After(o.deadline) {
		o.rejected++
		return o.stop(trace.EngineFailed)
	}
	frame, err := encode()
	if err != nil {
		o.rejected++
		return o.stop(trace.EngineFailed)
	}
	data, err := traceframe.Encode(frame)
	if err != nil {
		o.rejected++
		return o.stop(trace.EngineFailed)
	}
	if o.bytes+uint64(len(data))+traceframe.TerminalReserve > o.spec.Bounds().OutputBytes {
		o.rejected++
		return o.stop(trace.OutputLimit)
	}
	if o.events >= o.spec.Bounds().Events {
		o.rejected++
		return o.stop(trace.EventLimit)
	}
	if err := o.write(data); err != nil {
		o.rejected++
		return err
	}
	o.events++
	if o.events == o.spec.Bounds().Events {
		return o.stop(trace.EventLimit)
	}
	return nil
}

// write is serialised by Run before engine start and by event during execution.
func (o *output) write(data []byte) error {
	ctx, cancel := context.WithTimeout(o.ctx, time.Second)
	defer cancel()
	n, err := o.sink.WriteFrame(ctx, data)
	if n < 0 || n > len(data) {
		o.transportFailed = true
		return o.stop(trace.EngineFailed)
	}
	o.bytes += uint64(n)
	if err != nil || n != len(data) {
		o.transportFailed = true
		return o.stop(trace.EngineFailed)
	}
	return nil
}
func (o *output) stop(termination trace.Termination) error {
	err := Stop(termination)
	o.cancel(err)
	return err
}
func (o *output) summary(result trace.Result, engineErr, cause error, ended time.Time) traceframe.Summary {
	termination := trace.Termination(reason(Stop(result.Termination)))
	incomplete := result.Incomplete
	if cause != nil {
		termination = trace.Termination(reason(cause))
	} else if engineErr != nil {
		termination = trace.EngineFailed
	}
	if termination == trace.Expired && ended.Before(o.deadline) {
		termination = trace.EngineFailed
	}
	if termination == "" {
		termination = trace.EngineFailed
	}
	summary := traceframe.Summary{SessionEndedAt: ended, Termination: termination, EngineCounts: result.Counts, WrittenEvents: o.events, RejectedEvents: o.rejected, WrittenBytesBeforeSummary: o.bytes, Incomplete: incomplete}
	if result.Version == trace.ContractVersion && !result.StartedAt.IsZero() && !result.StartedAt.Before(o.started) && !result.EndedAt.Before(result.StartedAt) && !result.EndedAt.After(ended) {
		start, end := result.StartedAt.UTC(), result.EndedAt.UTC()
		summary.ObservationStartedAt = &start
		summary.ObservationEndedAt = &end
	}
	counts := result.Counts
	unknown := counts.Produced == nil || counts.Sampled == nil || counts.Lost == nil || counts.Rejected == nil
	summary.Incomplete = summary.Incomplete || summary.ObservationStartedAt == nil || unknown || o.rejected != 0 || termination != trace.Expired
	if !unknown {
		total, carry := bits.Add64(o.events, o.rejected, 0)
		overflow := carry != 0
		for _, count := range []uint64{*counts.Sampled, *counts.Lost, *counts.Rejected} {
			total, carry = bits.Add64(total, count, 0)
			overflow = overflow || carry != 0
		}
		summary.Incomplete = summary.Incomplete || overflow || total != *counts.Produced
		summary.Incomplete = summary.Incomplete || *counts.Sampled != 0 || *counts.Lost != 0 || *counts.Rejected != 0
	}
	if termination == trace.AuthorisationLost {
		summary.EngineCounts = trace.Counts{}
		summary.ObservationStartedAt = nil
		summary.ObservationEndedAt = nil
		summary.Incomplete = true
	}
	return summary
}
