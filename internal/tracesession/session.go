// Package tracesession owns one bounded, non-replayable ephemeral output session.
package tracesession

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

// Sink must honour context cancellation/deadlines and report actual bytes
// accepted by its transport, including partial writes. It must not retain data.
type Sink interface {
	WriteFrame(context.Context, []byte) (int, error)
}

// Validate checks current owner permission and exact target. It must honour its
// one-second context; return Stop for a specific permission/target loss.
type Validate func(context.Context) error

type Stop trace.Termination

func (s Stop) Error() string { return string(s) }

var ErrConsumed = errors.New("trace session already consumed")
var ErrConfiguration = errors.New("invalid trace session configuration")

type Outcome struct {
	Summary           traceframe.Summary
	WrittenBytes      uint64
	TerminalDelivered bool
	Err               error
}
type Session struct {
	mu       sync.Mutex
	consumed bool
	metadata traceframe.Metadata
	engine   *trace.Engine
	sink     Sink
	validate Validate
}

// New does not execute the engine. The trusted caller selects the approved
// programme identity; digest syntax validation does not constitute approval.
func New(metadata traceframe.Metadata, engine *trace.Engine, sink Sink, validate Validate) (*Session, error) {
	if engine == nil || sink == nil || validate == nil || metadata.Specification.Validate() != nil {
		return nil, ErrConfiguration
	}
	return &Session{metadata: metadata, engine: engine, sink: sink, validate: validate}, nil
}

func (s *Session) Run(parent context.Context) Outcome {
	s.mu.Lock()
	if s.consumed {
		s.mu.Unlock()
		return Outcome{Err: ErrConsumed}
	}
	s.consumed = true
	s.mu.Unlock()
	if parent == nil || parent.Err() != nil {
		return Outcome{Err: Stop(trace.Cancelled)}
	}
	started := time.Now().UTC()
	bounds := s.metadata.Specification.Bounds()
	s.metadata.SessionStartedAt = started
	s.metadata.Deadline = started.Add(bounds.Duration)
	if deadline, ok := parent.Deadline(); ok && deadline.Before(s.metadata.Deadline) {
		s.metadata.Deadline = deadline.UTC()
	}
	frame, err := traceframe.NewMetadata(s.metadata)
	if err != nil {
		return Outcome{Err: ErrConfiguration}
	}
	data, err := traceframe.Encode(frame)
	if err != nil || uint64(len(data)+traceframe.TerminalReserve) > bounds.OutputBytes {
		return Outcome{Err: Stop(trace.OutputLimit)}
	}
	ctx, deadlineCancel := context.WithDeadlineCause(parent, s.metadata.Deadline, Stop(trace.Expired))
	defer deadlineCancel()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(Stop(trace.Cancelled))
	if err := s.check(ctx); err != nil {
		return Outcome{Err: reason(err)}
	}
	output := &output{ctx: ctx, cancel: cancel, sink: s.sink, spec: s.metadata.Specification, started: started, deadline: s.metadata.Deadline}
	if err := output.write(data); err != nil {
		return Outcome{WrittenBytes: output.bytes, Err: Stop(trace.EngineFailed)}
	}
	validationDone := make(chan struct{})
	go s.watch(ctx, cancel, validationDone)
	result, engineErr := s.engine.Run(ctx, s.metadata.Specification, output)
	// Closing output prevents retained callbacks from emitting after the adapter
	// returns. The Adapter contract requires resources released before return.
	output.mu.Lock()
	output.closed = true
	output.mu.Unlock()
	cause := context.Cause(ctx)
	cancel(Stop(trace.Cancelled))
	<-validationDone
	summary := output.summary(result, engineErr, cause, time.Now().UTC())
	if output.transportFailed {
		summary.Incomplete = true
		return Outcome{Summary: summary, WrittenBytes: output.bytes, Err: Stop(trace.EngineFailed)}
	}
	terminal, err := traceframe.NewSummary(summary)
	if err != nil {
		return Outcome{Summary: summary, WrittenBytes: output.bytes, Err: Stop(trace.EngineFailed)}
	}
	terminalData, _ := traceframe.Encode(terminal)
	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(parent), time.Second)
	defer finalCancel()
	written, err := s.sink.WriteFrame(finalCtx, terminalData)
	if written < 0 || written > len(terminalData) {
		return Outcome{Summary: summary, WrittenBytes: output.bytes, Err: Stop(trace.EngineFailed)}
	}
	total := output.bytes + uint64(written)
	delivered := err == nil && written == len(terminalData)
	if !delivered {
		return Outcome{Summary: summary, WrittenBytes: total, Err: Stop(trace.EngineFailed)}
	}
	return Outcome{Summary: summary, WrittenBytes: total, TerminalDelivered: true}
}
func (s *Session) check(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	err := s.validate(bounded)
	if ctx.Err() != nil {
		return reason(context.Cause(ctx))
	}
	if err != nil {
		return reason(err)
	}
	if bounded.Err() != nil {
		return Stop(trace.AuthorisationLost)
	}
	return nil
}
func (s *Session) watch(ctx context.Context, cancel context.CancelCauseFunc, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.check(ctx); err != nil {
				cancel(err)
				return
			}
		}
	}
}
func reason(err error) Stop {
	var stop Stop
	if errors.As(err, &stop) {
		switch trace.Termination(stop) {
		case trace.Expired, trace.Cancelled, trace.TargetChanged, trace.OutputLimit, trace.EventLimit, trace.EngineFailed, trace.AuthorisationLost:
			return stop
		}
	}
	if errors.Is(err, context.Canceled) {
		return Stop(trace.Cancelled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Stop(trace.Expired)
	}
	return Stop(trace.EngineFailed)
}
