package admissionapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceaudit"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
)

type relay struct {
	version         int
	reader          *traceframe.Reader
	sink            *streamhttp.Sink
	lease           *admission.Lease
	written, events uint64
	transportFailed bool
	oomContext      *oomSessionContext
	batch           [traceframe.MaxBytes]byte
}

func (*relay) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[private trace relay]") }

func (r *relay) forward(ctx context.Context, frame traceframe.Frame) error {
	data, err := traceframe.Encode(frame)
	if err != nil {
		return admission.ErrUnavailable
	}
	events := uint64(0)
	if frame.Type() == traceframe.EventFrame {
		events = 1
	}
	return r.write(ctx, data, events)
}

// write commits delivery accounting only after the synchronous bounded flush.
// A partial batch is a failed transport and can never receive a final summary.
func (r *relay) write(ctx context.Context, data []byte, events uint64) error {
	n, err := r.sink.WriteFrame(ctx, data)
	if n >= 0 && n <= len(data) {
		r.written += uint64(n)
	}
	if err != nil || n != len(data) {
		r.transportFailed = true
		return admission.ErrUnavailable
	}
	r.events += events
	return nil
}
func (r *relay) run(ctx context.Context, first traceframe.Frame) error {
	frame := first
	for {
		if frame.Type() == traceframe.EventFrame {
			next, err := r.forwardEvents(ctx, frame)
			if err != nil {
				return err
			}
			frame = next
			continue
		}
		if frame.Type() == traceframe.SummaryFrame {
			if _, err := r.reader.Next(); err != io.EOF {
				return admission.ErrUnavailable
			}
			if err := r.lease.RevalidateStream(ctx); err != nil {
				return err
			}
			if r.oomContext != nil {
				var err error
				frame, err = r.oomContext.finish(ctx, frame)
				if err != nil {
					return err
				}
			}
		}
		if frame.Type() == traceframe.SummaryFrame {
			summary, err := frame.ClientSummary()
			if err != nil {
				return admission.ErrUnavailable
			}
			if err := r.lease.RecordTerminal(summary.Termination, traceaudit.StreamOutcome); err != nil {
				return err
			}
		}
		if err := r.forward(ctx, frame); err != nil {
			return err
		}
		if frame.Type() == traceframe.SummaryFrame {
			return nil
		}
		var err error
		frame, err = r.reader.Next()
		if err != nil {
			return admission.ErrUnavailable
		}
	}
}

// A control-side failure can still terminate a healthy downstream connection.
// It reports only known delivery counts and an unknown observation window. No
// summary is appended after a failed/partial downstream frame write.
func (r *relay) terminate(parent context.Context, reason trace.Termination) error {
	if r.transportFailed || parent.Err() != nil {
		return admission.ErrUnavailable
	}
	summary, err := traceframe.NewSummaryVersion(traceframe.Summary{SessionEndedAt: time.Now().UTC(), Termination: reason, WrittenEvents: r.events, WrittenBytesBeforeSummary: r.written, Incomplete: true}, r.version)
	if err != nil {
		return admission.ErrUnavailable
	}
	data, err := traceframe.Encode(summary)
	if err != nil || r.written+uint64(len(data)) > r.lease.Admission().Specification().Bounds().OutputBytes {
		return admission.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	return r.forward(ctx, summary)
}
func validationTermination(err error) trace.Termination {
	if errors.Is(err, admission.ErrTargetChanged) {
		return trace.TargetChanged
	}
	if errors.Is(err, admission.ErrDenied) {
		return trace.AuthorisationLost
	}
	return trace.EngineFailed
}
